#!/usr/bin/python3
"""Explicit bounded GTK3 CLIPBOARD exchange; never a host clipboard bridge.

read: JSON metadata line, exactly length UTF-8 bytes, EOF.
write: raw UTF-8 stdin through EOF; metadata receipt only. An owner remains
until selection loss or the exact active desktop disappears. No payload files.
"""
import ctypes as C
import errno
import json
import os
import pwd
import re
import resource
import select
import signal
import stat
import subprocess
import sys
import time

LIMIT = 1024 * 1024
TIMEOUT = 30
UID = 1000
RUNTIME = '/run/user/1000'
BASE_ENV = {'PATH': '/usr/bin:/bin', 'LANG': 'C.UTF-8', 'HOME': '/home/boxwarden',
            'USER': 'boxwarden', 'LOGNAME': 'boxwarden', 'XDG_RUNTIME_DIR': RUNTIME,
            'DBUS_SESSION_BUS_ADDRESS': 'unix:path=' + RUNTIME + '/bus'}


class ClipboardError(Exception):
    def __init__(self, message='clipboard unavailable', unknown=False):
        super().__init__(message)
        self.unknown = unknown


def validate(value):
    if not isinstance(value, bytes) or len(value) > LIMIT or b'\0' in value:
        raise ClipboardError('invalid clipboard text')
    try:
        value.decode('utf-8', errors='strict')
    except UnicodeError:
        raise ClipboardError('invalid clipboard text') from None
    return value


def read_input(stream):
    return validate(stream.read(LIMIT + 1))


def read_value(desktop, check):
    check()
    value = desktop.read()
    if value is None:
        raise ClipboardError('clipboard text absent')
    validate(value)
    check()
    return value


def claim_value(desktop, value, check, before_claim=None, on_claim=None):
    validate(value)
    check()
    if before_claim is not None:
        before_claim()
    try:
        claimed = desktop.claim(value)
    except BaseException:
        raise ClipboardError('clipboard outcome unknown', unknown=True) from None
    if not claimed:
        raise ClipboardError('clipboard claim refused')
    if on_claim is not None:
        on_claim()
    try:
        check()
    except Exception:
        raise ClipboardError('clipboard outcome unknown', unknown=True) from None


def write_frame(out, status, value=None, length=0):
    header = {'version': 1, 'status': status, 'length': len(value) if value is not None else length}
    out.write(json.dumps(header, separators=(',', ':')).encode('ascii') + b'\n')
    if value is not None:
        out.write(value)
    out.flush()


def choose_session(sessions):
    # A UID-wide user manager can retain the DISPLAY of an inactive login.
    # Refuse any second local user desktop, including inactive/closing ones,
    # rather than selecting only among the currently active graphical logins.
    desktops = [s for s in sessions if s.get('Remote') == 'no' and
                s.get('Type') in ('x11', 'wayland') and s.get('Class') == 'user']
    if len(desktops) != 1:
        raise ClipboardError('desktop absent or ambiguous')
    selected = desktops[0]
    if selected.get('User') != str(UID) or selected.get('Name') != 'boxwarden' or selected.get('Active') != 'yes' or selected.get('State') != 'active' or selected.get('Type') != 'wayland':
        raise ClipboardError('desktop absent or ambiguous')
    return selected['Id']


def desktop_env(values):
    display, auth = values.get('DISPLAY', ''), values.get('XAUTHORITY', '')
    if not re.fullmatch(r':[0-9]+(?:\.[0-9]+)?', display) or not auth.startswith(RUNTIME + '/') or '\0' in auth or '..' in auth.split('/'):
        raise ClipboardError('desktop environment unavailable')
    return dict(BASE_ENV, DISPLAY=display, XAUTHORITY=auth, GDK_BACKEND='x11')


def command(argv):
    try:
        result = subprocess.run(argv, env=BASE_ENV, stdin=subprocess.DEVNULL,
                                stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, timeout=2, check=True)
        if len(result.stdout) > 32768:
            raise ClipboardError()
        return result.stdout.decode('utf-8', errors='strict')
    except (OSError, subprocess.SubprocessError, UnicodeError):
        raise ClipboardError('desktop unavailable') from None


def properties(text):
    result = {}
    for line in text.splitlines():
        key, sep, value = line.partition('=')
        if sep:
            if key in result:
                raise ClipboardError('desktop environment ambiguous')
            result[key] = value
    return result


def session_id():
    # SSH sessions may disappear between list-sessions and show-session. One
    # complete metadata rescan handles that benign race; clipboard delivery
    # itself is never retried. Ambiguous/absent graphical snapshots still fail.
    deadline = time.monotonic() + 4
    for attempt in range(2):
        listing = command(['/usr/bin/loginctl', 'list-sessions', '--no-legend', '--no-pager'])
        lines = listing.splitlines()
        if len(lines) > 64:
            raise ClipboardError('desktop unavailable')
        sessions = []
        failed_query = False
        for line in lines:
            if time.monotonic() >= deadline:
                raise ClipboardError('desktop unavailable')
            fields = line.split()
            if not fields or not re.fullmatch(r'[A-Za-z0-9_-]{1,64}', fields[0]):
                raise ClipboardError('desktop unavailable')
            try:
                raw = command(['/usr/bin/loginctl', 'show-session', fields[0],
                    '-p', 'Id', '-p', 'User', '-p', 'Name', '-p', 'Active', '-p', 'Remote',
                    '-p', 'Type', '-p', 'Class', '-p', 'State', '--no-pager'])
            except ClipboardError:
                if attempt:
                    raise
                failed_query = True
                break
            sessions.append(properties(raw))
        if not failed_query:
            if time.monotonic() >= deadline:
                raise ClipboardError('desktop unavailable')
            return choose_session(sessions)
    raise ClipboardError('desktop unavailable')


def environment():
    values = properties(command(['/usr/bin/systemctl', '--user', 'show-environment']))
    env = desktop_env(values)
    for path, kind in ((RUNTIME, stat.S_ISDIR), (RUNTIME + '/bus', stat.S_ISSOCK), (env['XAUTHORITY'], stat.S_ISREG)):
        try:
            metadata = os.lstat(path)
        except OSError:
            raise ClipboardError('desktop environment unavailable') from None
        if not kind(metadata.st_mode) or metadata.st_uid != UID:
            raise ClipboardError('desktop environment unavailable')
        if path == RUNTIME and stat.S_IMODE(metadata.st_mode) != 0o700:
            raise ClipboardError('desktop environment unavailable')
    return env


def bounded_file(path, limit=65536):
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        with os.fdopen(fd, 'rb', closefd=False) as stream:
            raw = stream.read(limit + 1)
        if len(raw) > limit:
            raise ClipboardError('desktop metadata exceeds bound')
        return raw, os.fstat(fd)
    finally:
        os.close(fd)


def session_controller(identity):
    if not re.fullmatch(r'[A-Za-z0-9_-]{1,64}', identity):
        raise ClipboardError('desktop binding unavailable')
    # systemd 255 records the actual logind TakeControl bus connection here.
    # Reading the protected non-secret record avoids inferring session membership
    # from UID-wide environment, timestamps or user-service process ancestry.
    try:
        for directory in ('/run', '/run/systemd', '/run/systemd/sessions'):
            info = os.lstat(directory)
            if not stat.S_ISDIR(info.st_mode) or info.st_uid != 0 or info.st_gid != 0 or info.st_mode & 0o022:
                raise ClipboardError('desktop binding unavailable')
        path = '/run/systemd/sessions/' + identity
        before = os.lstat(path)
        if not stat.S_ISREG(before.st_mode) or before.st_uid != 0 or before.st_gid != 0 or before.st_mode & 0o022 or before.st_nlink != 1:
            raise ClipboardError('desktop binding unavailable')
        raw, opened = bounded_file(path)
        after = os.lstat(path)
        if (before.st_dev, before.st_ino) != (opened.st_dev, opened.st_ino) or (before.st_dev, before.st_ino) != (after.st_dev, after.st_ino):
            raise ClipboardError('desktop binding changed')
        fields = [line.partition(b'=')[2] for line in raw.splitlines() if line.startswith(b'CONTROLLER=')]
        if len(fields) != 1 or len(fields[0]) > 64 or not re.fullmatch(rb':[0-9]+\.[0-9]+', fields[0]):
            raise ClipboardError('desktop controller unavailable')
        return fields[0].decode('ascii')
    except OSError:
        raise ClipboardError('desktop binding unavailable') from None


def controller_pid(controller):
    raw = command(['/usr/bin/busctl', '--system', '--no-pager', 'call',
        'org.freedesktop.DBus', '/org/freedesktop/DBus', 'org.freedesktop.DBus',
        'GetConnectionUnixProcessID', 's', controller])
    if not re.fullmatch(r'u [1-9][0-9]{0,9}\n?', raw):
        raise ClipboardError('desktop controller unavailable')
    pid = int(raw.split()[1])
    if pid > 0xffffffff:
        raise ClipboardError('desktop controller unavailable')
    return pid


def process_exe(pid, deadline):
    if not re.fullmatch(r'[1-9][0-9]{0,9}', pid) or int(pid) > 4294967295:
        raise ClipboardError('desktop process unavailable')
    path = '/proc/' + pid + '/exe'
    try:
        return os.readlink(path)
    except OSError as error:
        if error.errno != errno.EACCES:
            raise
    # Same-user privileged processes (systemd and sd-pam) can deny ptrace exe
    # access. Resolve the actual link with the account's existing unrestricted
    # sudo permission; no name-based process exemption or broad proc read.
    remaining = deadline - time.monotonic()
    if remaining <= 0:
        raise ClipboardError('desktop metadata exceeds bound')
    try:
        result = subprocess.run(['/usr/bin/sudo', '-n', '--', '/usr/bin/readlink', '--', path],
                                env=BASE_ENV, stdin=subprocess.DEVNULL,
                                stdout=subprocess.PIPE, stderr=subprocess.DEVNULL,
                                timeout=min(2, remaining), check=True)
        raw = result.stdout
        # Linux symlink targets are bounded by PATH_MAX. Require exactly the
        # readlink command's single framing LF, without trimming target bytes.
        if len(raw) > 4096 or not raw.endswith(b'\n') or b'\n' in raw[:-1] or b'\0' in raw or not raw.startswith(b'/'):
            raise ClipboardError('desktop process unavailable')
        return raw[:-1].decode('utf-8', errors='strict')
    except (OSError, subprocess.SubprocessError, UnicodeError):
        raise ClipboardError('desktop process unavailable') from None


def process_inventory():
    # Inspect only same-user known compositor/Xwayland metadata. No process
    # environment or unrelated command line is read. Bound scan time, count and
    # the combined metadata bytes, and reject PID reuse while taking a snapshot.
    deadline = time.monotonic() + 4
    remaining = 65536
    records = []
    count = 0
    def read(path):
        nonlocal remaining
        raw, _ = bounded_file(path, remaining)
        remaining -= len(raw)
        return raw
    def identity(raw):
        fields = raw.rsplit(b') ', 1)[1].split()
        if len(fields) < 20:
            raise ClipboardError('desktop process unavailable')
        if fields[0] == b'Z':
            raise ProcessLookupError()
        return int(fields[1]), int(fields[19])
    try:
        with os.scandir('/proc') as entries:
            for entry in entries:
                if not re.fullmatch(r'[1-9][0-9]{0,9}', entry.name):
                    continue
                count += 1
                if count > 4096 or time.monotonic() >= deadline:
                    raise ClipboardError('desktop metadata exceeds bound')
                path = '/proc/' + entry.name
                try:
                    metadata = os.lstat(path)
                    if not stat.S_ISDIR(metadata.st_mode) or metadata.st_uid != UID:
                        continue
                    before = identity(read(path + '/stat'))
                    exe = process_exe(entry.name, deadline)
                    argv = ()
                    if exe == '/usr/bin/Xwayland':
                        raw = read(path + '/cmdline')
                        if not raw.endswith(b'\0'):
                            raise ClipboardError('desktop process unavailable')
                        argv = tuple(part.decode('utf-8', errors='strict') for part in raw[:-1].split(b'\0'))
                    after = identity(read(path + '/stat'))
                    if before != after or os.lstat(path).st_uid != UID or process_exe(entry.name, deadline) != exe or time.monotonic() >= deadline:
                        raise ClipboardError('desktop process changed')
                    if exe not in ('/usr/bin/gnome-shell', '/usr/bin/Xwayland'):
                        continue
                    records.append(dict(pid=int(entry.name), uid=UID, ppid=before[0], start=before[1], exe=exe, argv=argv))
                    if len(records) > 2:
                        raise ClipboardError('desktop process ambiguous')
                except (FileNotFoundError, ProcessLookupError):
                    # An unrelated exiting process is absent in this snapshot.
                    continue
        return records
    except (OSError, ValueError, IndexError, UnicodeError):
        raise ClipboardError('desktop process unavailable') from None


def desktop_binding(identity, env):
    controller = session_controller(identity)
    pid = controller_pid(controller)
    def process_proof(records):
        shells = [p for p in records if p['exe'] == '/usr/bin/gnome-shell']
        servers = [p for p in records if p['exe'] == '/usr/bin/Xwayland']
        if len(shells) != 1 or len(servers) != 1:
            raise ClipboardError('desktop process absent or ambiguous')
        shell, server = shells[0], servers[0]
        argv = server['argv']
        if shell['uid'] != UID or server['uid'] != UID or shell['pid'] != pid or server['ppid'] != pid or shell['start'] <= 0 or server['start'] <= 0:
            raise ClipboardError('desktop controller differs')
        if len(argv) < 4 or argv[0] != '/usr/bin/Xwayland' or argv[1] != env['DISPLAY'] or argv.count('-auth') != 1:
            raise ClipboardError('desktop display differs')
        index = argv.index('-auth')
        if index + 1 >= len(argv) or argv[index + 1] != env['XAUTHORITY']:
            raise ClipboardError('desktop authority differs')
        return (shell['pid'], shell['start'], server['pid'], server['start'], argv)
    proof = process_proof(process_inventory())
    if session_controller(identity) != controller or controller_pid(controller) != pid or process_proof(process_inventory()) != proof:
        raise ClipboardError('desktop binding changed')
    return (controller,) + proof


class Session:
    def __init__(self):
        self.identity = session_id()
        self.env = environment()
        self.binding = desktop_binding(self.identity, self.env)
        self.check()
    def check(self):
        if session_id() != self.identity or environment() != self.env or desktop_binding(self.identity, self.env) != self.binding:
            raise ClipboardError('desktop session changed')


class TargetEntry(C.Structure):
    _fields_ = [('target', C.c_char_p), ('flags', C.c_uint), ('info', C.c_uint)]


class GTKClipboard:
    """Native callbacks avoid GI text conversion and GI's omitted set_with_data."""
    def __init__(self):
        self.gtk = C.CDLL('libgtk-3.so.0')
        self.gdk = C.CDLL('libgdk-3.so.0')
        self.get_callback = C.CFUNCTYPE(None, C.c_void_p, C.c_void_p, C.c_uint, C.c_void_p)
        self.clear_callback = C.CFUNCTYPE(None, C.c_void_p, C.c_void_p)
        self.read_callback = C.CFUNCTYPE(None, C.c_void_p, C.c_void_p, C.c_void_p)
        def bind(lib, name, restype, *args):
            fn = getattr(lib, name); fn.restype = restype; fn.argtypes = list(args); return fn
        bind(self.gtk, 'gtk_init_check', C.c_int, C.c_void_p, C.c_void_p)
        bind(self.gdk, 'gdk_atom_intern', C.c_void_p, C.c_char_p, C.c_int)
        bind(self.gtk, 'gtk_clipboard_get', C.c_void_p, C.c_void_p)
        bind(self.gtk, 'gtk_clipboard_request_contents', None, C.c_void_p, C.c_void_p, self.read_callback, C.c_void_p)
        bind(self.gtk, 'gtk_selection_data_get_length', C.c_int, C.c_void_p)
        bind(self.gtk, 'gtk_selection_data_get_format', C.c_int, C.c_void_p)
        bind(self.gtk, 'gtk_selection_data_get_data_type', C.c_void_p, C.c_void_p)
        bind(self.gtk, 'gtk_selection_data_get_data', C.c_void_p, C.c_void_p)
        bind(self.gtk, 'gtk_selection_data_set', None, C.c_void_p, C.c_void_p, C.c_int, C.c_void_p, C.c_int)
        bind(self.gtk, 'gtk_clipboard_set_with_data', C.c_int, C.c_void_p, C.POINTER(TargetEntry), C.c_uint, self.get_callback, self.clear_callback, C.c_void_p)
        bind(self.gtk, 'gtk_events_pending', C.c_int)
        bind(self.gtk, 'gtk_main_iteration_do', C.c_int, C.c_int)
        if not self.gtk.gtk_init_check(None, None):
            raise ClipboardError('desktop display unavailable')
        self.atom = self.gdk.gdk_atom_intern(b'UTF8_STRING', 0)
        self.clipboard = self.gtk.gtk_clipboard_get(self.gdk.gdk_atom_intern(b'CLIPBOARD', 0))
        self.owned = False
    def pump(self):
        # Bound each tick so a busy event source cannot starve validity checks.
        for _ in range(64):
            if not self.gtk.gtk_events_pending(): break
            self.gtk.gtk_main_iteration_do(0)
    def read(self):
        received = []
        def callback(_clipboard, selection, _data):
            n = self.gtk.gtk_selection_data_get_length(selection)
            if n < 0:
                received.append(None)
            elif n > LIMIT or self.gtk.gtk_selection_data_get_format(selection) != 8 or self.gtk.gtk_selection_data_get_data_type(selection) != self.atom:
                received.append(ClipboardError('invalid clipboard representation'))
            else:
                received.append(C.string_at(self.gtk.gtk_selection_data_get_data(selection), n) if n else b'')
        keep = self.read_callback(callback)
        self.gtk.gtk_clipboard_request_contents(self.clipboard, self.atom, keep, None)
        deadline = time.monotonic() + TIMEOUT
        while not received:
            self.pump()
            if time.monotonic() >= deadline: raise ClipboardError('clipboard timeout')
            time.sleep(0.005)
        if isinstance(received[0], Exception): raise received[0]
        return received[0]
    def claim(self, value):
        self.value = C.create_string_buffer(value)
        def get(_clipboard, selection, _info, _data):
            self.gtk.gtk_selection_data_set(selection, self.atom, 8, self.value, len(value))
        def clear(_clipboard, _data):
            self.owned = False
        self.callbacks = (self.get_callback(get), self.clear_callback(clear))
        entries = (TargetEntry * 1)(TargetEntry(b'UTF8_STRING', 0, 0))
        self.owned = bool(self.gtk.gtk_clipboard_set_with_data(self.clipboard, entries, 1, *self.callbacks, None))
        return self.owned
    def retain(self, check):
        next_check = time.monotonic()
        while self.owned:
            self.pump()
            if time.monotonic() >= next_check:
                check()
                next_check = time.monotonic() + 2
            time.sleep(0.01)


def detach_stdio():
    fd = os.open('/dev/null', os.O_RDWR)
    try:
        for target in (0, 1, 2): os.dup2(fd, target)
    finally:
        if fd > 2: os.close(fd)


def owner_write(value, session, deadline=None):
    if deadline is None:
        deadline = time.monotonic() + TIMEOUT
    if deadline <= time.monotonic():
        raise ClipboardError('clipboard timeout')
    reader, writer = os.pipe()
    lease_reader, lease_writer = os.pipe()
    try:
        pid = os.fork()
    except BaseException:
        for fd in (reader, writer, lease_reader, lease_writer): os.close(fd)
        raise ClipboardError('clipboard owner unavailable') from None
    if pid == 0:
        os.close(reader)
        # Only the operation parent holds the write end. Its death or any
        # cancellation path therefore makes the precommit lease readable EOF.
        os.close(lease_writer)
        status = b'error'
        claim_started = False
        def expired(_signum, _frame):
            raise ClipboardError('clipboard timeout', unknown=claim_started)
        def before_claim():
            nonlocal claim_started
            if time.monotonic() >= deadline:
                raise ClipboardError('clipboard timeout')
            ready, _, _ = select.select([lease_reader], [], [], 0)
            if ready:
                raise ClipboardError('clipboard operation cancelled')
            claim_started = True
        def on_claim():
            # Ownership lifetime begins at successful native claim. It must
            # survive normal parent exit, receipt loss and the operation timer.
            signal.setitimer(signal.ITIMER_REAL, 0)
            os.close(lease_reader)
        def acknowledge(receipt):
            try: os.write(writer, receipt)
            except OSError: pass
            finally: os.close(writer)
        try:
            signal.signal(signal.SIGALRM, expired)
            # POSIX interval timers are not inherited across fork: arm this
            # explicitly before potentially blocking GTK/session initialization.
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                raise ClipboardError('clipboard timeout')
            signal.setitimer(signal.ITIMER_REAL, remaining)
            os.setsid()
            # Closing SSH descriptors before claim means GTK diagnostics cannot leak.
            detach_stdio()
            desktop = GTKClipboard()
            try:
                # A failure inside native claim can occur after selection mutation.
                # The lease check runs after session inspection, immediately before
                # that first destination mutation; no clipboard operation retries.
                status = b'unknown'
                claim_value(desktop, value, session.check, before_claim, on_claim)
            except ClipboardError as error:
                acknowledge(b'unknown' if error.unknown else b'error')
                os._exit(1)
            acknowledge(b'ok')
            desktop.retain(session.check)
            os._exit(0)
        except BaseException:
            try: acknowledge(status)
            except OSError: pass
            os._exit(1)
    os.close(writer)
    os.close(lease_reader)
    try:
        remaining = max(0, deadline - time.monotonic())
        ready, _, _ = select.select([reader], [], [], remaining)
        receipt = os.read(reader, 16) if ready else b''
        if receipt == b'ok': return
        if receipt == b'error': raise ClipboardError('clipboard claim refused')
        # No retry: the child may have claimed even without a receipt.
        raise ClipboardError('clipboard outcome unknown', unknown=True)
    finally:
        # Closing the lease cancels every not-yet-started claim, including parent
        # exceptions. Committed owners have already dropped their lease reader.
        os.close(lease_writer)
        os.close(reader)
        try: os.waitpid(pid, os.WNOHANG)
        except ChildProcessError: pass


def drop_user():
    account = pwd.getpwnam('boxwarden')
    if account.pw_uid != UID or account.pw_dir != BASE_ENV['HOME']:
        raise ClipboardError('workstation account unavailable')
    if os.geteuid() == 0:
        os.initgroups(account.pw_name, account.pw_gid)
        os.setgid(account.pw_gid)
        os.setuid(UID)
    if os.geteuid() != UID or os.getuid() != UID:
        raise ClipboardError('workstation account required')


def operation_deadline(args):
    if len(args) not in (1, 3) or args[0] not in ('read', 'write'):
        raise ClipboardError('invalid clipboard operation')
    remaining = TIMEOUT
    if len(args) == 3:
        if args[1] != '--deadline-unix-ns' or not re.fullmatch(r'[1-9][0-9]{0,18}', args[2]):
            raise ClipboardError('invalid clipboard deadline')
        remaining = (int(args[2]) - time.time_ns()) / 1000000000
        if remaining <= 0:
            raise ClipboardError('clipboard timeout')
        remaining = min(TIMEOUT, remaining)
    return args[0], time.monotonic() + remaining


def main():
    dispatched = False
    def expired(_signum, _frame): raise ClipboardError('clipboard timeout', unknown=dispatched)
    signal.signal(signal.SIGALRM, expired)
    try:
        operation, deadline = operation_deadline(sys.argv[1:])
        remaining = deadline - time.monotonic()
        if remaining <= 0:
            raise ClipboardError('clipboard timeout')
        signal.setitimer(signal.ITIMER_REAL, remaining)
        value = read_input(sys.stdin.buffer) if operation == 'write' else None
        resource.setrlimit(resource.RLIMIT_AS, (1024 * 1024 * 1024, 1024 * 1024 * 1024))
        resource.setrlimit(resource.RLIMIT_CORE, (0, 0))
        resource.setrlimit(resource.RLIMIT_NOFILE, (64, 64))
        drop_user()
        session = Session()
        os.environ.clear(); os.environ.update(session.env)
        if operation == 'read':
            value = read_value(GTKClipboard(), session.check)
            write_frame(sys.stdout.buffer, 'ok', value)
        else:
            dispatched = True
            owner_write(value, session, deadline)
            write_frame(sys.stdout.buffer, 'ok', length=len(value))
        return 0
    except ClipboardError as error:
        write_frame(sys.stdout.buffer, 'unknown' if error.unknown else 'error')
        return 2 if error.unknown else 1
    except BaseException:
        try: write_frame(sys.stdout.buffer, 'unknown' if dispatched else 'error')
        except BaseException: pass
        return 2 if dispatched else 1
    finally:
        signal.setitimer(signal.ITIMER_REAL, 0)


if __name__ == '__main__':
    sys.exit(main())

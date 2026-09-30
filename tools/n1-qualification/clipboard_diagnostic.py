#!/usr/bin/python3
"""Clone-only overlay. Fixed metadata traces; unchanged production text frames.

Installed at the normal adapter path, this imports a preserved exact production
sibling. Collection never constructs GTKClipboard or requests clipboard text.
Guest reports are diagnostic observations, never trusted readiness authority.
"""
import ctypes as C
import hashlib
import json
import os
from pathlib import Path
import re
import signal
import stat
import subprocess
import sys
import time
import types

PRODUCTION = '/usr/local/libexec/boxwarden-guest-clipboard.production.py'
OVERLAY = '/usr/local/libexec/boxwarden-guest-clipboard.py'
PRODUCTION_SHA256 = 'e38530c45a2aab705be80aad3e9096f2fce9330cb6c0b5b6623a3d76403b4dbb'
DIRECTORY = '/run/boxwarden/n1-clipboard-diagnostic'
MAX_EVENTS = 64
SLOT_BYTES = 256
MAX_BYTES = MAX_EVENTS * SLOT_BYTES
WINDOW_SECONDS = 60
EVENTS = frozenset(('session_init', 'session_ready', 'session_failed', 'session_check',
    'session_check_ok', 'session_check_failed', 'binding', 'owner_pid', 'owner_start',
    'claim_begin', 'claim_ok', 'claim_refused', 'claim_failed', 'owner_get', 'owner_clear',
    'retain_begin', 'retain_return', 'retain_failed', 'read_begin', 'read_request',
    'read_complete', 'read_failed', 'text_absent', 'text_invalid', 'metadata_owner',
    'metadata_targets', 'metadata_unavailable', 'native_read_absent', 'native_read_invalid',
    'native_read_complete', 'native_read_failed', 'trace_saturated', 'frame_ok',
    'frame_error', 'frame_unknown', 'frame_failed'))


class DiagnosticRefused(Exception):
    def __init__(self): super().__init__('diagnostic metadata unavailable')


def valid_record(record):
    if type(record) is not dict or set(record) not in ({'event', 'at_ms'}, {'event', 'value', 'at_ms'}, {'event', 'digest', 'at_ms'}):
        return False
    if record.get('event') not in EVENTS:
        return False
    expected = {'event', 'digest', 'at_ms'} if record['event'] == 'binding' else ({'event', 'value', 'at_ms'} if record['event'] in ('owner_pid', 'owner_start', 'metadata_owner', 'metadata_targets') else {'event', 'at_ms'})
    if set(record) != expected:
        return False
    if type(record['at_ms']) is not int or not 0 <= record['at_ms'] <= 0x7fffffffffffffff:
        return False
    if 'value' in record and (type(record['value']) is not int or not 0 <= record['value'] <= 0xffffffff):
        return False
    if 'digest' in record and (record['event'] != 'binding' or type(record['digest']) is not str or not re.fullmatch('[a-f0-9]{64}', record['digest'])):
        return False
    if record['event'] == 'metadata_targets' and record['value'] > 1:
        return False
    return True


def metadata_ok(info, uid, gid):
    return stat.S_ISREG(info.st_mode) and info.st_uid == uid and info.st_gid == gid and stat.S_IMODE(info.st_mode) == 0o600 and info.st_nlink == 1 and info.st_size <= MAX_BYTES


def open_checked(path, uid, gid):
    try:
        before = os.lstat(path)
        fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
        opened, after = os.fstat(fd), os.lstat(path)
        if not all(metadata_ok(info, uid, gid) for info in (before, opened, after)) or (before.st_dev, before.st_ino) != (opened.st_dev, opened.st_ino) or (before.st_dev, before.st_ino) != (after.st_dev, after.st_ino):
            os.close(fd)
            raise DiagnosticRefused()
        return fd
    except OSError:
        raise DiagnosticRefused() from None


class FileTrace:
    @classmethod
    def create(cls, path, uid=0, gid=0):
        try:
            fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600)
            if not metadata_ok(os.fstat(fd), uid, gid):
                os.close(fd)
                raise DiagnosticRefused()
            return cls(fd)
        except OSError:
            raise DiagnosticRefused() from None
    def __init__(self, fd):
        self.fd, self.creator, self.count = fd, os.getpid(), 0
        self.until = time.monotonic() + WINDOW_SECONDS
    def event(self, event, **fields):
        if 'at_ms' in fields: return
        stamp = time.monotonic_ns() // 1000000
        record = dict(event=event, at_ms=stamp, **fields)
        child = os.getpid() != self.creator
        if child and not getattr(self, 'child_initialized', False):
            self.count = 0
            self.child_initialized = True
        if not valid_record(record) or self.count >= MAX_EVENTS // 2 or time.monotonic() >= self.until:
            return
        # Parent/read and forked owner have disjoint slots and inherited private
        # descriptors. Neither locks, appends indefinitely, nor retains payload.
        if self.count == MAX_EVENTS // 2 - 1:
            record = {'event': 'trace_saturated', 'at_ms': stamp}
        slot = self.count + (0 if not child else MAX_EVENTS // 2)
        encoded = json.dumps(record, separators=(',', ':'), sort_keys=True).encode('ascii') + b'\n'
        if len(encoded) > SLOT_BYTES:
            return
        self.count += 1
        try: os.pwrite(self.fd, encoded.ljust(SLOT_BYTES, b'\0'), slot * SLOT_BYTES)
        except OSError: pass
    def close(self): os.close(self.fd)


def collect_file(path, uid=0, gid=0):
    fd = open_checked(path, uid, gid)
    try:
        raw = os.read(fd, MAX_BYTES + 1)
        if len(raw) > MAX_BYTES or len(raw) % SLOT_BYTES:
            raise DiagnosticRefused()
        result = []
        def pairs(items):
            value = {}
            for key, item in items:
                if key in value: raise DiagnosticRefused()
                value[key] = item
            return value
        for offset in range(0, len(raw), SLOT_BYTES):
            chunk = raw[offset:offset + SLOT_BYTES].rstrip(b'\0')
            if not chunk: continue
            try:
                if not chunk.endswith(b'\n'): raise DiagnosticRefused()
                record = json.loads(chunk[:-1], object_pairs_hook=pairs)
            except (ValueError, UnicodeError):
                raise DiagnosticRefused() from None
            if not valid_record(record): raise DiagnosticRefused()
            result.append(record)
        return result
    finally:
        os.close(fd)


def binding_digest(session):
    # No payload, process environment dump, auth bytes, or argv dump is emitted.
    raw = json.dumps([session.identity, session.env.get('DISPLAY'), session.env.get('XAUTHORITY'), session.binding], separators=(',', ':')).encode('utf-8')
    if len(raw) > 4096: raise DiagnosticRefused()
    return hashlib.sha256(raw).hexdigest()


def owner_metadata():
    """Disposable worker's one native owner query; no GTK or text request."""
    library = C.CDLL('libX11.so.6')
    library.XOpenDisplay.argtypes = [C.c_char_p]
    library.XOpenDisplay.restype = C.c_void_p
    library.XInternAtom.argtypes = [C.c_void_p, C.c_char_p, C.c_int]
    library.XInternAtom.restype = C.c_ulong
    library.XGetSelectionOwner.argtypes = [C.c_void_p, C.c_ulong]
    library.XGetSelectionOwner.restype = C.c_ulong
    library.XCloseDisplay.argtypes = [C.c_void_p]
    library.XCloseDisplay.restype = C.c_int
    display = library.XOpenDisplay(None)
    if not display: raise DiagnosticRefused()
    try: return int(library.XGetSelectionOwner(display, library.XInternAtom(display, b'CLIPBOARD', 0)))
    finally: library.XCloseDisplay(display)


def bounded_owner(helper):
    env = helper.desktop_env({'DISPLAY': os.environ.get('DISPLAY', ''), 'XAUTHORITY': os.environ.get('XAUTHORITY', '')})
    try:
        result = subprocess.run(['/usr/bin/python3', '-I', '-S', OVERLAY, 'owner-metadata'],
            env=env, cwd='/', stdin=subprocess.DEVNULL, stdout=subprocess.PIPE,
            stderr=subprocess.DEVNULL, timeout=0.25, check=True)
        if len(result.stdout) > 11 or not re.fullmatch(rb'[0-9]{1,10}\n', result.stdout):
            raise DiagnosticRefused()
        owner = int(result.stdout)
        if owner > 0xffffffff: raise DiagnosticRefused()
        return owner
    except (OSError, subprocess.SubprocessError): raise DiagnosticRefused() from None


def native_metadata(helper, desktop):
    """Bounded owner worker plus one TARGETS query, never a text read."""
    owner = bounded_owner(helper)
    # TARGETS is protocol metadata. GTK bounds the target array before the
    # observer scans it. Callback memory belongs to GTK and is never retained.
    callback_type = C.CFUNCTYPE(None, C.c_void_p, C.POINTER(C.c_void_p), C.c_int, C.c_void_p)
    fn = desktop.gtk.gtk_clipboard_request_targets
    fn.argtypes = [C.c_void_p, callback_type, C.c_void_p]
    fn.restype = None
    answer = []
    def received(_clipboard, targets, count, _data):
        if count < 0 or count > 256 or (count and not targets): answer.append(None)
        else: answer.append(any(targets[i] == desktop.atom for i in range(count)))
    callback = callback_type(received)
    # Timed-out async callbacks must remain alive while GTK may still invoke
    # them. This overlay issues at most one metadata request per desktop.
    desktop._diagnostic_callback = callback
    fn(desktop.clipboard, callback, None)
    # The production operation alarm remains the outer bound. No retry and no
    # fresh thirty-second deadline is introduced for this metadata request.
    until = time.monotonic() + 0.25
    while not answer and time.monotonic() < until:
        desktop.pump()
        time.sleep(0.005)
    if not answer or answer[0] is None: raise DiagnosticRefused()
    return owner, answer[0]


def install(helper, trace, metadata=None):
    """Overlay existing entry points; driver supplies original module in tests."""
    def emit(event, **fields):
        try: trace.event(event, **fields)
        except helper.ClipboardError: raise
        except Exception: pass
    def probe(desktop, owner_only=False):
        try:
            if metadata is not None:
                owner, targets = metadata()
            elif owner_only:
                owner, targets = bounded_owner(helper), None
            else:
                owner, targets = native_metadata(helper, desktop)
            emit('metadata_owner', value=owner)
            if not owner_only: emit('metadata_targets', value=int(targets))
        except helper.ClipboardError: raise
        except Exception:
            emit('metadata_unavailable')
    session_init, session_check = helper.Session.__init__, helper.Session.check
    def init(self):
        emit('session_init')
        try:
            session_init(self)
            emit('binding', digest=binding_digest(self))
            emit('session_ready')
        except BaseException:
            emit('session_failed')
            raise
    def check(self):
        emit('session_check')
        try: session_check(self)
        except BaseException:
            emit('session_check_failed')
            raise
        emit('session_check_ok')
    helper.Session.__init__, helper.Session.check = init, check
    read_value = helper.read_value
    def read(desktop, check):
        emit('read_begin')
        probe(desktop)
        emit('read_request')
        try: value = read_value(desktop, check)
        except helper.ClipboardError as error:
            # Only fixed original messages select fixed codes; arbitrary errors
            # never become diagnostics, serialized strings, or payload previews.
            code = {'clipboard text absent': 'text_absent', 'invalid clipboard text': 'text_invalid'}.get(str(error), 'read_failed')
            emit(code)
            raise
        except BaseException:
            emit('read_failed')
            raise
        emit('read_complete')
        return value
    helper.read_value = read
    native_read = helper.GTKClipboard.read
    def gtk_read(self):
        try: value = native_read(self)
        except helper.ClipboardError as error:
            emit('native_read_invalid' if str(error) == 'invalid clipboard representation' else 'native_read_failed')
            raise
        except BaseException:
            emit('native_read_failed')
            raise
        emit('native_read_absent' if value is None else 'native_read_complete')
        return value
    helper.GTKClipboard.read = gtk_read
    claim = helper.GTKClipboard.claim
    def gtk_claim(self, value):
        emit('owner_pid', value=os.getpid())
        try:
            fields = Path('/proc/self/stat').read_bytes().rsplit(b') ', 1)[1].split()
            emit('owner_start', value=int(fields[19]))
        except BaseException: pass
        emit('claim_begin')
        get_type, clear_type = self.get_callback, self.clear_callback
        def get_factory(callback):
            def get(*args):
                emit('owner_get')
                return callback(*args)
            return get_type(get)
        def clear_factory(callback):
            def clear(*args):
                emit('owner_clear')
                return callback(*args)
            return clear_type(clear)
        self.get_callback, self.clear_callback = get_factory, clear_factory
        try: claimed = claim(self, value)
        except BaseException:
            emit('claim_failed')
            raise
        finally: self.get_callback, self.clear_callback = get_type, clear_type
        emit('claim_ok' if claimed else 'claim_refused')
        return claimed
    helper.GTKClipboard.claim = gtk_claim
    retain = helper.GTKClipboard.retain
    def keep(self, check):
        emit('retain_begin')
        # Original owner_write acknowledges first, then calls retain. All extra
        # writer native work therefore occurs after its original commit path.
        probe(self, owner_only=True)
        try: result = retain(self, check)
        except BaseException:
            emit('retain_failed')
            raise
        emit('retain_return')
        return result
    helper.GTKClipboard.retain = keep
    write_frame = helper.write_frame
    def frame(out, status, value=None, length=0):
        try: result = write_frame(out, status, value, length)
        except BaseException:
            emit('frame_failed')
            raise
        code = {'ok': 'frame_ok', 'error': 'frame_error', 'unknown': 'frame_unknown'}.get(status)
        if code is not None: emit(code)
        return result
    helper.write_frame = frame


def directory_check():
    try:
        for path, mode in (('/run', 0o755), ('/run/boxwarden', 0o700), (DIRECTORY, 0o700)):
            info = os.lstat(path)
            if not stat.S_ISDIR(info.st_mode) or info.st_uid != 0 or info.st_gid != 0 or stat.S_IMODE(info.st_mode) != mode:
                raise DiagnosticRefused()
    except OSError: raise DiagnosticRefused() from None


def main():
    try:
        if sys.argv[1:] == ['owner-metadata']:
            if os.getuid() != 1000 or os.geteuid() != 1000: raise DiagnosticRefused()
            signal.signal(signal.SIGALRM, signal.SIG_DFL)
            signal.setitimer(signal.ITIMER_REAL, 0.2)
            owner = owner_metadata()
            if not 0 <= owner <= 0xffffffff: raise DiagnosticRefused()
            sys.stdout.write(str(owner) + '\n')
            return 0
        if os.geteuid() != 0: raise DiagnosticRefused()
        directory_check()
        if sys.argv[1:] == ['collect']:
            result = {operation: collect_file(DIRECTORY + '/' + operation + '.trace') for operation in ('write', 'read')}
            sys.stdout.write(json.dumps(result, separators=(',', ':')) + '\n')
            return 0
        if len(sys.argv) != 4 or sys.argv[1] not in ('read', 'write') or sys.argv[2] != '--deadline-unix-ns':
            raise DiagnosticRefused()
        # The original adapter validates the precise deadline and owns all text
        # input/frames. Overlay admission is complete before it reads stdin.
        fd = os.open(PRODUCTION, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
        try:
            info = os.fstat(fd)
            if not stat.S_ISREG(info.st_mode) or info.st_uid != 0 or info.st_gid != 0 or stat.S_IMODE(info.st_mode) != 0o755 or info.st_nlink != 1 or info.st_size > 65536:
                raise DiagnosticRefused()
            raw = os.read(fd, 65537)
        finally: os.close(fd)
        if hashlib.sha256(raw).hexdigest() != PRODUCTION_SHA256: raise DiagnosticRefused()
        helper = types.ModuleType('retained_clipboard')
        exec(compile(raw, PRODUCTION, 'exec'), helper.__dict__)
        trace = FileTrace.create(DIRECTORY + '/' + sys.argv[1] + '.trace')
        install(helper, trace)
        try: return helper.main()
        finally: trace.close()
    except BaseException:
        # No raw path, exception, environment, or protocol bytes in stderr.
        sys.stderr.write('diagnostic metadata unavailable\n')
        return 1


if __name__ == '__main__':
    sys.exit(main())

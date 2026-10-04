#!/usr/bin/python3
"""Fixed installed support/desktop sanity checks. Never read or claim CLIPBOARD.

Receipts are guest cooperation evidence, not attestation against guest root.
Static preparation intentionally needs no graphical login. Runtime association
sanity complements the host's exact-generation action admission and receipt.
"""
import ctypes
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
import uuid

SUPPORT_VERSION = 1
LIBEXEC = Path('/usr/local/libexec')
ENV = {'PATH': '/usr/bin:/bin', 'HOME': '/root', 'LANG': 'C.UTF-8', 'LC_ALL': 'C.UTF-8', 'PYTHONDONTWRITEBYTECODE': '1'}
LIMIT = 16 * 1024 * 1024

class SupportError(Exception):
    pass


def ordinary_file(path, mode, maximum):
    try:
        fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
        with os.fdopen(fd, 'rb') as stream:
            before = os.fstat(stream.fileno())
            if not stat.S_ISREG(before.st_mode) or before.st_uid != 0 or before.st_gid != 0 or stat.S_IMODE(before.st_mode) != mode or before.st_nlink != 1 or not 0 < before.st_size <= maximum:
                raise SupportError('installed file metadata is unsafe')
            raw = stream.read(maximum + 1)
            after = os.fstat(stream.fileno())
            final = os.lstat(path)
            if (before.st_dev, before.st_ino, before.st_size, before.st_mtime_ns) != (after.st_dev, after.st_ino, after.st_size, after.st_mtime_ns) or (before.st_dev, before.st_ino) != (final.st_dev, final.st_ino) or len(raw) != before.st_size:
                raise SupportError('installed file changed during check')
            return raw
    except (OSError, ValueError):
        raise SupportError('installed file is missing or unavailable') from None


def check_file(path, expected):
    if re.fullmatch('[0-9a-f]{64}', expected) is None:
        raise SupportError('expected support digest is invalid')
    if hashlib.sha256(ordinary_file(path, 0o755, LIMIT)).hexdigest() != expected:
        raise SupportError('installed support digest differs; replace this system with the supported recipe')


def check_directories(paths, mode):
    for path in paths:
        try:
            info = os.lstat(path)
            if not stat.S_ISDIR(info.st_mode) or info.st_uid != 0 or info.st_gid != 0 or stat.S_IMODE(info.st_mode) != mode:
                raise SupportError('support directory metadata is unsafe')
        except OSError:
            raise SupportError('support directory is missing') from None


def check_dependencies():
    for library in ('libgtk-3.so.0', 'libgdk-3.so.0', 'libX11.so.6'):
        try:
            ctypes.CDLL(library)
        except (OSError, AttributeError):
            raise SupportError('required Python/GTK3/GDK/X11 dependency is unavailable') from None


def unique_object(pairs):
    value = {}
    for key, item in pairs:
        if key in value:
            raise SupportError('runtime support metadata is ambiguous')
        value[key] = item
    return value


def record(path, keys):
    raw = ordinary_file(path, 0o600, 4096)
    try:
        value = json.loads(raw.decode('utf-8'), object_pairs_hook=unique_object)
        if not isinstance(value, dict) or set(value) != set(keys) or raw != (json.dumps(value, separators=(',', ':'), ensure_ascii=False) + '\n').encode():
            raise SupportError('runtime support metadata is invalid')
        return value
    except (ValueError, UnicodeError):
        raise SupportError('runtime support metadata is invalid') from None


def check_runtime_binding():
    check_directories(['/etc', '/etc/ssh', '/etc/ssh/boxwarden', '/etc/ssh/boxwarden/active'], 0o755)
    check_directories(['/run'], 0o755)
    check_directories(['/run/boxwarden'], 0o700)
    association = ('domain', 'session_id', 'backend_kind', 'backend_object')
    marker = record('/run/boxwarden/clipboard-generation.json', ('version', *association, 'generation'))
    binding = record('/etc/ssh/boxwarden/active/management-binding.json', ('version', *association, 'ca_fingerprint', 'principal'))
    if type(marker['version']) is not int or marker['version'] != 1 or binding['version'] != 1 or any(marker[key] != binding[key] for key in association):
        raise SupportError('clipboard runtime association differs from management')
    try:
        if str(uuid.UUID(marker['generation'])) != marker['generation'] or str(uuid.UUID(marker['session_id'])) != marker['session_id']:
            raise ValueError()
    except (ValueError, TypeError, AttributeError):
        raise SupportError('clipboard runtime generation is invalid') from None


# Imported adapter bytes have already passed root ownership and digest checks.
# Session and GTK initialization do not read or claim selection contents.
DESKTOP_CHECK = """import importlib.util, os
spec = importlib.util.spec_from_file_location('boxwarden_clipboard', '/usr/local/libexec/boxwarden-guest-clipboard.py')
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)
module.drop_user()
session = module.Session()
os.environ.clear(); os.environ.update(session.env)
module.GTKClipboard()
session.check()
"""


def check_desktop():
    deadline = time.monotonic() + 25
    while time.monotonic() < deadline:
        try:
            result = subprocess.run(['/usr/bin/python3', '-I', '-B', '-c', DESKTOP_CHECK], cwd='/', env=ENV, stdin=subprocess.DEVNULL, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=min(8, deadline-time.monotonic()))
            if result.returncode == 0:
                return
        except (OSError, subprocess.TimeoutExpired):
            pass
        if time.monotonic() < deadline:
            time.sleep(min(1, deadline-time.monotonic()))
    raise SupportError('desktop support unavailable; inspect session action list to resolve the failed support attempt')


def main(args):
    if len(args) != 5 or args[0] not in ('--prepare', '--runtime') or args[1] != str(SUPPORT_VERSION) or any(re.fullmatch('[0-9a-f]{64}', item) is None for item in args[2:]):
        raise SupportError('expected prepare/runtime mode, compatibility version and three support digests')
    if os.geteuid() != 0:
        raise SupportError('support check requires guest root through the fixed recipe action')
    check_directories(['/usr', '/usr/local', '/usr/local/libexec'], 0o755)
    for name, digest in zip(('boxwarden-guest-bootstrap', 'boxwarden-guest-clipboard.py', 'boxwarden-guest-support-check'), args[2:]):
        check_file(LIBEXEC / name, digest)
    check_dependencies()
    if args[0] == '--runtime':
        check_runtime_binding()
        check_desktop()
        check_runtime_binding()
    return 0


if __name__ == '__main__':
    def expired(_signum, _frame): raise SupportError('support check exceeded its time bound')
    signal.signal(signal.SIGALRM, expired)
    signal.alarm(30)
    try:
        sys.exit(main(sys.argv[1:]))
    except SupportError as error:
        print('guest support check failed: ' + str(error), file=sys.stderr)
        sys.exit(1)
    except Exception:
        print('guest support check failed: support unavailable', file=sys.stderr)
        sys.exit(1)

#!/usr/bin/python3
"""Clone-only overlay. Fixed metadata traces; unchanged production text frames.

Installed at the normal adapter path, this imports a preserved exact production
sibling. Collection never constructs GTKClipboard or requests clipboard text.
Guest reports are diagnostic observations, never trusted readiness authority.
"""
import calendar
import datetime
import fcntl
import math
import select
import ctypes as C
import hashlib
import json
import os
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
BINDING_BYTES = 4096
CLOSURE_BYTES = 512
PROC_BYTES = 8192
BINDING_KEYS = frozenset(("version", "operation_id", "direction", "domain", "session_id", "backend_kind", "backend_object", "generation", "expires_at"))
UUID = r"[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}"
EVENTS = frozenset(('session_init', 'session_ready', 'session_failed', 'session_check',
    'session_check_ok', 'session_check_failed', 'binding', 'owner_pid', 'owner_start',
    'claim_begin', 'claim_ok', 'claim_refused', 'claim_failed', 'owner_get', 'owner_clear',
    'retain_begin', 'retain_return', 'retain_failed', 'read_begin', 'read_request',
    'read_complete', 'read_failed', 'text_absent', 'text_invalid', 'metadata_owner',
    'metadata_targets', 'metadata_unavailable', 'native_read_absent', 'native_read_invalid',
    'native_read_complete', 'native_read_failed', 'trace_saturated', 'frame_ok',
    'frame_error', 'frame_unknown', 'frame_failed', 'on_claim_ok', 'ack_ok',
    'ack_error', 'ack_unknown', 'ack_failed', 'owner_checkpoint', 'trace_begin',
    'trace_end', 'trace_incomplete', 'input_begin'))


class DiagnosticRefused(Exception):
    def __init__(self): super().__init__('diagnostic metadata unavailable')


def strict_json(raw, limit):
    def pairs(items):
        value = {}
        for key, item in items:
            if key in value: raise DiagnosticRefused()
            value[key] = item
        return value
    if type(raw) is not bytes or not 0 < len(raw) <= limit:
        raise DiagnosticRefused()
    try:
        return json.loads(raw.decode('ascii'), object_pairs_hook=pairs,
            parse_constant=lambda _value: (_ for _ in ()).throw(DiagnosticRefused()))
    except (ValueError, UnicodeError, RecursionError):
        raise DiagnosticRefused() from None


def expiry_ns(value):
    # Exact integer conversion of Go's UTC RFC3339Nano JSON encoding. No float
    # rounding, alternate timezone or independent operation budget is accepted.
    if type(value) is not str or len(value) > 30:
        raise DiagnosticRefused()
    match = re.fullmatch(r'(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2})(?:\.([0-9]{1,9}))?Z', value)
    if not match or (match[2] and match[2].endswith('0')):
        raise DiagnosticRefused()
    try:
        stamp = datetime.datetime.strptime(match[1], '%Y-%m-%dT%H:%M:%S')
        result = calendar.timegm(stamp.timetuple()) * 1000000000 + int((match[2] or '').ljust(9, '0'))
    except (ValueError, OverflowError):
        raise DiagnosticRefused() from None
    if not 0 < result <= 0x7fffffffffffffff: raise DiagnosticRefused()
    return result


def binding_shape(value):
    if type(value) is not dict or set(value) != BINDING_KEYS:
        raise DiagnosticRefused()
    if type(value['version']) is not int or value['version'] != 1 or value['domain'] != 'n1qualification' or value['backend_kind'] != 'tart' or value['direction'] not in ('read', 'write'):
        raise DiagnosticRefused()
    for field in ('operation_id', 'session_id', 'generation'):
        item = value[field]
        if type(item) is not str or not re.fullmatch(UUID, item) or item == '00000000-0000-0000-0000-000000000000':
            raise DiagnosticRefused()
    if type(value['backend_object']) is not str or not re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9_.-]{0,127}', value['backend_object']):
        raise DiagnosticRefused()
    expiry_ns(value['expires_at'])
    return value


def encode_binding(binding):
    binding_shape(binding)
    return json.dumps(binding, separators=(',', ':'), sort_keys=True).encode('ascii')


def parse_binding(raw, direction, expires_at, expected=None, now_ns=None):
    """Task3 helper must first compare all nine fields to its admitted request.

    fd3 is its dedicated metadata channel, never an authority inferred from
    stdin/environment. `expected` compares the full operation for collection or
    a caller that already holds the admitted binding. `expires_at` is the exact
    original canonical adapter argv UnixNano value.
    """
    value = binding_shape(strict_json(raw, BINDING_BYTES))
    now_ns = time.time_ns() if now_ns is None else now_ns
    if type(expires_at) is not int or type(now_ns) is not int or value['direction'] != direction or expiry_ns(value['expires_at']) != expires_at or not now_ns < expires_at <= now_ns + 30000000000:
        raise DiagnosticRefused()
    if expected is not None:
        binding_shape(expected)
        if value != expected: raise DiagnosticRefused()
    return value


def intake_metadata(until):
    """Internal fixed fd3 reader. Callers supply only a bounded local deadline."""
    try:
        if not stat.S_ISFIFO(os.fstat(3).st_mode): raise DiagnosticRefused()
        raw = bytearray()
        while True:
            remaining = until - time.monotonic()
            if not math.isfinite(remaining) or remaining <= 0 or not select.select([3], [], [], remaining)[0]:
                raise DiagnosticRefused()
            block = os.read(3, BINDING_BYTES + 1 - len(raw))
            if not block: break
            raw.extend(block)
            if len(raw) > BINDING_BYTES: raise DiagnosticRefused()
        if time.monotonic() >= until: raise DiagnosticRefused()
        return bytes(raw)
    except (OSError, ValueError, OverflowError):
        raise DiagnosticRefused() from None
    finally:
        try: os.close(3)
        except OSError: pass


def intake_binding(direction, expires_at):
    """Original expiry, exact fd3, no payload or fresh operation budget."""
    if type(expires_at) is not int: raise DiagnosticRefused()
    remaining = (expires_at - time.time_ns()) / 1000000000
    if not 0 < remaining <= 30: raise DiagnosticRefused()
    raw = intake_metadata(time.monotonic() + remaining)
    return parse_binding(raw, direction, expires_at)


def intake_collection(direction):
    """Separate 250ms metadata budget, same original binding/expiry, no transfer.

    Collection intentionally may follow operation expiry. It admits only the
    existing full binding; neither a new operation nor a revised expiry enters
    the receipt or any canonical transfer path.
    """
    raw = intake_metadata(time.monotonic() + 0.25)
    value = binding_shape(strict_json(raw, BINDING_BYTES))
    if value['direction'] != direction: raise DiagnosticRefused()
    return value


def admit_closure_output():
    """Fixed fd4 is an output-only unnamed nonblocking metadata pipe."""
    try:
        flags = fcntl.fcntl(4, fcntl.F_GETFL)
        if not stat.S_ISFIFO(os.fstat(4).st_mode) or flags & os.O_ACCMODE != os.O_WRONLY or not flags & os.O_NONBLOCK:
            raise DiagnosticRefused()
    except OSError: raise DiagnosticRefused() from None


def parse_closure(raw, header_digest, direction):
    """Proof from the outer root publisher; no new operation authority.

    It witnesses only the operation parent's trace descriptor close. The live
    detached owner may retain its disjoint lane and descriptor after this point.
    """
    value = strict_json(raw, CLOSURE_BYTES)
    if type(value) is not dict or set(value) != {'version','header_digest','direction','elapsed_ms'}:
        raise DiagnosticRefused()
    if type(value['version']) is not int or value['version'] != 1 or value['direction'] not in ('read','write') or value['direction'] != direction or type(value['header_digest']) is not str or not re.fullmatch('[a-f0-9]{64}',value['header_digest']) or value['header_digest'] != header_digest or type(value['elapsed_ms']) is not int or not 0 <= value['elapsed_ms'] < 60000:
        raise DiagnosticRefused()
    canonical = json.dumps(value, sort_keys=True, separators=(',', ':')).encode('ascii') + b'\n'
    if raw != canonical: raise DiagnosticRefused()
    return value


def collect_closure(direction, header_digest):
    """Only the fixed root-published file, after helper EOF/reader-close proof.

    Task3a must implement that outer producer and no-overwrite namespace seal;
    the UID1000 adapter cannot publish or repair this root-owned record.
    """
    if direction not in ('read','write'): raise DiagnosticRefused()
    path = DIRECTORY + '/' + direction + '.closure'
    fd = open_checked(path, 0, 0, CLOSURE_BYTES)
    try:
        before = file_signature(os.fstat(fd))
        raw = os.read(fd, CLOSURE_BYTES + 1)
        proof = parse_closure(raw, header_digest, direction)
        if file_signature(os.fstat(fd)) != before or file_signature(os.lstat(path)) != before or os.pread(fd, CLOSURE_BYTES + 1, 0) != raw:
            raise DiagnosticRefused()
    except OSError: raise DiagnosticRefused() from None
    finally:
        try: os.close(fd)
        except OSError: raise DiagnosticRefused() from None
    return proof


def valid_record(record):
    if type(record) is not dict or type(record.get('event')) is not str or record['event'] not in EVENTS:
        return False
    expected = {'event', 'at_ms', 'header_digest'}
    if record['event'] == 'binding': expected.add('digest')
    if record['event'] in ('owner_pid', 'owner_start', 'metadata_owner', 'metadata_targets'): expected.add('value')
    if set(record) != expected or type(record['at_ms']) is not int or not 0 <= record['at_ms'] < 60000:
        return False
    if type(record['header_digest']) is not str or not re.fullmatch('[a-f0-9]{64}', record['header_digest']):
        return False
    if 'value' in record:
        maximum = 0xffffffffffffffff if record['event'] == 'owner_start' else 0xffffffff
        if type(record['value']) is not int or not 0 <= record['value'] <= maximum: return False
        if record['event'] in ('owner_pid', 'owner_start') and record['value'] == 0: return False
        if record['event'] == 'metadata_targets' and record['value'] > 1: return False
    if 'digest' in record and (type(record['digest']) is not str or not re.fullmatch('[a-f0-9]{64}', record['digest'])):
        return False
    return True


def metadata_ok(info, uid, gid, limit=MAX_BYTES):
    return stat.S_ISREG(info.st_mode) and info.st_uid == uid and info.st_gid == gid and stat.S_IMODE(info.st_mode) == 0o600 and info.st_nlink == 1 and 0 <= info.st_size <= limit


def open_checked(path, uid, gid, limit=MAX_BYTES):
    fd = None
    try:
        before = os.lstat(path)
        fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
        opened, after = os.fstat(fd), os.lstat(path)
        if not all(metadata_ok(info, uid, gid, limit) for info in (before, opened, after)) or (before.st_dev, before.st_ino) != (opened.st_dev, opened.st_ino) or (before.st_dev, before.st_ino) != (after.st_dev, after.st_ino):
            raise DiagnosticRefused()
        return fd
    except (OSError, DiagnosticRefused):
        if fd is not None: os.close(fd)
        raise DiagnosticRefused() from None


class FileTrace:
    @classmethod
    def create(cls, path, binding, uid=0, gid=0):
        raw = encode_binding(binding)
        fd = None
        try:
            fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600)
            if not metadata_ok(os.fstat(fd), uid, gid): raise DiagnosticRefused()
            return cls(fd, hashlib.sha256(raw).hexdigest(), binding['direction'])
        except (OSError, DiagnosticRefused):
            if fd is not None: os.close(fd)
            raise DiagnosticRefused() from None
    def __init__(self, fd, header_digest, direction):
        self.fd, self.creator, self.count = fd, os.getpid(), 0
        self.origin = time.monotonic()
        self.direction, self.finished, self.closure_admitted = direction, False, False
        self.header_digest, self.failed, self.last_ms, self.last_delta = header_digest, False, 0, 0.0
    def _lane(self):
        child = os.getpid() != self.creator
        if child and not getattr(self, 'child_initialized', False):
            self.count, self.failed, self.last_ms, self.last_delta = 0, False, 0, 0.0
            self.child_initialized = True
        return MAX_EVENTS // 2 if child else 0
    def _stamp(self):
        delta = time.monotonic() - self.origin
        if not math.isfinite(delta) or delta < self.last_delta or delta >= WINDOW_SECONDS:
            raise DiagnosticRefused()
        stamp = int(delta * 1000)
        if stamp < self.last_ms: raise DiagnosticRefused()
        self.last_ms, self.last_delta = stamp, delta
        return stamp
    def _write(self, record, slot):
        encoded = json.dumps(record, separators=(',', ':'), sort_keys=True).encode('ascii') + b'\n'
        if not valid_record(record) or len(encoded) > SLOT_BYTES:
            raise DiagnosticRefused()
        if os.pwrite(self.fd, encoded.ljust(SLOT_BYTES, b'\0'), slot * SLOT_BYTES) != SLOT_BYTES:
            raise DiagnosticRefused()
    def invalidate(self):
        lane = self._lane()
        self.failed = True
        if self.fd is None: return
        try:
            self._write({'event':'trace_incomplete', 'at_ms':self.last_ms,
                'header_digest':self.header_digest}, lane + MAX_EVENTS // 2 - 1)
        except (OSError, DiagnosticRefused): pass
    def event(self, event, **fields):
        lane = self._lane()
        try:
            if self.failed or self.count >= MAX_EVENTS // 2 - 1:
                raise DiagnosticRefused()
            record = dict(event=event, at_ms=self._stamp(), header_digest=self.header_digest, **fields)
            self._write(record, lane + self.count)
            self.count += 1
        except (OSError, DiagnosticRefused, TypeError, ValueError, OverflowError):
            self.invalidate()
            raise DiagnosticRefused() from None
    def finish(self):
        # Finalization is within the quota/window. Check the time after actual
        # sink sync; a clock sampled before blocking IO cannot certify closure.
        try:
            os.fsync(self.fd)
            self.event('trace_end')
            os.fsync(self.fd)
            self._stamp()
            self.finished = not self.failed
        except (OSError, DiagnosticRefused): self.invalidate()
    def close(self):
        if self.fd is None: return
        fd, self.fd = self.fd, None
        parent_proof = self.closure_admitted and os.getpid() == self.creator
        try:
            # Once close is attempted, never reuse the ambiguous/released fd.
            os.close(fd)
            elapsed_ms = self._stamp()
            if parent_proof:
                if not self.finished or self.failed: raise DiagnosticRefused()
                proof = {'version':1, 'header_digest':self.header_digest,
                    'direction':self.direction, 'elapsed_ms':elapsed_ms}
                raw = json.dumps(proof, sort_keys=True, separators=(',', ':')).encode('ascii') + b'\n'
                # POSIX PIPE_BUF >=512. One nonblocking atomic attempt, no retry.
                if len(raw) > CLOSURE_BYTES or os.write(4, raw) != len(raw):
                    raise DiagnosticRefused()
        except (OSError, DiagnosticRefused):
            self.failed = True
            raise DiagnosticRefused() from None
        finally:
            if parent_proof:
                try: os.close(4)
                except OSError: pass



def collect_file(path, header_digest, uid=0, gid=0):
    fd = open_checked(path, uid, gid)
    try:
        before = os.fstat(fd)
        raw = os.read(fd, MAX_BYTES + 1)
        if len(raw) > MAX_BYTES or len(raw) % SLOT_BYTES:
            raise DiagnosticRefused()
        result = []
        for offset in range(0, len(raw), SLOT_BYTES):
            chunk = raw[offset:offset + SLOT_BYTES].rstrip(b'\0')
            if not chunk: continue
            if not chunk.endswith(b'\n'): raise DiagnosticRefused()
            record = strict_json(chunk[:-1], SLOT_BYTES)
            if not valid_record(record) or record['header_digest'] != header_digest:
                raise DiagnosticRefused()
            result.append(dict(record, lane='parent' if offset < MAX_BYTES // 2 else 'owner'))
        after = os.fstat(fd)
        if (before.st_dev, before.st_ino, before.st_size, before.st_mtime_ns, before.st_ctime_ns) != (after.st_dev, after.st_ino, after.st_size, after.st_mtime_ns, after.st_ctime_ns) or os.pread(fd, MAX_BYTES + 1, 0) != raw:
            raise DiagnosticRefused()
        # Per-writer slots are contiguous and monotonic; no mid-lane holes.
        for lane in (0, MAX_EVENTS // 2):
            seen_empty, last = False, -1
            for slot in range(lane, lane + MAX_EVENTS // 2):
                chunk = raw[slot*SLOT_BYTES:(slot+1)*SLOT_BYTES].rstrip(b'\0')
                if not chunk:
                    seen_empty = True; continue
                value = strict_json(chunk[:-1], SLOT_BYTES)
                if (seen_empty and value['event'] != 'trace_incomplete') or value['at_ms'] < last:
                    raise DiagnosticRefused()
                last = value['at_ms']
        return result
    finally:
        os.close(fd)


def trace_complete(records, direction):
    """Successful finite prefixes, keeping independent parent/owner order.

    Lane is derived by collect_file from the physical slot, never slot JSON.
    Parent frame/end and owner retain records can interleave in real time.
    """
    if direction not in ('read', 'write') or not records: return False
    lanes = {'parent': [], 'owner': []}
    for record in records:
        if type(record) is not dict or record.get('lane') not in lanes: return False
        slot = {key:value for key,value in record.items() if key != 'lane'}
        if not valid_record(slot): return False
        lanes[record['lane']].append(record)
    parent = [r['event'] for r in lanes['parent']]
    owner = [r['event'] for r in lanes['owner']]
    if direction == 'read':
        expected = ['trace_begin','session_init','binding','session_ready','read_begin',
            'metadata_owner','metadata_targets','read_request','session_check','session_check_ok',
            'native_read_complete','session_check','session_check_ok','read_complete','frame_ok','trace_end']
        return parent == expected and not owner
    if parent != ['trace_begin','input_begin','session_init','binding','session_ready','frame_ok','trace_end']:
        return False
    required = ['session_check','session_check_ok','owner_pid','owner_start','claim_begin',
        'claim_ok','on_claim_ok','session_check','session_check_ok','ack_ok','retain_begin',
        'metadata_owner','owner_checkpoint']
    # Native get callbacks carry no progression authority and can run during
    # claim/retention. They are allowed only after claim_begin, within slot quota.
    core = []
    for code in owner:
        if code == 'owner_get':
            if 'claim_begin' not in core: return False
        else: core.append(code)
    if core[:len(required)] != required: return False
    tail = core[len(required):]
    if len(tail) % 2 or tail != ['session_check','session_check_ok'] * (len(tail)//2):
        return False
    native = [r for r in lanes['owner'] if r['event'] == 'metadata_owner']
    return len(native) == 1 and native[0]['value'] != 0


def read_proc_metadata(path, limit):
    """Internal fixed exact-pid stat/status reader, never a process inventory."""
    fd = None
    try:
        fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC | os.O_NONBLOCK)
        raw = os.read(fd, limit + 1)
        if not 0 < len(raw) <= limit: raise DiagnosticRefused()
        return raw
    except OSError: raise DiagnosticRefused() from None
    finally:
        if fd is not None: os.close(fd)


def stat_identity(raw, pid):
    if type(raw) is not bytes or not 0 < len(raw) <= PROC_BYTES: raise DiagnosticRefused()
    try:
        prefix, rest = raw.rsplit(b') ', 1)
        first, separator, _name = prefix.partition(b' (')
        fields = rest.split()
        if not separator or first != str(pid).encode() or len(fields) < 20 or fields[0] not in (b'R',b'S',b'D',b'I',b'T',b't') or not re.fullmatch(rb'[1-9][0-9]{0,19}', fields[19]):
            raise DiagnosticRefused()
        ticks = int(fields[19])
        if not 0 < ticks <= 0xffffffffffffffff: raise DiagnosticRefused()
        return ticks
    except (ValueError, IndexError): raise DiagnosticRefused() from None


def status_uid(raw):
    if type(raw) is not bytes or not 0 < len(raw) <= PROC_BYTES: raise DiagnosticRefused()
    values = [line for line in raw.splitlines() if line.startswith(b'Uid:')]
    if len(values) != 1 or not re.fullmatch(rb'Uid:[ \t]+1000[ \t]+1000[ \t]+1000[ \t]+1000', values[0]):
        raise DiagnosticRefused()


def collect_owner(pid, start_ticks):
    if type(pid) is not int or not 0 < pid <= 0xffffffff or type(start_ticks) is not int or not 0 < start_ticks <= 0xffffffffffffffff:
        raise DiagnosticRefused()
    base = '/proc/' + str(pid)
    origin = time.monotonic()
    before = stat_identity(read_proc_metadata(base + '/stat', PROC_BYTES), pid)
    status_uid(read_proc_metadata(base + '/status', PROC_BYTES))
    after = stat_identity(read_proc_metadata(base + '/stat', PROC_BYTES), pid)
    status_uid(read_proc_metadata(base + '/status', PROC_BYTES))
    elapsed = time.monotonic() - origin
    if before != start_ticks or after != start_ticks or not math.isfinite(elapsed) or not 0 <= elapsed < 0.25:
        raise DiagnosticRefused()
    return {'pid':pid, 'start_ticks':start_ticks, 'uid':1000, 'state':'live'}


def self_owner():
    pid = os.getpid()
    return pid, stat_identity(read_proc_metadata('/proc/' + str(pid) + '/stat', PROC_BYTES), pid)


def create_operation(binding):
    directory_check()
    raw = encode_binding(binding)
    path = DIRECTORY + '/' + binding['direction']
    fd = None
    try:
        fd = os.open(path + '.binding', os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600)
        if not metadata_ok(os.fstat(fd), 0, 0, BINDING_BYTES) or os.write(fd, raw) != len(raw):
            raise DiagnosticRefused()
        os.fsync(fd)
    except OSError: raise DiagnosticRefused() from None
    finally:
        if fd is not None: os.close(fd)
    # Header is created once and never reopened for mutation; slots reference
    # the hash of these exact separately quota-bound canonical bytes.
    trace = FileTrace.create(path + '.trace', binding)
    try: trace.event('trace_begin')
    except DiagnosticRefused:
        trace.close(); raise
    return trace


def file_signature(info):
    return (info.st_dev, info.st_ino, info.st_size, info.st_mtime_ns, info.st_ctime_ns)


def collect_operation(expected):
    """Fixed paths, exact original operation; finite stable progression prefix.

    Serving-owner completeness covers observed acknowledgement and initial
    retain checkpoint plus collection-time PID/birth/UID liveness. It never
    certifies future retention, text delivery or a full owner lifetime.
    """
    binding_shape(expected)
    directory_check()
    origin = time.monotonic()
    path = DIRECTORY + '/' + expected['direction']
    header_fd, trace_fd = None, None
    close_failed = False
    try:
        header_fd = open_checked(path + '.binding', 0, 0, BINDING_BYTES)
        header_info = file_signature(os.fstat(header_fd))
        raw = os.read(header_fd, BINDING_BYTES + 1)
        actual = binding_shape(strict_json(raw, BINDING_BYTES))
        if actual != expected or raw != encode_binding(expected): raise DiagnosticRefused()
        digest = hashlib.sha256(raw).hexdigest()
        trace_fd = open_checked(path + '.trace', 0, 0)
        trace_info = file_signature(os.fstat(trace_fd))
        records = collect_file(path + '.trace', digest)
        complete = trace_complete(records, expected['direction'])
        closure = None
        try:
            closure_signature = file_signature(os.lstat(path + '.closure'))
            closure = collect_closure(expected['direction'], digest)
            ends = [r['at_ms'] for r in records if r['lane'] == 'parent' and r['event'] == 'trace_end']
            if len(ends) != 1 or ends[0] > closure['elapsed_ms']: complete = False
            if collect_closure(expected['direction'], digest) != closure or file_signature(os.lstat(path + '.closure')) != closure_signature: complete = False
        except (DiagnosticRefused, OSError): complete = False
        owner = None
        owner_code = 'not_applicable'
        if expected['direction'] == 'write':
            pids = [r['value'] for r in records if r['event'] == 'owner_pid']
            ticks = [r['value'] for r in records if r['event'] == 'owner_start']
            if len(pids) == len(ticks) == 1:
                try:
                    owner = collect_owner(pids[0], ticks[0]); owner_code = 'live'
                except DiagnosticRefused: owner_code = 'unverifiable'; complete = False
            else: owner_code = 'unverifiable'; complete = False
            if any(r['event'] in ('owner_clear','retain_return','retain_failed') for r in records):
                owner_code = 'retention_ended'; complete = False
            checkpoints = [r for r in records if r['event'] == 'metadata_owner']
            if len(checkpoints) != 1 or checkpoints[0]['value'] == 0: complete = False
        # Keep both originally opened descriptors throughout collection. Exact
        # bytes alone cannot detect an identical-content inode replacement.
        if collect_file(path + '.trace', digest) != records: complete = False
        if (file_signature(os.fstat(header_fd)) != header_info or file_signature(os.lstat(path + '.binding')) != header_info or os.pread(header_fd, BINDING_BYTES + 1, 0) != raw):
            raise DiagnosticRefused()
        if (file_signature(os.fstat(trace_fd)) != trace_info or file_signature(os.lstat(path + '.trace')) != trace_info):
            raise DiagnosticRefused()
        before_close = time.monotonic()
        receipt = {'version':1, 'binding':actual, 'header_digest':digest,
            'coverage_scope':'collected_progression_prefix', 'complete':complete,
            'owner_code':owner_code, 'owner':owner, 'records':records, 'closure':closure}
    except OSError: raise DiagnosticRefused() from None
    finally:
        for fd in (trace_fd, header_fd):
            if fd is not None:
                try: os.close(fd)
                except OSError: close_failed = True
    after_close = time.monotonic()
    if close_failed or not all(math.isfinite(stamp) for stamp in (origin,before_close,after_close)) or not origin <= before_close <= after_close < origin + 0.25:
        receipt['complete'] = False
    return receipt


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
    """Wrap the exact retained adapter; production frame and native call stay exact.

    Only helper.os is proxied during owner_write, never the process-wide os
    module. The proxy observes successful writes to the first canonical private
    pipe writer and delegates every call unchanged. It makes no additional pipe,
    fork, native query or acknowledgement. This is a diagnostic timing change.
    """
    state = {'committed':False, 'ack':False, 'read_started':False, 'closure_child_refused':False}
    def invalidate():
        try: trace.invalidate()
        except BaseException: pass
    def emit(event, strict=False, **fields):
        try: trace.event(event, **fields)
        except BaseException:
            invalidate()
            if strict and not state['committed'] and not state['read_started']: raise DiagnosticRefused() from None
    def probe(desktop, owner_only=False):
        try:
            if metadata is not None:
                owner, targets = metadata()
            elif owner_only:
                owner, targets = bounded_owner(helper), None
            else:
                owner, targets = native_metadata(helper, desktop)
            if type(owner) is not int or not 0 <= owner <= 0xffffffff or (not owner_only and type(targets) is not bool):
                raise DiagnosticRefused()
            emit('metadata_owner', strict=not state['committed'], value=owner)
            if not owner_only: emit('metadata_targets', strict=True, value=int(targets))
            return True
        except helper.ClipboardError:
            if not state['committed']: raise
            emit('metadata_unavailable'); invalidate(); return False
        except BaseException:
            emit('metadata_unavailable'); invalidate()
            if not state['committed']: raise DiagnosticRefused() from None
            return False
    read_input = helper.read_input
    def input_value(stream):
        emit('input_begin', strict=True)
        return read_input(stream)
    helper.read_input = input_value
    session_init, session_check = helper.Session.__init__, helper.Session.check
    def init(self):
        emit('session_init', strict=True)
        try:
            session_init(self)
            emit('binding', strict=True, digest=binding_digest(self))
            emit('session_ready', strict=True)
        except BaseException:
            emit('session_failed')
            raise
    def check(self):
        emit('session_check', strict=True)
        try: session_check(self)
        except BaseException:
            emit('session_check_failed')
            raise
        emit('session_check_ok', strict=True)
    helper.Session.__init__, helper.Session.check = init, check
    read_value = helper.read_value
    def read(desktop, check):
        emit('read_begin', strict=True)
        probe(desktop)
        emit('read_request', strict=True)
        class Read:
            def read(self):
                state['read_started'] = True
                return desktop.read()
        try: value = read_value(Read(), check)
        except helper.ClipboardError as error:
            code = {'clipboard text absent':'text_absent', 'invalid clipboard text':'text_invalid'}.get(str(error), 'read_failed')
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
        try: return claim(self, value)
        finally: self.get_callback, self.clear_callback = get_type, clear_type
    helper.GTKClipboard.claim = gtk_claim
    claim_value = helper.claim_value
    def claim_observed(desktop, value, check, before_claim=None, on_claim=None):
        def before():
            if state['closure_child_refused']:
                raise helper.ClipboardError('clipboard diagnostic unavailable')
            try:
                pid, ticks = self_owner()
                emit('owner_pid', strict=True, value=pid)
                emit('owner_start', strict=True, value=ticks)
                emit('claim_begin', strict=True)
            except DiagnosticRefused:
                # This hook is outside the canonical native-claim try scope.
                # The original owner therefore acknowledges explicit refusal.
                raise helper.ClipboardError('clipboard diagnostic unavailable') from None
            # Keep the original lease/deadline check immediately before native
            # mutation; diagnostic IO must not widen that cancellation race.
            if before_claim is not None: before_claim()
        class Claim:
            def claim(self, original):
                try: result = desktop.claim(original)
                except BaseException:
                    emit('claim_failed'); raise
                # Native success is already irreversible. No recording failure
                # may prevent canonical on_claim timer/lease bookkeeping.
                state['committed'] = bool(result)
                emit('claim_ok' if result else 'claim_refused')
                return result
        def on():
            if on_claim is not None: on_claim()
            emit('on_claim_ok')
        return claim_value(Claim(), value, check, before, on)
    helper.claim_value = claim_observed
    retain = helper.GTKClipboard.retain
    def keep(self, check):
        emit('retain_begin')
        if not state['ack']: invalidate()
        if probe(self, owner_only=True): emit('owner_checkpoint')
        try: result = retain(self, check)
        except BaseException:
            emit('retain_failed'); raise
        emit('retain_return')
        return result
    helper.GTKClipboard.retain = keep
    owner_write = helper.owner_write
    def owner(value, session, deadline=None):
        original_os = helper.os
        class AckOS:
            def __init__(self): self.writer, self.pipes = None, 0
            def __getattr__(self, name): return getattr(original_os, name)
            def pipe(self):
                pair = original_os.pipe()
                if self.pipes == 0: self.writer = pair[1]
                self.pipes += 1
                return pair
            def fork(self):
                pid = original_os.fork()
                if pid == 0 and getattr(trace, 'closure_admitted', False):
                    trace.closure_admitted = False
                    try: original_os.close(4)
                    except OSError:
                        state['closure_child_refused'] = True
                        invalidate()
                return pid
            def write(self, fd, data):
                observed = fd == self.writer and data in (b'ok', b'error', b'unknown')
                try: result = original_os.write(fd, data)
                except BaseException:
                    if observed: emit('ack_failed'); invalidate()
                    raise
                if observed:
                    if result != len(data): emit('ack_failed'); invalidate()
                    else:
                        state['ack'] = data == b'ok'
                        emit({b'ok':'ack_ok', b'error':'ack_error', b'unknown':'ack_unknown'}[data])
                return result
        helper.os = AckOS()
        try: return owner_write(value, session, deadline)
        finally: helper.os = original_os
    helper.owner_write = owner
    write_frame = helper.write_frame
    def frame(out, status, value=None, length=0):
        try: result = write_frame(out, status, value, length)
        except BaseException:
            emit('frame_failed'); raise
        code = {'ok':'frame_ok','error':'frame_error','unknown':'frame_unknown'}.get(status)
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
    closure_output = False
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
        if len(sys.argv) == 3 and sys.argv[1] == 'collect' and sys.argv[2] in ('write', 'read'):
            # Collection expiry is the original operation expiry; collection
            # itself has a separate bounded metadata budget, never a transfer.
            expected = intake_collection(sys.argv[2])
            result = collect_operation(expected)
            sys.stdout.write(json.dumps(result, separators=(',', ':')) + '\n')
            return 0
        if len(sys.argv) != 4 or sys.argv[1] not in ('read', 'write') or sys.argv[2] != '--deadline-unix-ns':
            raise DiagnosticRefused()
        if not re.fullmatch(r'[1-9][0-9]{0,18}', sys.argv[3]): raise DiagnosticRefused()
        signal.signal(signal.SIGALRM, signal.SIG_DFL)
        remaining = (int(sys.argv[3]) - time.time_ns()) / 1000000000
        if not 0 < remaining <= 30: raise DiagnosticRefused()
        signal.setitimer(signal.ITIMER_REAL, remaining)
        admit_closure_output()
        closure_output = True
        binding = intake_binding(sys.argv[1], int(sys.argv[3]))
        # Original argv, deadline, payload input and public frames stay exact.
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
        trace = create_operation(binding)
        trace.closure_admitted = True
        install(helper, trace)
        outcome = helper.main()
        try:
            trace.finish()
        except BaseException:
            try: trace.invalidate()
            except BaseException: pass
        finally:
            try: trace.close()
            except BaseException:
                try: trace.invalidate()
                except BaseException: pass
        return outcome
    except BaseException:
        if closure_output:
            try: os.close(4)
            except OSError: pass
        # No raw path, exception, environment, or protocol bytes in stderr.
        sys.stderr.write('diagnostic metadata unavailable\n')
        return 1


if __name__ == '__main__':
    sys.exit(main())

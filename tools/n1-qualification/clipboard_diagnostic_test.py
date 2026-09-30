import hashlib
import ctypes
import contextlib
import fcntl
import select
import importlib.util
import io
import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest import mock

ROOT = Path(__file__).resolve().parents[2]
PATH = Path(__file__).with_name('clipboard_diagnostic.py')


def load(path, name):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


class DiagnosticTests(unittest.TestCase):
    def setUp(self):
        self.assertTrue(PATH.exists(), 'trial diagnostic overlay is missing')
        self.diag = load(PATH, 'diagnostic')
        self.helper = load(ROOT / 'guest/ubuntu-24.04-arm64/clipboard.py', 'retained_helper')
        self.owner_patch=mock.patch.object(self.diag,'self_owner',return_value=(77,5000000000),create=True)
        self.owner_patch.start();self.addCleanup(self.owner_patch.stop)

    def trace(self):
        directory = tempfile.TemporaryDirectory()
        self.addCleanup(directory.cleanup)
        path = Path(directory.name) / 'trace'
        sink = self.diag.FileTrace.create(path, binding=BINDING, uid=os.getuid(), gid=os.getgid())
        self.addCleanup(sink.close)
        outer = self
        class Trace:
            def event(self, *args, **kwargs): sink.event(*args, **kwargs)
            def invalidate(self): sink.invalidate()
            def finish(self): sink.finish()
            @property
            def records(self): return outer.diag.collect_file(path, header_digest=BINDING_DIGEST, uid=os.getuid(), gid=os.getgid())
        return Trace()

    def test_trace_strict_codes_never_serializes_payload_or_exception(self):
        trace = self.trace()
        trace.event('metadata_owner', value=1)
        for code,fields in (('synthetic private payload',{'value':1}),('read_request',{'value':'synthetic private payload'})):
            with self.assertRaises(self.diag.DiagnosticRefused):trace.event(code,**fields)
        self.assertEqual([{k: v for k, v in r.items() if k not in ('at_ms','header_digest','lane')} for r in trace.records], [{'event': 'metadata_owner', 'value': 1}, {'event':'trace_incomplete'}])
        self.assertNotIn('private', json.dumps(trace.records))

    def test_binding_digest_stable_and_changes_with_xwayland_generation(self):
        session = type('Session', (), {'identity': '3', 'env': {'DISPLAY': ':0', 'XAUTHORITY': '/run/user/1000/auth'},
            'binding': (':1.4', 4, 40, 5, 50, ('/usr/bin/Xwayland', ':0', '-auth', '/run/user/1000/auth'))})()
        before = self.diag.binding_digest(session)
        self.assertRegex(before, r'^[a-f0-9]{64}$')
        self.assertEqual(before, self.diag.binding_digest(session))
        session.binding = (':1.4', 4, 40, 5, 51, session.binding[-1])
        self.assertNotEqual(before, self.diag.binding_digest(session))

    def test_first_read_hooks_preserve_bytes_and_do_not_retry(self):
        trace = self.trace()
        desktop = type('Desktop', (), {'read': lambda self: b'synthetic private payload'})()
        original = desktop.read
        calls = []
        desktop.read = lambda: (calls.append(1), original())[1]
        self.diag.install(self.helper, trace, metadata=lambda: (17, True))
        self.assertEqual(self.helper.read_value(desktop, lambda: None), b'synthetic private payload')
        self.assertEqual(calls, [1])
        events = [r['event'] for r in trace.records]
        self.assertIn('read_complete', events)
        self.assertNotIn('private', json.dumps(trace.records))

    def test_absent_read_and_validation_are_distinct_without_raw_errors(self):
        for value, expected in ((None, 'text_absent'), (b'private\xff', 'text_invalid')):
            helper = load(ROOT / 'guest/ubuntu-24.04-arm64/clipboard.py', 'helper_' + expected)
            trace = self.trace()
            self.diag.install(helper, trace, metadata=lambda: (0, False))
            desktop = type('Desktop', (), {'read': lambda self: value})()
            with self.assertRaises(helper.ClipboardError):
                helper.read_value(desktop, lambda: None)
            self.assertIn(expected, [r['event'] for r in trace.records])
            self.assertNotIn('private', json.dumps(trace.records))

    def test_native_callbacks_record_get_clear_without_payload(self):
        trace = self.trace()
        self.diag.install(self.helper, trace, metadata=lambda: (17, True))
        desktop = self.helper.GTKClipboard.__new__(self.helper.GTKClipboard)
        desktop.atom = 17
        desktop.clipboard = 23
        desktop.get_callback = ctypes.CFUNCTYPE(None, ctypes.c_void_p, ctypes.c_void_p, ctypes.c_uint, ctypes.c_void_p)
        desktop.clear_callback = ctypes.CFUNCTYPE(None, ctypes.c_void_p, ctypes.c_void_p)
        class GTK:
            def gtk_clipboard_set_with_data(self, clip, targets, count, get, clear, data):
                self.get, self.clear = get, clear
                return 1
            def gtk_selection_data_set(self, *args): pass
        desktop.gtk = GTK()
        self.assertTrue(desktop.claim(b'synthetic private payload'))
        desktop.gtk.get(23, 1, 0, None)
        desktop.gtk.clear(23, None)
        events = [r['event'] for r in trace.records]
        self.assertIn('owner_get', events)
        self.assertIn('owner_clear', events)
        self.assertFalse(desktop.owned)
        self.assertNotIn('private', json.dumps(trace.records))

    def test_preflight_trace_failure_refuses_before_read(self):
        trace = self.trace()
        trace.event = mock.Mock(side_effect=OSError('synthetic private failure'))
        self.diag.install(self.helper, trace, metadata=lambda: (17, True))
        desktop = type('Desktop', (), {'read': lambda self: b'valid'})()
        with self.assertRaises(self.diag.DiagnosticRefused):
            self.helper.read_value(desktop,lambda:None)

    def test_metadata_does_not_swallow_original_deadline_exception(self):
        def expired(): raise self.helper.ClipboardError('clipboard timeout')
        self.diag.install(self.helper, self.trace(), metadata=expired)
        outer = self
        desktop = type('Desktop', (), {'read': lambda self: outer.fail('text read after deadline')})()
        with self.assertRaises(self.helper.ClipboardError):
            self.helper.read_value(desktop, lambda: None)

    def test_claim_does_not_probe_metadata_before_original_commit_bookkeeping(self):
        trace = self.trace()
        self.diag.install(self.helper, trace, metadata=lambda: self.fail('metadata before commit'))
        desktop = self.helper.GTKClipboard.__new__(self.helper.GTKClipboard)
        desktop.atom, desktop.clipboard = 17, 23
        desktop.get_callback = ctypes.CFUNCTYPE(None, ctypes.c_void_p, ctypes.c_void_p, ctypes.c_uint, ctypes.c_void_p)
        desktop.clear_callback = ctypes.CFUNCTYPE(None, ctypes.c_void_p, ctypes.c_void_p)
        desktop.gtk = type('GTK', (), {'gtk_clipboard_set_with_data': lambda self, *args: 1})()
        completed = []
        self.helper.claim_value(desktop, b'valid', lambda: None, on_claim=lambda: completed.append(True))
        self.assertEqual(completed, [True])
        self.assertNotIn('metadata_unavailable', [record['event'] for record in trace.records])

    def test_target_metadata_requests_only_targets_and_keeps_callback_alive(self):
        class Function:
            def __call__(self, clipboard, callback, data):
                self.args = (clipboard, data)
                callback(clipboard, (ctypes.c_void_p * 2)(18, 17), 2, None)
        function = Function()
        desktop = type('Desktop', (), {'gtk': type('GTK', (), {'gtk_clipboard_request_targets': function})(),
            'atom': 17, 'clipboard': 23, 'pump': lambda self: None})()
        with mock.patch.object(self.diag, 'bounded_owner', return_value=123):
            self.assertEqual(self.diag.native_metadata(self.helper, desktop), (123, True))
        self.assertEqual(function.args, (23, None))
        self.assertTrue(callable(desktop._diagnostic_callback))

    def test_owner_worker_has_fixed_argv_closed_env_and_sanitized_output(self):
        env = {'DISPLAY': ':0', 'XAUTHORITY': '/run/user/1000/auth', 'PRIVATE_PAYLOAD': 'private'}
        class Result: stdout = b'123\n'
        with mock.patch.dict(os.environ, env, clear=True), mock.patch.object(self.diag.subprocess, 'run', return_value=Result()) as runner:
            self.assertEqual(self.diag.bounded_owner(self.helper), 123)
            args, kwargs = runner.call_args
            self.assertEqual(args[0], ['/usr/bin/python3', '-I', '-S', '/usr/local/libexec/boxwarden-guest-clipboard.py', 'owner-metadata'])
            self.assertNotIn('PRIVATE_PAYLOAD', kwargs['env'])
            self.assertEqual(kwargs['timeout'], 0.25)
            self.assertEqual(kwargs['stdin'], self.diag.subprocess.DEVNULL)
        with mock.patch.dict(os.environ, env, clear=True), mock.patch.object(self.diag.subprocess, 'run', side_effect=self.diag.subprocess.TimeoutExpired('private', 0.25)):
            with self.assertRaises(self.diag.DiagnosticRefused) as caught:
                self.diag.bounded_owner(self.helper)
            self.assertNotIn('private', str(caught.exception))

    def test_native_invalid_representation_is_distinct_from_absent(self):
        fixtures = load(ROOT / 'guest/ubuntu-24.04-arm64/tests/clipboard_test.py', 'native_fixtures')
        trace = self.trace()
        self.diag.install(fixtures.helper, trace, metadata=lambda: (123, True))
        fixture = fixtures.NativeGTKTests()
        with self.assertRaises(fixtures.helper.ClipboardError):
            fixtures.helper.read_value(fixture.make_desktop(fmt=16), lambda: None)
        self.assertIn('native_read_invalid', [record['event'] for record in trace.records])

    def test_trace_internal_monotonic_timestamp_and_frame_status_only(self):
        with tempfile.TemporaryDirectory() as directory:
            path=Path(directory)/'trace'
            sink=self.diag.FileTrace.create(path,BINDING,uid=os.getuid(),gid=os.getgid())
            sink.origin=100
            self.diag.install(self.helper,sink,metadata=lambda:(123,True))
            out=io.BytesIO()
            with mock.patch.object(self.diag.time,'monotonic',return_value=sink.origin+1):
                self.helper.write_frame(out,'ok',b'synthetic private payload')
            with mock.patch.object(self.diag.time,'monotonic',return_value=101):sink.close()
            records=self.diag.collect_file(path,BINDING_DIGEST,uid=os.getuid(),gid=os.getgid())
            self.assertEqual(records,[{'event':'frame_ok','at_ms':1000,'header_digest':BINDING_DIGEST,'lane':'parent'}])
            self.assertIn(b'synthetic private payload',out.getvalue())
            self.assertNotIn('private',json.dumps(records))

    def test_fixed_trace_caps_and_collector_rejects_malformed_records(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'trace'
            trace = self.diag.FileTrace.create(path, binding=BINDING, uid=os.getuid(), gid=os.getgid())
            for _ in range(100):
                try: trace.event('owner_get')
                except self.diag.DiagnosticRefused:pass
            trace.close()
            self.assertLessEqual(path.stat().st_size, self.diag.MAX_BYTES)
            records = self.diag.collect_file(path, header_digest=BINDING_DIGEST, uid=os.getuid(), gid=os.getgid())
            self.assertLessEqual(len(records), self.diag.MAX_EVENTS)
            with self.assertRaises(self.diag.DiagnosticRefused):
                self.diag.FileTrace.create(path, binding=BINDING, uid=os.getuid(), gid=os.getgid())
            path.write_bytes(b'{"event":"owner_get","value":"private"}\n')
            with self.assertRaises(self.diag.DiagnosticRefused):
                self.diag.collect_file(path, header_digest=BINDING_DIGEST, uid=os.getuid(), gid=os.getgid())

    def test_collector_refuses_symlink_permissions_hardlink_and_oversize(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'trace'
            target = Path(directory) / 'target'
            target.write_bytes(b'')
            path.symlink_to(target)
            with self.assertRaises(self.diag.DiagnosticRefused): self.diag.collect_file(path, header_digest=BINDING_DIGEST, uid=os.getuid(), gid=os.getgid())
            path.unlink()
            path.write_bytes(b'')
            path.chmod(0o644)
            with self.assertRaises(self.diag.DiagnosticRefused): self.diag.collect_file(path, header_digest=BINDING_DIGEST, uid=os.getuid(), gid=os.getgid())
            path.chmod(0o600)
            os.link(path, target.with_name('linked'))
            with self.assertRaises(self.diag.DiagnosticRefused): self.diag.collect_file(path, header_digest=BINDING_DIGEST, uid=os.getuid(), gid=os.getgid())
            target.with_name('linked').unlink()
            path.write_bytes(b'x' * (self.diag.MAX_BYTES + 1))
            with self.assertRaises(self.diag.DiagnosticRefused): self.diag.collect_file(path, header_digest=BINDING_DIGEST, uid=os.getuid(), gid=os.getgid())

    def test_trace_schema_rejects_wrong_fields_and_duplicates(self):
        for record in ({'event': 'binding'}, {'event': 'owner_pid'}, {'event': 'owner_clear', 'value': 1},
                       {'event': 'metadata_targets', 'value': 2}):
            self.assertFalse(self.diag.valid_record(dict(record, at_ms=1,header_digest=BINDING_DIGEST)))
        for value in (True, -1, 0x8000000000000000):
            self.assertFalse(self.diag.valid_record({'event': 'owner_clear', 'at_ms': value,'header_digest':BINDING_DIGEST}))
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'trace'
            path.write_bytes(b'{"event":"owner_clear","event":"owner_get","at_ms":1}\n'.ljust(256, b'\0'))
            path.chmod(0o600)
            with self.assertRaises(self.diag.DiagnosticRefused): self.diag.collect_file(path, header_digest=BINDING_DIGEST, uid=os.getuid(), gid=os.getgid())

    def test_forked_owner_has_disjoint_bounded_slots_and_saturation_receipt(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'trace'
            trace = self.diag.FileTrace.create(path, binding=BINDING, uid=os.getuid(), gid=os.getgid())
            trace.event('read_begin')
            pid = os.fork()
            if pid == 0:
                for _ in range(100):
                    try: trace.event('owner_get')
                    except self.diag.DiagnosticRefused:pass
                os._exit(0)
            os.waitpid(pid, 0)
            trace.event('read_complete')
            trace.close()
            records = self.diag.collect_file(path, header_digest=BINDING_DIGEST, uid=os.getuid(), gid=os.getgid())
            events = [record['event'] for record in records]
            self.assertIn('read_begin', events)
            self.assertIn('read_complete', events)
            self.assertIn('trace_incomplete', events)
            self.assertLessEqual(path.stat().st_size, 16384)




BINDING = {'version': 1, 'operation_id': '11111111-1111-4111-8111-111111111111',
    'direction': 'write', 'domain': 'n1qualification',
    'session_id': '22222222-2222-4222-8222-222222222222', 'backend_kind': 'tart',
    'backend_object': 'n1-candidate', 'generation': '33333333-3333-4333-8333-333333333333',
    'expires_at': '2026-09-30T04:00:00.123456789Z'}
EXPIRY = 1790740800123456789


BINDING_DIGEST=hashlib.sha256(json.dumps(BINDING,separators=(',',':'),sort_keys=True).encode()).hexdigest()


class BoundDiagnosticTests(unittest.TestCase):
    setUp=DiagnosticTests.setUp
    trace=DiagnosticTests.trace
    def binding(self, direction='write'):
        value = dict(BINDING, direction=direction)
        self.assertTrue(hasattr(self.diag, 'parse_binding'), 'strict operation binding API missing')
        return self.diag.parse_binding(json.dumps(value).encode(), direction, EXPIRY,
            expected=value, now_ns=EXPIRY - 1000000000)

    def test_binding_refuses_wrong_generation_id_expiry_and_malformed_json(self):
        self.binding()
        cases = [dict(BINDING, generation='44444444-4444-4444-8444-444444444444'),
                 dict(BINDING, operation_id='00000000-0000-0000-0000-000000000000'),
                 dict(BINDING, domain='personal'), dict(BINDING, expires_at='2026-09-30T04:00:00.123456788Z'),
                 dict(BINDING, unexpected='private')]
        for value in cases:
            with self.subTest(value=value), self.assertRaises(self.diag.DiagnosticRefused):
                self.diag.parse_binding(json.dumps(value).encode(), 'write', EXPIRY,
                    expected=BINDING, now_ns=EXPIRY-1)
        for raw in (b'{"version":1,"version":1}', b'['*1500+b']'*1500, b'{"version":'+b'9'*4301+b'}'):
            with self.subTest(raw_len=len(raw)), self.assertRaises(self.diag.DiagnosticRefused):
                self.diag.parse_binding(raw, 'write', EXPIRY, expected=BINDING, now_ns=EXPIRY-1)
        with self.assertRaises(self.diag.DiagnosticRefused):
            self.diag.parse_binding(json.dumps(BINDING).encode(), 'write', EXPIRY, expected=BINDING, now_ns=EXPIRY)

    def test_fixed_fd3_intake_exact_header_then_eof_before_input(self):
        self.binding()
        raw = json.dumps(BINDING).encode()
        for blocks, good in (([raw, b''], True), ([raw, b'private', b''], False), ([b''], False)):
            calls=[]
            def read(fd, amount):
                calls.append((fd, amount)); return blocks.pop(0)
            with mock.patch.object(self.diag.os, 'read', side_effect=read), mock.patch.object(self.diag.os, 'fstat', return_value=type('Info', (), {'st_mode': 0o010600})()), mock.patch.object(self.diag.os, 'close'), mock.patch.object(self.diag.select,'select',return_value=([3],[],[])), mock.patch.object(self.diag.time, 'time_ns', return_value=EXPIRY-1000000000):
                if good: self.assertEqual(self.diag.intake_binding('write', EXPIRY), BINDING)
                else:
                    with self.assertRaises(self.diag.DiagnosticRefused): self.diag.intake_binding('write', EXPIRY)
            self.assertTrue(all(fd == 3 and amount <= 4097 for fd, amount in calls))

    def test_owner_exact_pid_birth_uid_and_no_other_proc_reads(self):
        self.binding()
        self.assertTrue(hasattr(self.diag, 'collect_owner'), 'exact owner collector missing')
        fields=[b'S']+[b'1']*18+[b'5000000000']+[b'0']*30
        statraw=b'77 (unrelated private name) '+b' '.join(fields)+b'\n'
        status=b'Name:\tprivate\nUid:\t1000\t1000\t1000\t1000\n'
        seen=[]
        def read(path, limit):
            seen.append(path); return status if path.endswith('/status') else statraw
        with mock.patch.object(self.diag, 'read_proc_metadata', side_effect=read):
            self.assertEqual(self.diag.collect_owner(77, 5000000000), {'pid':77,'start_ticks':5000000000,'uid':1000,'state':'live'})
        self.assertEqual(set(seen), {'/proc/77/stat','/proc/77/status'})
        for badstat,badstatus in ((statraw.replace(b'5000000000',b'5000000001'),status),
            (statraw.replace(b') S ', b') Z '),status), (b'malformed',status),
            (statraw,status.replace(b'1000',b'1001')), (b'',status)):
            with mock.patch.object(self.diag, 'read_proc_metadata', side_effect=lambda path,limit: badstatus if path.endswith('/status') else badstat):
                with self.assertRaises(self.diag.DiagnosticRefused): self.diag.collect_owner(77,5000000000)
        counter=[0]
        def changed(path,limit):
            if path.endswith('/status'): return status
            counter[0]+=1
            return statraw if counter[0]==1 else statraw.replace(b'5000000000',b'5000000001')
        with mock.patch.object(self.diag,'read_proc_metadata',side_effect=changed):
            with self.assertRaises(self.diag.DiagnosticRefused): self.diag.collect_owner(77,5000000000)

    def test_sink_failure_preflight_refuses_before_text_or_claim(self):
        trace=self.trace(); trace.event=mock.Mock(side_effect=OSError('private'))
        self.diag.install(self.helper,trace,metadata=lambda:(1,True))
        outer=self
        desktop=type('Desktop',(),{'read':lambda self: outer.fail('read despite failed preflight'), 'claim':lambda self,value: outer.fail('claim despite failed preflight')})()
        with self.assertRaises(self.diag.DiagnosticRefused): self.helper.read_value(desktop,lambda:None)
        with self.assertRaises(self.helper.ClipboardError) as caught:self.helper.claim_value(desktop,b'valid',lambda:None)
        self.assertFalse(caught.exception.unknown)

    def test_post_native_claim_failure_preserves_on_claim_ack_retain(self):
        self.binding()
        for seam in ('claim_ok','on_claim_ok','ack_ok','retain_begin','metadata_owner','owner_checkpoint'):
            helper=load(ROOT/'guest/ubuntu-24.04-arm64/clipboard.py','post_'+seam)
            trace=self.trace(); original=trace.event
            def event(code, **kw):
                if code==seam: raise OSError('private')
                return original(code,**kw)
            trace.event=event
            # Fake native GTK only; keep canonical claim and wrapped retain,
            # real fork/lease/timer/ack and file slots. Each injected seam is hit.
            reader,writer=os.pipe()
            desktop=helper.GTKClipboard.__new__(helper.GTKClipboard)
            desktop.atom,desktop.clipboard=17,23
            desktop.get_callback=ctypes.CFUNCTYPE(None,ctypes.c_void_p,ctypes.c_void_p,ctypes.c_uint,ctypes.c_void_p)
            desktop.clear_callback=ctypes.CFUNCTYPE(None,ctypes.c_void_p,ctypes.c_void_p)
            class GTK:
                def gtk_clipboard_set_with_data(self,*args):os.write(writer,b'c');return 1
            desktop.gtk=GTK()
            def pump():os.write(writer,b'r');desktop.owned=False
            desktop.pump=pump
            trace.event('trace_begin');trace.event('binding',digest='a'*64)
            self.diag.install(helper,trace,metadata=lambda:(17,True))
            try:
                with mock.patch.object(helper,'GTKClipboard',return_value=desktop),mock.patch.object(self.diag,'self_owner',return_value=(77,5000000000)):
                    helper.owner_write(b'valid',type('Session',(),{'check':lambda self:None})())
                os.close(writer);writer=None
                data=b''
                while len(data)<2:
                    block=os.read(reader,2-len(data))
                    if not block:break
                    data+=block
                self.assertEqual(data,b'cr',seam)
                helper.write_frame(io.BytesIO(),'ok',length=5);trace.finish()
                records=trace.records
                self.assertIn('trace_incomplete',[r['event'] for r in records],seam)
                self.assertFalse(self.diag.trace_complete(records,'write'),seam)
            finally:
                os.close(reader)
                if writer is not None: os.close(writer)

    def test_trace_requires_binding_and_bounds_owner_birth_and_offsets(self):
        self.binding()
        with tempfile.TemporaryDirectory() as directory:
            path=Path(directory)/'trace'
            trace=self.diag.FileTrace.create(path,binding=self.binding(),uid=os.getuid(),gid=os.getgid())
            digest=trace.header_digest
            trace.origin=100
            with mock.patch.object(self.diag.time,'monotonic',return_value=101):
                trace.event('owner_start',value=5000000000)
            with mock.patch.object(self.diag.time,'monotonic',return_value=101):trace.close()
            records=self.diag.collect_file(path,uid=os.getuid(),gid=os.getgid(),header_digest=digest)
            self.assertEqual(records[0],{'event':'owner_start','value':5000000000,'at_ms':1000,'header_digest':digest,'lane':'parent'})
            self.assertTrue(all(record['header_digest']==digest for record in records))
            self.assertLessEqual(path.stat().st_size,16384)
            with self.assertRaises(self.diag.DiagnosticRefused):
                self.diag.collect_file(path,uid=os.getuid(),gid=os.getgid(),header_digest='f'*64)

    def test_quota_window_clock_regression_and_failed_finalization_are_incomplete(self):
        fixture=ReviewRegressionTests();fixture.setUp();self.addCleanup(fixture.doCleanups)
        for mode in ('healthy','quota','window','infinite','regression','write_failure','short_write','fsync_delay'):
            with self.subTest(mode=mode),fixture.operation('read',auto_finish=False) as (binding,trace,path):
                if mode=='quota':
                    for _ in range(40):
                        try:trace.event('owner_get')
                        except fixture.diag.DiagnosticRefused:pass
                elif mode in ('window','infinite','regression'):
                    value={'window':trace.origin+61,'infinite':float('inf'),'regression':trace.origin-1}[mode]
                    with mock.patch.object(fixture.diag.time,'monotonic',return_value=value),self.assertRaises(fixture.diag.DiagnosticRefused):trace.event('owner_get')
                elif mode in ('write_failure','short_write'):
                    patch=mock.patch.object(fixture.diag.os,'pwrite',side_effect=OSError('private')) if mode=='write_failure' else mock.patch.object(fixture.diag.os,'pwrite',return_value=1)
                    with patch,self.assertRaises(fixture.diag.DiagnosticRefused):trace.event('owner_get')
                if mode=='fsync_delay':
                    real=fixture.diag.os.fsync
                    def delayed(fd):real(fd);trace.origin-=61
                    with mock.patch.object(fixture.diag.os,'fsync',side_effect=delayed):trace.finish()
                else:trace.finish()
                with closure_channel() as reader:
                    fixture.diag.admit_closure_output();trace.closure_admitted=True
                    try:trace.close()
                    except fixture.diag.DiagnosticRefused:pass
                    if mode=='healthy':root_fixture_publish(fixture.diag,reader,binding,path)
                    else:
                        with self.assertRaises(fixture.diag.DiagnosticRefused):root_fixture_publish(fixture.diag,reader,binding,path)
                receipt=fixture.diag.collect_operation(binding)
                self.assertEqual(receipt['complete'],mode=='healthy')

    def test_collection_prefix_requires_ack_checkpoint_and_live_exact_owner(self):
        self.binding()
        self.assertTrue(hasattr(self.diag,'intake_collection'),'fixed collection fd3 API missing')
        with tempfile.TemporaryDirectory() as directory:
            original_metadata=self.diag.metadata_ok
            def metadata(info,uid,gid,limit=16384):
                return original_metadata(info,os.getuid(),os.getgid(),limit)
            with mock.patch.object(self.diag,'DIRECTORY',directory),mock.patch.object(self.diag,'directory_check'),mock.patch.object(self.diag,'metadata_ok',side_effect=metadata):
                trace=self.diag.create_operation(BINDING)
                for code in ReviewRegressionTests.WRITE:
                    trace.event(code,**({'digest':'a'*64} if code=='binding' else {}))
                trace.finish()
                # Synthetic detached-owner lane using the real fixed slot writer.
                trace.count=0;trace.creator=-1
                parent_count,parent_last=trace.count,trace.last_ms
                for code in ReviewRegressionTests.OWNER:
                    trace.event(code,**({'value':77} if code=='owner_pid' else {'value':5000000000} if code=='owner_start' else {'value':17} if code=='metadata_owner' else {}))
                trace.creator=os.getpid();trace.count=parent_count;trace.last_ms=parent_last
                # fd4 is reserved before actual trace creation in real main.
                if trace.fd==4:
                    new=fcntl.fcntl(trace.fd,fcntl.F_DUPFD_CLOEXEC,5);os.close(trace.fd);trace.fd=new
                close_and_publish(self.diag,trace,BINDING,directory)
                live={'pid':77,'start_ticks':5000000000,'uid':1000,'state':'live'}
                with mock.patch.object(self.diag,'collect_owner',return_value=live):
                    receipt=self.diag.collect_operation(BINDING)
                self.assertTrue(receipt['complete'])
                self.assertEqual(receipt['coverage_scope'],'collected_progression_prefix')
                self.assertEqual(receipt['owner'],live)
                self.assertNotIn('payload',json.dumps(receipt))
                with mock.patch.object(self.diag,'collect_owner',side_effect=self.diag.DiagnosticRefused()):
                    self.assertFalse(self.diag.collect_operation(BINDING)['complete'])
                # Same generation with wrong operation must refuse old receipts.
                with self.assertRaises(self.diag.DiagnosticRefused):
                    self.diag.collect_operation(dict(BINDING,operation_id='44444444-4444-4444-8444-444444444444'))
                for missing in ('ack_ok','owner_checkpoint','trace_end','binding'):
                    records=[r for r in receipt['records'] if r['event']!=missing]
                    self.assertFalse(self.diag.trace_complete(records,'write'),missing)
                reordered=[r for r in receipt['records'] if r['event'] not in ('ack_ok','retain_begin')]
                reordered += [{'event':'retain_begin'},{'event':'ack_ok'}]
                self.assertFalse(self.diag.trace_complete(reordered,'write'))

    def test_fixed_collection_intake_allows_original_expired_binding_without_new_transfer(self):
        self.binding()
        self.assertTrue(hasattr(self.diag,'intake_collection'),'fixed collection fd3 API missing')
        blocks=[json.dumps(BINDING).encode(),b'']
        with mock.patch.object(self.diag.os,'read',side_effect=lambda fd,amount:blocks.pop(0)),mock.patch.object(self.diag.os,'fstat',return_value=type('Info',(),{'st_mode':0o010600})()),mock.patch.object(self.diag.os,'close'),mock.patch.object(self.diag.select,'select',return_value=([3],[],[])):
            self.assertEqual(self.diag.intake_collection('write'),BINDING)

    def test_binding_creation_refuses_reuse_and_header_mutation(self):
        self.binding()
        with tempfile.TemporaryDirectory() as directory:
            original_metadata=self.diag.metadata_ok
            def metadata(info,uid,gid,limit=16384):return original_metadata(info,os.getuid(),os.getgid(),limit)
            with mock.patch.object(self.diag,'DIRECTORY',directory),mock.patch.object(self.diag,'directory_check'),mock.patch.object(self.diag,'metadata_ok',side_effect=metadata):
                trace=self.diag.create_operation(BINDING);trace.close()
                with self.assertRaises(self.diag.DiagnosticRefused):self.diag.create_operation(BINDING)
                header=Path(directory)/'write.binding'
                header.write_bytes(json.dumps(dict(BINDING,generation='44444444-4444-4444-8444-444444444444')).encode())
                with self.assertRaises(self.diag.DiagnosticRefused):self.diag.collect_operation(BINDING)

    def test_decoder_valueerror_and_recursionerror_normalize_without_error_text(self):
        self.binding()
        for error in (ValueError('private integer'),RecursionError('private recursion')):
            with mock.patch.object(self.diag.json,'loads',side_effect=error),self.assertRaises(self.diag.DiagnosticRefused) as caught:
                self.diag.parse_binding(json.dumps(BINDING).encode(),'write',EXPIRY,now_ns=EXPIRY-1)
            self.assertNotIn('private',str(caught.exception))

    def test_post_read_sink_loss_preserves_canonical_bytes_and_session_check(self):
        trace=self.trace();original=trace.event
        def event(code,**kw):
            if code=='native_read_complete':raise OSError('private')
            return original(code,**kw)
        trace.event=event
        self.diag.install(self.helper,trace,metadata=lambda:(17,True))
        # Simulate the native result from an already-started first retrieval.
        def read():
            try:trace.event('native_read_complete')
            except OSError:trace.invalidate()
            return b'valid'
        desktop=type('Desktop',(),{'read':lambda self:read()})()
        checks=[]
        self.assertEqual(self.helper.read_value(desktop,lambda:checks.append(1)),b'valid')
        self.assertEqual(checks,[1,1])

    def test_collection_close_delay_and_changed_header_inode_prevent_completeness(self):
        self.binding()
        original_metadata=self.diag.metadata_ok
        def metadata(info,uid,gid,limit=16384):return original_metadata(info,os.getuid(),os.getgid(),limit)
        with tempfile.TemporaryDirectory() as directory,mock.patch.object(self.diag,'DIRECTORY',directory),mock.patch.object(self.diag,'directory_check'),mock.patch.object(self.diag,'metadata_ok',side_effect=metadata):
            binding=dict(BINDING,direction='read')
            trace=self.diag.create_operation(binding)
            for code in ReviewRegressionTests.READ:
                trace.event(code,**({'digest':'a'*64} if code=='binding' else {'value':17} if code=='metadata_owner' else {'value':1} if code=='metadata_targets' else {}))
            trace.finish()
            if trace.fd==4:
                new=fcntl.fcntl(trace.fd,fcntl.F_DUPFD_CLOEXEC,5);os.close(trace.fd);trace.fd=new
            close_and_publish(self.diag,trace,binding,directory)
            self.assertTrue(self.diag.collect_operation(binding)['complete'])
            with mock.patch.object(self.diag.time,'monotonic',side_effect=[10,10.3,10.3]):
                self.assertFalse(self.diag.collect_operation(binding)['complete'])
            real_collect=self.diag.collect_file;calls=[0]
            def changed(*args,**kw):
                records=real_collect(*args,**kw);calls[0]+=1
                if calls[0]==1:
                    path=Path(directory)/'read.binding';raw=path.read_bytes();path.unlink();path.write_bytes(raw);path.chmod(0o600)
                return records
            with mock.patch.object(self.diag,'collect_file',side_effect=changed):
                with self.assertRaises(self.diag.DiagnosticRefused):self.diag.collect_operation(binding)

    def test_preclaim_recording_refusal_is_explicit_before_original_lease_check(self):
        trace=self.trace();trace.event=mock.Mock(side_effect=OSError('private'))
        self.diag.install(self.helper,trace,metadata=lambda:(17,True))
        order=[]
        desktop=type('Desktop',(),{'claim':lambda self,value:order.append('native')})()
        with self.assertRaises((self.helper.ClipboardError,self.diag.DiagnosticRefused)) as caught:
            self.helper.claim_value(desktop,b'valid',lambda:None,before_claim=lambda:order.append('lease'))
        self.assertIsInstance(caught.exception,self.helper.ClipboardError,'preclaim diagnostic refusal must be explicit canonical precommit error')
        self.assertFalse(caught.exception.unknown)
        self.assertEqual(order,[])

    def test_recording_preflight_precedes_original_immediate_lease_deadline_check(self):
        trace=self.trace();original=trace.event;order=[]
        def event(code,**fields):
            if code=='claim_begin':order.append('metadata')
            return original(code,**fields)
        trace.event=event
        self.diag.install(self.helper,trace,metadata=lambda:(17,True))
        desktop=type('Desktop',(),{'claim':lambda self,value:order.append('native') or True})()
        self.helper.claim_value(desktop,b'valid',lambda:None,before_claim=lambda:order.append('lease'))
        self.assertEqual(order,['metadata','lease','native'])

    def test_main_post_operation_trace_close_failure_preserves_canonical_status(self):
        self.binding()
        source=ROOT/'guest/ubuntu-24.04-arm64/clipboard.py'
        original_fstat=self.diag.os.fstat
        def fstat(fd):
            info=original_fstat(fd)
            return type('Info',(),{'st_mode':0o100755,'st_uid':0,'st_gid':0,'st_nlink':1,'st_size':info.st_size})()
        class FailedFinalization:
            def __init__(self):self.invalidated=False
            def finish(self):pass
            def close(self):raise OSError('private')
            def invalidate(self):self.invalidated=True
        sink=FailedFinalization()
        def install(helper,trace):helper.main=lambda:2
        with mock.patch.object(self.diag,'PRODUCTION',str(source)),mock.patch.object(self.diag,'directory_check'),mock.patch.object(self.diag.os,'geteuid',return_value=0),mock.patch.object(self.diag.os,'fstat',side_effect=fstat),mock.patch.object(self.diag,'admit_closure_output'),mock.patch.object(self.diag,'intake_binding',return_value=BINDING),mock.patch.object(self.diag,'create_operation',return_value=sink),mock.patch.object(self.diag,'install',side_effect=install),mock.patch.object(self.diag.signal,'signal'),mock.patch.object(self.diag.signal,'setitimer'),mock.patch.object(self.diag.time,'time_ns',return_value=EXPIRY-1000000000),mock.patch.object(self.diag.sys,'argv',['adapter','write','--deadline-unix-ns',str(EXPIRY)]),mock.patch.object(self.diag.sys,'stderr',io.StringIO()):
            self.assertEqual(self.diag.main(),2)
            self.assertTrue(sink.invalidated,'failed actual closure must mark incomplete')

    def test_canonical_fixtures_with_overlay_installed(self):
        self.binding()
        fixtures=load(ROOT/'guest/ubuntu-24.04-arm64/tests/clipboard_test.py','canonical_overlay')
        suite=unittest.defaultTestLoader.loadTestsFromModule(fixtures)
        outer=self
        class Result(unittest.TestResult):
            def startTest(self,test):
                fixtures.helper=load(ROOT/'guest/ubuntu-24.04-arm64/clipboard.py','fresh_canonical')
                class AdmittedSink:
                    def event(self,code,**fields): pass
                    def invalidate(self): pass
                # The canonical suite has several operations per test and mocked
                # clocks. Quota/time admission has its own real-file fixtures.
                outer.diag.install(fixtures.helper,AdmittedSink(),metadata=lambda:(17,True))
                super().startTest(test)
        with mock.patch.object(self.diag,'self_owner',return_value=(77,5000000000)):
            result=Result();suite.run(result)
        self.assertGreaterEqual(result.testsRun,59)
        self.assertEqual(result.errors,[])
        self.assertEqual(result.failures,[])


def fixture_pipe():
    pair=os.pipe()
    moved=[fcntl.fcntl(fd,fcntl.F_DUPFD_CLOEXEC,5) for fd in pair]
    for fd in pair:os.close(fd)
    return moved


@contextlib.contextmanager
def closure_channel():
    # Keep both pipe ends above fixed fd4 before replacing it; preserve any
    # unrelated test descriptor. No display, filesystem sharing or listener.
    try: saved = fcntl.fcntl(4, fcntl.F_DUPFD_CLOEXEC, 5)
    except OSError: saved = None
    reader,writer = os.pipe()
    moved = []
    try:
        for fd in (reader,writer): moved.append(fcntl.fcntl(fd,fcntl.F_DUPFD_CLOEXEC,5))
    finally:
        os.close(reader);os.close(writer)
    reader,writer = moved
    os.dup2(writer,4);os.close(writer);os.set_blocking(4,False)
    try: yield reader
    finally:
        for fd in (reader,4):
            try: os.close(fd)
            except OSError: pass
        if saved is not None:os.dup2(saved,4);os.close(saved)


def root_fixture_publish(diag, reader, binding, directory):
    # Synthetic outer-root publisher contract, not the pending Go producer.
    # EOF and actual reader close are prerequisites for namespace publication.
    raw=b''
    try:
        while True:
            if not select.select([reader],[],[],0.1)[0]:raise diag.DiagnosticRefused()
            block=os.read(reader,513-len(raw))
            if not block:break
            raw+=block
            if len(raw)>512:raise diag.DiagnosticRefused()
        digest=hashlib.sha256(diag.encode_binding(binding)).hexdigest()
        diag.parse_closure(raw,digest,binding['direction'])
    finally:os.close(reader)
    pending=Path(directory)/(binding['direction']+'.closure.pending')
    final=Path(directory)/(binding['direction']+'.closure')
    fd=os.open(pending,os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
    try:
        if os.write(fd,raw)!=len(raw):raise diag.DiagnosticRefused()
        os.fsync(fd)
    finally:os.close(fd)
    os.link(pending,final);pending.unlink()
    return raw


def close_and_publish(diag, trace, binding, directory):
    with closure_channel() as reader:
        diag.admit_closure_output()
        trace.closure_admitted=True
        trace.close()
        return root_fixture_publish(diag,reader,binding,directory)


class ReviewRegressionTests(unittest.TestCase):
    setUp = DiagnosticTests.setUp
    READ = ['session_init','binding','session_ready','read_begin','metadata_owner',
        'metadata_targets','read_request','session_check','session_check_ok',
        'native_read_complete','session_check','session_check_ok','read_complete','frame_ok']
    WRITE = ['input_begin','session_init','binding','session_ready','frame_ok']
    OWNER = ['session_check','session_check_ok','owner_pid','owner_start','claim_begin',
        'claim_ok','on_claim_ok','session_check','session_check_ok','ack_ok','retain_begin',
        'metadata_owner','owner_checkpoint']

    def emit(self, trace, codes):
        for code in codes:
            fields = {'digest':'a'*64} if code == 'binding' else {'value':77} if code == 'owner_pid' else {'value':5000000000} if code == 'owner_start' else {'value':17} if code == 'metadata_owner' else {'value':1} if code == 'metadata_targets' else {}
            trace.event(code, **fields)

    @contextlib.contextmanager
    def operation(self, direction, parent=None, owner=None, owner_first=False, origin_age=0, auto_finish=True):
        # Only fixed-path ownership checks and the actual external /proc query
        # are adapted. Header/slots/sync/close/parser/adjudication are real.
        original = self.diag.metadata_ok
        def local(info, uid, gid, limit=16384):
            return original(info, os.getuid(), os.getgid(), limit)
        with tempfile.TemporaryDirectory() as directory, mock.patch.object(self.diag,'DIRECTORY',directory), mock.patch.object(self.diag,'directory_check'), mock.patch.object(self.diag,'metadata_ok',side_effect=local), mock.patch.object(self.diag,'collect_owner',return_value={'pid':77,'start_ticks':5000000000,'uid':1000,'state':'live'}):
            binding = dict(BINDING, direction=direction)
            trace = self.diag.create_operation(binding)
            if trace.fd<=4:
                fd=fcntl.fcntl(trace.fd,fcntl.F_DUPFD_CLOEXEC,5);os.close(trace.fd);trace.fd=fd
            trace.origin-=origin_age
            parent = list(self.READ if direction == 'read' else self.WRITE) if parent is None else parent
            owner = list(self.OWNER) if direction == 'write' and owner is None else (owner or [])
            def child():
                creator,count,last = trace.creator,trace.count,trace.last_ms
                trace.creator = -1
                self.emit(trace,owner)
                trace.creator,trace.count,trace.last_ms = creator,count,last
            try:
                if owner_first and owner: child()
                self.emit(trace,parent)
                if auto_finish:trace.finish()
                if not owner_first and owner: child()
                yield binding,trace,Path(directory)
            finally:
                if getattr(trace,'fd',None) is not None:
                    try: trace.close()
                    except self.diag.DiagnosticRefused: pass

    def receipt(self, direction, parent=None, owner=None, owner_first=False):
        with self.operation(direction,parent,owner,owner_first) as (binding,trace,_):
            close_and_publish(self.diag,trace,binding,_)
            return self.diag.collect_operation(binding)

    def test_valid_fixed_slot_read_and_both_owner_parent_interleavings(self):
        self.assertTrue(self.receipt('read')['complete'])
        for before in (True,False):
            with self.subTest(owner_first=before):
                receipt=self.receipt('write',owner_first=before)
                self.assertTrue(receipt['complete'])
                self.assertEqual({r.get('lane') for r in receipt['records']},{'parent','owner'})
                self.assertNotIn('retain_return',[r['event'] for r in receipt['records']])

    def test_read_reordered_conflicting_duplicate_and_misattributed_progression(self):
        bad=[]
        order=list(self.READ);order.remove('read_request');order.insert(order.index('frame_ok')+1,'read_request');bad.append(order)
        bad.append(self.READ+['read_failed','frame_error'])
        for code in ('read_request','read_complete','native_read_complete','frame_ok','binding','session_ready'):
            bad.append(self.READ+[code])
        for code in ('frame_unknown','text_absent','native_read_failed','session_failed'):
            bad.append(self.READ+[code])
        for index,codes in enumerate(bad):
            with self.subTest(case=index):self.assertFalse(self.receipt('read',parent=codes)['complete'])
        self.assertFalse(self.receipt('read',parent=[],owner=self.READ)['complete'])

    def test_owner_missing_claim_begin_contradictions_duplicates_and_wrong_lanes(self):
        bad=[ [c for c in self.OWNER if c!='claim_begin'], self.OWNER+['ack_ok'], self.OWNER+['ack_unknown'], self.OWNER+['claim_refused'] ]
        for code in ('owner_pid','owner_start','claim_begin','claim_ok','on_claim_ok','retain_begin','owner_checkpoint'):
            bad.append(self.OWNER+[code])
        for index,codes in enumerate(bad):
            with self.subTest(case=index):self.assertFalse(self.receipt('write',owner=codes)['complete'])
        self.assertFalse(self.receipt('write',parent=self.WRITE+self.OWNER,owner=[])['complete'])
        self.assertFalse(self.receipt('write',parent=self.WRITE+['frame_unknown'])['complete'])
        self.assertFalse(self.receipt('write',parent=[c for c in self.WRITE if c!='frame_ok'],owner=self.OWNER+['frame_ok'])['complete'])

    def test_native_metadata_must_belong_to_the_actual_post_ack_checkpoint(self):
        for position in ('claim_ok','ack_ok','retain_begin','owner_checkpoint'):
            codes=[c for c in self.OWNER if c!='metadata_owner']
            insert=codes.index(position) if position!='owner_checkpoint' else len(codes)
            codes.insert(insert,'metadata_owner')
            with self.subTest(position=position):self.assertFalse(self.receipt('write',owner=codes)['complete'])
        self.assertFalse(self.receipt('write',owner=self.OWNER+['metadata_owner'])['complete'])

    def test_actual_held_collector_closes_are_inside_the_budget(self):
        for mode in ('overrun','regression','infinite','close_error'):
            with self.subTest(mode=mode),self.operation('read') as (binding,trace,_):
                close_and_publish(self.diag,trace,binding,_)
                self.assertTrue(self.diag.collect_operation(binding)['complete'])
                clock=[10.0];held=[];real_open=self.diag.open_checked;real_close=os.close
                def opened(*args,**kw):
                    fd=real_open(*args,**kw)
                    if len(held)<2:held.append(fd)
                    return fd
                def closed(fd):
                    real_close(fd)
                    if fd in held:
                        if mode=='overrun':clock[0]+=.3
                        elif mode=='regression':clock[0]-=1
                        elif mode=='infinite':clock[0]=float('inf')
                        else:raise OSError('synthetic private close failure')
                with mock.patch.object(self.diag,'open_checked',side_effect=opened),mock.patch.object(self.diag.os,'close',side_effect=closed),mock.patch.object(self.diag.time,'monotonic',side_effect=lambda:clock[0]):
                    try: result=self.diag.collect_operation(binding)
                    except self.diag.DiagnosticRefused:continue
                    self.assertFalse(result['complete'])

    def test_actual_trace_close_loss_cannot_admit_a_valid_positive_baseline(self):
        self.assertTrue(self.receipt('read')['complete'])
        for mode in ('delay','close_then_error','regression','infinite'):
            with self.subTest(mode=mode), self.operation('read') as (binding,trace,path),closure_channel() as reader:
                self.diag.admit_closure_output();trace.closure_admitted=True
                clock=[trace.origin+trace.last_ms/1000];real_close=os.close
                def close(fd):
                    real_close(fd)
                    if mode=='delay':clock[0]+=61
                    elif mode=='regression':clock[0]-=1
                    elif mode=='infinite':clock[0]=float('inf')
                    else:raise OSError('synthetic private close failure after release')
                with mock.patch.object(self.diag.os,'close',side_effect=close),mock.patch.object(self.diag.time,'monotonic',side_effect=lambda:clock[0]):
                    try:trace.close()
                    except BaseException:
                        try:trace.invalidate()
                        except BaseException:pass
                with self.assertRaises(self.diag.DiagnosticRefused):root_fixture_publish(self.diag,reader,binding,path)
                self.assertFalse((path/'read.closure').exists())
                try: receipt=self.diag.collect_operation(binding)
                except self.diag.DiagnosticRefused:continue
                self.assertFalse(receipt['complete'])

    def test_closure_pipe_schema_atomic_write_and_parent_only_admission(self):
        with self.operation('read') as (binding,trace,path):
            raw=close_and_publish(self.diag,trace,binding,path)
            proof=self.diag.parse_closure(raw,trace.header_digest,'read')
            self.assertEqual(set(proof),{'version','direction','header_digest','elapsed_ms'})
            self.assertLessEqual(len(raw),512)
            self.assertTrue(self.diag.collect_operation(binding)['complete'])
        for mode in ('short','error','blocking','read_end','non_pipe'):
            with self.subTest(mode=mode),self.operation('read') as (binding,trace,path),closure_channel() as reader:
                if mode=='blocking':os.set_blocking(4,True)
                if mode=='read_end':os.dup2(reader,4)
                if mode=='non_pipe':
                    fd=os.open(path/'ordinary',os.O_WRONLY|os.O_CREAT,0o600)
                    os.dup2(fd,4);os.close(fd)
                if mode in ('blocking','read_end','non_pipe'):
                    with self.assertRaises(self.diag.DiagnosticRefused):self.diag.admit_closure_output()
                    continue
                self.diag.admit_closure_output();trace.closure_admitted=True
                real_write=os.write
                def write(fd,data):
                    if fd==4:
                        if mode=='error':raise OSError('private')
                        return 1
                    return real_write(fd,data)
                with mock.patch.object(self.diag.os,'write',side_effect=write):
                    try:trace.close()
                    except self.diag.DiagnosticRefused:pass
                with self.assertRaises(self.diag.DiagnosticRefused):root_fixture_publish(self.diag,reader,binding,path)
                self.assertFalse((path/'read.closure').exists())

    def test_missing_malformed_duplicate_trailing_foreign_and_late_proof_incomplete(self):
        with self.operation('read') as (binding,trace,path):
            raw=close_and_publish(self.diag,trace,binding,path)
            self.assertTrue(self.diag.collect_operation(binding)['complete'])
            final=path/'read.closure';proof=json.loads(raw)
            cases=[b'',b'not-json',raw+b'\n',raw+b'{}',b'x'*513,
                raw.replace(b'"version":1',b'"version":1,"version":1')]
            for changes in ({'direction':'write'},{'header_digest':'f'*64},{'elapsed_ms':60000},{'elapsed_ms':-1},{'elapsed_ms':True},{'version':True}):
                cases.append(json.dumps(dict(proof,**changes),sort_keys=True,separators=(',',':')).encode()+b'\n')
            for data in cases:
                final.write_bytes(data)
                self.assertFalse(self.diag.collect_operation(binding)['complete'])
            final.unlink()
            self.assertFalse(self.diag.collect_operation(binding)['complete'])
        with self.operation('read',origin_age=1) as (binding,trace,path):
            raw=close_and_publish(self.diag,trace,binding,path)
            proof=json.loads(raw);proof['elapsed_ms']=0
            (path/'read.closure').write_bytes(json.dumps(proof,sort_keys=True,separators=(',',':')).encode()+b'\n')
            self.assertFalse(self.diag.collect_operation(binding)['complete'])

    def test_root_fixture_waits_for_eof_and_actual_reader_close_before_publication(self):
        with self.operation('read') as (binding,trace,path),closure_channel() as reader:
            self.diag.admit_closure_output();trace.closure_admitted=True
            extra=os.dup(4)
            trace.close()
            try:
                with self.assertRaises(self.diag.DiagnosticRefused):root_fixture_publish(self.diag,reader,binding,path)
                self.assertFalse((path/'read.closure').exists())
            finally:os.close(extra)
        with self.operation('read') as (binding,trace,path),closure_channel() as reader:
            self.diag.admit_closure_output();trace.closure_admitted=True;trace.close()
            real_close=os.close
            def close(fd):
                real_close(fd)
                if fd==reader:raise OSError('private reader close after release')
            with mock.patch.object(self.diag.os,'close',side_effect=close),self.assertRaises(OSError):root_fixture_publish(self.diag,reader,binding,path)
            self.assertFalse((path/'read.closure').exists())

    def test_real_fork_owner_live_does_not_retain_parent_closure_pipe(self):
        helper=load(ROOT/'guest/ubuntu-24.04-arm64/clipboard.py','closure_owner')
        helper.Session.check=lambda self:None
        children=[];real_os=helper.os
        class TrackingOS:
            def __getattr__(self,name):return getattr(real_os,name)
            def fork(self):
                pid=real_os.fork()
                if pid:children.append(pid)
                return pid
        helper.os=TrackingOS()
        notify_r,notify_w=fixture_pipe();release_r,release_w=fixture_pipe()
        child=None
        try:
            with self.operation('write',parent=self.WRITE[:-1],owner=[],auto_finish=False) as (binding,trace,path),closure_channel() as reader:
                self.diag.admit_closure_output();trace.closure_admitted=True
                desktop=helper.GTKClipboard.__new__(helper.GTKClipboard)
                desktop.atom,desktop.clipboard=17,23
                desktop.get_callback=ctypes.CFUNCTYPE(None,ctypes.c_void_p,ctypes.c_void_p,ctypes.c_uint,ctypes.c_void_p)
                desktop.clear_callback=ctypes.CFUNCTYPE(None,ctypes.c_void_p,ctypes.c_void_p)
                class GTK:
                    def gtk_clipboard_set_with_data(_self,*args):
                        try:os.fstat(4)
                        except OSError:os.write(notify_w,b'c');return 1
                        os.write(notify_w,b'BAD');return 0
                desktop.gtk=GTK()
                def retained(_self,check):
                    os.write(notify_w,b'r')
                    os.read(release_r,1)
                helper.GTKClipboard.retain=retained
                self.diag.install(helper,trace,metadata=lambda:(17,True))
                with mock.patch.object(helper,'GTKClipboard',return_value=desktop),mock.patch.object(self.diag,'self_owner',side_effect=lambda:(os.getpid(),5000000000)):
                    helper.owner_write(b'valid',helper.Session.__new__(helper.Session))
                helper.write_frame(io.BytesIO(),'ok',length=5)
                trace.finish()
                data=b''
                while len(data)<2:
                    self.assertTrue(select.select([notify_r],[],[],1)[0],'owner native checkpoint missing')
                    data+=os.read(notify_r,2-len(data))
                self.assertEqual(data,b'cr')
                trace.close()
                root_fixture_publish(self.diag,reader,binding,path)
                with mock.patch.object(self.diag,'collect_owner',side_effect=lambda pid,ticks:{'pid':pid,'start_ticks':ticks,'uid':1000,'state':'live'}):
                    receipt=self.diag.collect_operation(binding)
                self.assertTrue(receipt['complete'])
                child=receipt['owner']['pid']
                self.assertEqual(os.waitpid(child,os.WNOHANG),(0,0),'owner must still be alive after EOF/publication')
                self.assertNotIn('retain_return',[r['event'] for r in receipt['records']])
        finally:
            os.write(release_w,b'x')
            if child is None and children:child=children[0]
            if child is not None:
                try:os.waitpid(child,0)
                except ChildProcessError:pass
            for fd in (notify_r,notify_w,release_r,release_w):os.close(fd)

    def test_child_closure_close_fault_refuses_before_native_claim_with_original_outcome(self):
        helper=load(ROOT/'guest/ubuntu-24.04-arm64/clipboard.py','closure_child_fault')
        helper.Session.check=lambda self:None
        real_os=helper.os;children=[];notify_r,notify_w=fixture_pipe()
        class ChildFaultOS:
            failed_once=False
            closure_identity=None
            def __getattr__(self,name):return getattr(real_os,name)
            def fork(self):
                pid=real_os.fork()
                if pid:children.append(pid)
                return pid
            def close(self,fd):
                target=False
                if fd==4 and not self.failed_once:
                    info=real_os.fstat(fd)
                    target=(info.st_dev,info.st_ino)==self.closure_identity
                real_os.close(fd)
                if target:
                    self.failed_once=True
                    raise OSError('synthetic child fd4 close error after release')
        fault_os=ChildFaultOS();helper.os=fault_os
        try:
            with self.operation('write',parent=self.WRITE[:-1],owner=[],auto_finish=False) as (binding,trace,path),closure_channel() as reader:
                self.diag.admit_closure_output();trace.closure_admitted=True
                info=os.fstat(4);fault_os.closure_identity=(info.st_dev,info.st_ino)
                desktop=type('Desktop',(),{'claim':lambda self,value:os.write(notify_w,b'BAD') or True})()
                self.diag.install(helper,trace,metadata=lambda:(17,True))
                with mock.patch.object(helper,'GTKClipboard',return_value=desktop),self.assertRaises(helper.ClipboardError) as error:
                    helper.owner_write(b'valid',helper.Session.__new__(helper.Session))
                # Canonical owner_write sets its unknown guard before the
                # instrumented preclaim session check; preserve that mapping.
                self.assertTrue(error.exception.unknown)
                self.assertFalse(select.select([notify_r],[],[],0.02)[0],'native claim despite fd4 failure')
                helper.write_frame(io.BytesIO(),'unknown');trace.finish();trace.close()
                root_fixture_publish(self.diag,reader,binding,path)
                self.assertFalse(self.diag.collect_operation(binding)['complete'])
        finally:
            for pid in children:
                try:os.waitpid(pid,0)
                except ChildProcessError:pass
            os.close(notify_r);os.close(notify_w)

    def test_submillisecond_close_clock_regression_is_not_rounded_into_a_proof(self):
        clock=[10.0]
        with mock.patch.object(self.diag.time,'monotonic',side_effect=lambda:clock[0]),self.operation('read',auto_finish=False) as (binding,trace,path),closure_channel() as reader:
            self.diag.admit_closure_output();trace.closure_admitted=True
            clock[0]=10.0005;trace.finish()
            clock[0]=10.0004
            try:trace.close()
            except self.diag.DiagnosticRefused:pass
            with self.assertRaises(self.diag.DiagnosticRefused):root_fixture_publish(self.diag,reader,binding,path)
            self.assertFalse(self.diag.collect_operation(binding)['complete'])

    def test_same_bytes_replaced_closure_inode_does_not_admit_stable_proof(self):
        with self.operation('read') as (binding,trace,path):
            close_and_publish(self.diag,trace,binding,path)
            self.assertTrue(self.diag.collect_operation(binding)['complete'])
            real=self.diag.collect_closure;calls=[0]
            def replaced(*args):
                proof=real(*args);calls[0]+=1
                if calls[0]==1:
                    target=path/'read.closure';raw=target.read_bytes()
                    staged=path/'replacement';staged.write_bytes(raw);staged.chmod(0o600)
                    os.replace(staged,target)
                return proof
            with mock.patch.object(self.diag,'collect_closure',side_effect=replaced):
                self.assertFalse(self.diag.collect_operation(binding)['complete'])

    def test_live_owner_offsets_at_the_trace_window_cutoff_are_incomplete(self):
        with self.operation('write') as (binding,trace,path):
            close_and_publish(self.diag,trace,binding,path)
            self.assertTrue(self.diag.collect_operation(binding)['complete'])
            target=path/'write.trace';raw=bytearray(target.read_bytes())
            for index in range(32,64):
                chunk=raw[index*256:(index+1)*256].rstrip(b'\0')
                if chunk:
                    record=json.loads(chunk);record['at_ms']=60000
                    encoded=json.dumps(record,sort_keys=True,separators=(',',':')).encode()+b'\n'
                    raw[index*256:(index+1)*256]=encoded.ljust(256,b'\0')
            target.write_bytes(raw)
            try:receipt=self.diag.collect_operation(binding)
            except self.diag.DiagnosticRefused:return
            self.assertFalse(receipt['complete'])

    def test_real_main_preserves_success_but_close_loss_cannot_publish_proof(self):
        source=ROOT/'guest/ubuntu-24.04-arm64/clipboard.py'
        source_inode=source.stat().st_ino
        for mode in ('healthy','error','delay'):
            with self.subTest(mode=mode),self.operation('read',auto_finish=False) as (binding,trace,path),closure_channel() as reader:
                target_fd=trace.fd;real_close=os.close;real_fstat=os.fstat
                def fstat(fd):
                    info=real_fstat(fd)
                    if info.st_ino!=source_inode:return info
                    return type('Info',(),{'st_mode':0o100755,'st_uid':0,'st_gid':0,'st_nlink':1,'st_size':info.st_size})()
                def close(fd):
                    real_close(fd)
                    if fd==target_fd:
                        if mode=='error':raise OSError('synthetic private close error after release')
                        if mode=='delay':trace.origin-=61
                def install(helper,sink):helper.main=lambda:0
                with mock.patch.object(self.diag,'PRODUCTION',str(source)),mock.patch.object(self.diag.os,'geteuid',return_value=0),mock.patch.object(self.diag.os,'fstat',side_effect=fstat),mock.patch.object(self.diag.os,'close',side_effect=close),mock.patch.object(self.diag,'intake_binding',return_value=binding),mock.patch.object(self.diag,'create_operation',return_value=trace),mock.patch.object(self.diag,'install',side_effect=install),mock.patch.object(self.diag.signal,'signal'),mock.patch.object(self.diag.signal,'setitimer'),mock.patch.object(self.diag.time,'time_ns',return_value=EXPIRY-1000000000),mock.patch.object(self.diag.sys,'argv',['adapter','read','--deadline-unix-ns',str(EXPIRY)]),mock.patch.object(self.diag.sys,'stderr',io.StringIO()):
                    self.assertEqual(self.diag.main(),0)
                if mode=='healthy':root_fixture_publish(self.diag,reader,binding,path)
                else:
                    with self.assertRaises(self.diag.DiagnosticRefused):root_fixture_publish(self.diag,reader,binding,path)
                self.assertEqual(self.diag.collect_operation(binding)['complete'],mode=='healthy')


if __name__ == '__main__':
    unittest.main()

import ctypes
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

    def trace(self):
        directory = tempfile.TemporaryDirectory()
        self.addCleanup(directory.cleanup)
        path = Path(directory.name) / 'trace'
        sink = self.diag.FileTrace.create(path, uid=os.getuid(), gid=os.getgid())
        self.addCleanup(sink.close)
        outer = self
        class Trace:
            def event(self, *args, **kwargs): sink.event(*args, **kwargs)
            @property
            def records(self): return outer.diag.collect_file(path, uid=os.getuid(), gid=os.getgid())
        return Trace()

    def test_trace_strict_codes_never_serializes_payload_or_exception(self):
        trace = self.trace()
        trace.event('metadata_owner', value=1)
        trace.event('synthetic private payload', value=1)
        trace.event('read_request', value='synthetic private payload')
        self.assertEqual([{k: v for k, v in r.items() if k != 'at_ms'} for r in trace.records], [{'event': 'metadata_owner', 'value': 1}])
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

    def test_trace_failure_does_not_change_transfer(self):
        trace = self.trace()
        trace.event = mock.Mock(side_effect=OSError('synthetic private failure'))
        self.diag.install(self.helper, trace, metadata=lambda: (17, True))
        desktop = type('Desktop', (), {'read': lambda self: b'valid'})()
        self.assertEqual(self.helper.read_value(desktop, lambda: None), b'valid')

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
        trace = self.trace()
        self.diag.install(self.helper, trace, metadata=lambda: (123, True))
        out = io.BytesIO()
        with mock.patch.object(self.diag.time, 'monotonic_ns', return_value=1234567000):
            self.helper.write_frame(out, 'ok', b'synthetic private payload')
        self.assertTrue(trace.records, 'successful original frame has no diagnostic status')
        self.assertEqual(trace.records[-1], {'event': 'frame_ok', 'at_ms': 1234})
        self.assertIn(b'synthetic private payload', out.getvalue())
        self.assertNotIn('private', json.dumps(trace.records))
        trace.event('owner_clear', at_ms=1)
        self.assertEqual(len(trace.records), 1)

    def test_fixed_trace_caps_and_collector_rejects_malformed_records(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'trace'
            trace = self.diag.FileTrace.create(path, uid=os.getuid(), gid=os.getgid())
            for _ in range(100): trace.event('owner_get')
            trace.close()
            self.assertLessEqual(path.stat().st_size, self.diag.MAX_BYTES)
            records = self.diag.collect_file(path, uid=os.getuid(), gid=os.getgid())
            self.assertLessEqual(len(records), self.diag.MAX_EVENTS)
            with self.assertRaises(self.diag.DiagnosticRefused):
                self.diag.FileTrace.create(path, uid=os.getuid(), gid=os.getgid())
            path.write_bytes(b'{"event":"owner_get","value":"private"}\n')
            with self.assertRaises(self.diag.DiagnosticRefused):
                self.diag.collect_file(path, uid=os.getuid(), gid=os.getgid())

    def test_collector_refuses_symlink_permissions_hardlink_and_oversize(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'trace'
            target = Path(directory) / 'target'
            target.write_bytes(b'')
            path.symlink_to(target)
            with self.assertRaises(self.diag.DiagnosticRefused): self.diag.collect_file(path, uid=os.getuid(), gid=os.getgid())
            path.unlink()
            path.write_bytes(b'')
            path.chmod(0o644)
            with self.assertRaises(self.diag.DiagnosticRefused): self.diag.collect_file(path, uid=os.getuid(), gid=os.getgid())
            path.chmod(0o600)
            os.link(path, target.with_name('linked'))
            with self.assertRaises(self.diag.DiagnosticRefused): self.diag.collect_file(path, uid=os.getuid(), gid=os.getgid())
            target.with_name('linked').unlink()
            path.write_bytes(b'x' * (self.diag.MAX_BYTES + 1))
            with self.assertRaises(self.diag.DiagnosticRefused): self.diag.collect_file(path, uid=os.getuid(), gid=os.getgid())

    def test_trace_schema_rejects_wrong_fields_and_duplicates(self):
        for record in ({'event': 'binding'}, {'event': 'owner_pid'}, {'event': 'owner_clear', 'value': 1},
                       {'event': 'metadata_targets', 'value': 2}):
            self.assertFalse(self.diag.valid_record(dict(record, at_ms=1)))
        for value in (True, -1, 0x8000000000000000):
            self.assertFalse(self.diag.valid_record({'event': 'owner_clear', 'at_ms': value}))
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'trace'
            path.write_bytes(b'{"event":"owner_clear","event":"owner_get","at_ms":1}\n'.ljust(256, b'\0'))
            path.chmod(0o600)
            with self.assertRaises(self.diag.DiagnosticRefused): self.diag.collect_file(path, uid=os.getuid(), gid=os.getgid())

    def test_forked_owner_has_disjoint_bounded_slots_and_saturation_receipt(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'trace'
            trace = self.diag.FileTrace.create(path, uid=os.getuid(), gid=os.getgid())
            trace.event('read_begin')
            pid = os.fork()
            if pid == 0:
                for _ in range(100): trace.event('owner_get')
                os._exit(0)
            os.waitpid(pid, 0)
            trace.event('read_complete')
            trace.close()
            records = self.diag.collect_file(path, uid=os.getuid(), gid=os.getgid())
            events = [record['event'] for record in records]
            self.assertIn('read_begin', events)
            self.assertIn('read_complete', events)
            self.assertIn('trace_saturated', events)
            self.assertLessEqual(path.stat().st_size, 16384)


if __name__ == '__main__':
    unittest.main()

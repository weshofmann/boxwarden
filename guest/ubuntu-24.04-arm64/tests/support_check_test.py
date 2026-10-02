import importlib.util
import hashlib
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest import mock

PATH = Path(__file__).resolve().parents[1] / 'support-check.py'

class SupportTests(unittest.TestCase):
    def load(self):
        self.assertTrue(PATH.is_file(), 'fixed support checker is missing')
        spec = importlib.util.spec_from_file_location('support_check', PATH)
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        return module

    def test_real_execution_rejects_invalid_arguments_without_payload(self):
        self.assertTrue(PATH.is_file(), 'fixed support checker is missing')
        result = subprocess.run([sys.executable, '-I', str(PATH), '--prepare', 'private-invalid'], capture_output=True, timeout=5)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn(b'guest support check failed:', result.stderr)
        self.assertNotIn(b'private-invalid', result.stderr)
        self.assertLess(len(result.stderr), 512)

    def test_file_hash_missing_changed_and_link_refused(self):
        helper = self.load()
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'helper'
            with self.assertRaises(helper.SupportError): helper.check_file(path, 'a' * 64)
            path.write_bytes(b'synthetic'); path.chmod(0o755)
            digest = hashlib.sha256(b'synthetic').hexdigest()
            # Real bytes are read; metadata root ownership is a test-only seam.
            original = helper.os.fstat
            def root_stat(fd):
                value = list(original(fd)); value[4] = 0; value[5] = 0
                return os.stat_result(value)
            with mock.patch.object(helper.os, 'fstat', side_effect=root_stat):
                helper.check_file(path, digest)
                with self.assertRaises(helper.SupportError): helper.check_file(path, 'a' * 64)
                path.chmod(0o777)
                with self.assertRaises(helper.SupportError): helper.check_file(path, digest)
            path.unlink(); path.symlink_to('/etc/passwd')
            with self.assertRaises(helper.SupportError): helper.check_file(path, digest)

    def test_real_prepare_process_checks_fixture_files_and_missing_dependencies(self):
        self.assertTrue(PATH.is_file())
        with tempfile.TemporaryDirectory() as directory:
            files = [('boxwarden-guest-bootstrap', b'synthetic bootstrap'), ('boxwarden-guest-clipboard.py', b'synthetic adapter'), ('boxwarden-guest-support-check', PATH.read_bytes())]
            digests = []
            for name, raw in files:
                path = Path(directory) / name
                path.write_bytes(raw); path.chmod(0o755)
                digests.append(hashlib.sha256(raw).hexdigest())
            driver = """
import ctypes, importlib.util, os, pathlib, sys, types
spec = importlib.util.spec_from_file_location('support', sys.argv[1])
m = importlib.util.module_from_spec(spec); spec.loader.exec_module(m)
m.LIBEXEC = pathlib.Path(sys.argv[2])
m.os.geteuid = lambda: 0
m.check_directories = lambda *a: None
original = os.fstat
def root_stat(fd):
    info = original(fd)
    fields = ('st_mode','st_dev','st_ino','st_size','st_mtime_ns','st_nlink')
    result = {key:getattr(info,key) for key in fields}
    return types.SimpleNamespace(**result, st_uid=0, st_gid=0)
m.os.fstat = root_stat
if sys.argv[3] == 'dependencies':
    def missing(_name): raise OSError('private dependency details')
    m.ctypes.CDLL = missing
else:
    m.ctypes.CDLL = lambda name: None
try:
    sys.exit(m.main(['--prepare','1',*sys.argv[4:]]))
except m.SupportError as error:
    print('guest support check failed: ' + str(error), file=sys.stderr)
    sys.exit(1)
"""
            def execute(mode='ok'):
                return subprocess.run([sys.executable, '-I', '-c', driver, str(PATH), directory, mode, *digests], capture_output=True, timeout=5)
            self.assertEqual(execute().returncode, 0)
            dependency = execute('dependencies')
            self.assertNotEqual(dependency.returncode, 0)
            self.assertIn(b'dependency', dependency.stderr)
            self.assertNotIn(b'private dependency details', dependency.stderr)
            path = Path(directory) / files[1][0]
            path.write_bytes(b'changed')
            self.assertIn(b'digest differs', execute().stderr)
            path.unlink()
            self.assertIn(b'missing or unavailable', execute().stderr)

    def test_prepare_checks_dependencies_without_desktop(self):
        helper = self.load()
        loaded = []
        with mock.patch.object(helper.ctypes, 'CDLL', side_effect=lambda name: loaded.append(name)):
            helper.check_dependencies()
        self.assertEqual(loaded, ['libgtk-3.so.0', 'libgdk-3.so.0', 'libX11.so.6'])
        with mock.patch.object(helper.ctypes, 'CDLL', side_effect=OSError('private detail')):
            with self.assertRaises(helper.SupportError) as caught: helper.check_dependencies()
        self.assertNotIn('private detail', str(caught.exception))

    def test_prepare_main_never_dispatches_desktop_or_binding(self):
        helper = self.load()
        with mock.patch.object(helper.os, 'geteuid', return_value=0), mock.patch.object(helper, 'check_directories'), mock.patch.object(helper, 'check_file'), mock.patch.object(helper, 'check_dependencies'), mock.patch.object(helper, 'check_runtime_binding', side_effect=AssertionError('runtime metadata')), mock.patch.object(helper, 'check_desktop', side_effect=AssertionError('desktop')):
            self.assertEqual(helper.main(['--prepare', '1', *(['a' * 64] * 3)]), 0)

    def test_runtime_binding_mismatch_and_invalid_generation_refused(self):
        helper = self.load()
        association = dict(domain='alpha', session_id='00112233-4455-4677-8899-aabbccddeeff', backend_kind='tart', backend_object='candidate')
        marker = dict(version=1, **association, generation='10112233-4455-4677-8899-aabbccddeeff')
        binding = dict(version=1, **association, ca_fingerprint='public', principal='public')
        with mock.patch.object(helper, 'check_directories'), mock.patch.object(helper, 'record', side_effect=[marker, binding]):
            helper.check_runtime_binding()
        for changed in (dict(marker, backend_object='foreign'), dict(marker, generation='invalid')):
            with mock.patch.object(helper, 'check_directories'), mock.patch.object(helper, 'record', side_effect=[changed, binding]):
                with self.assertRaises(helper.SupportError): helper.check_runtime_binding()

    def test_runtime_subprocess_has_closed_environment_and_no_bytecode_writes(self):
        helper = self.load()
        calls = []
        def run(argv, **options):
            calls.append((argv, options))
            return subprocess.CompletedProcess(argv, 0)
        with mock.patch.object(helper.subprocess, 'run', side_effect=run):
            helper.check_desktop()
        argv, options = calls[0]
        self.assertEqual(argv[:4], ['/usr/bin/python3', '-I', '-B', '-c'])
        self.assertEqual(options['stdin'], subprocess.DEVNULL)
        self.assertEqual(options['stdout'], subprocess.DEVNULL)
        self.assertEqual(options['stderr'], subprocess.DEVNULL)
        self.assertEqual(options['env'], helper.ENV)
        self.assertLessEqual(options['timeout'], 8)

    def test_runtime_child_checks_desktop_without_read_or_claim(self):
        helper = self.load()
        self.assertNotIn('.read(', helper.DESKTOP_CHECK)
        self.assertNotIn('.claim(', helper.DESKTOP_CHECK)
        result = subprocess.run([sys.executable, '-c', """
import importlib, types, sys
m = types.ModuleType('importlib.util')
class Session:
    env = {}
    def check(self): pass
class Adapter:
    Session = Session
    def drop_user(self): pass
    class GTKClipboard:
        def read(self): raise AssertionError('clipboard read')
        def claim(self, *args): raise AssertionError('clipboard claim')
class Spec:
    class Loader:
        def exec_module(self, value): pass
    loader = Loader()
m.spec_from_file_location = lambda *a: Spec()
m.module_from_spec = lambda *a: Adapter()
sys.modules['importlib.util'] = m
importlib.util = m
exec(""" + repr(helper.DESKTOP_CHECK) + ")" , str(PATH)], capture_output=True, timeout=5)
        self.assertEqual(result.returncode, 0, result.stderr)

if __name__ == '__main__': unittest.main()

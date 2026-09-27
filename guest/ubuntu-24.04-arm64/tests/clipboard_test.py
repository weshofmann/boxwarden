import importlib.util
import ctypes
import io
import json
import os
import select
import signal
import stat
import tempfile
import time
import unittest
from unittest import mock
from pathlib import Path
PATH = Path(__file__).resolve().parents[1] / 'clipboard.py'
spec = importlib.util.spec_from_file_location('clipboard_helper', PATH)
helper = importlib.util.module_from_spec(spec)
if PATH.exists():
    spec.loader.exec_module(helper)

class Desktop:
    def __init__(self, value=b''):
        self.value, self.claims, self.alive = value, [], True
    def read(self): return self.value
    def claim(self, value):
        self.claims.append(value)
        return self.alive

class ClipboardTests(unittest.TestCase):
    def test_exact_text_empty_newlines_and_limit(self):
        for value in (b'', '雪\n\r\n\t '.encode(), b'a' * 1048576):
            self.assertEqual(helper.validate(value), value)
    def test_invalid_text_fixed_error(self):
        for value in (b'secret\x00', b'secret\xff', b's' * 1048577):
            with self.assertRaises(helper.ClipboardError) as caught: helper.validate(value)
            self.assertNotIn('secret', str(caught.exception))
    def test_absent_differs_from_empty(self):
        with self.assertRaises(helper.ClipboardError): helper.read_value(Desktop(None), lambda: None)
        self.assertEqual(helper.read_value(Desktop(b''), lambda: None), b'')
    def test_read_rechecks_session(self):
        calls = []
        self.assertEqual(helper.read_value(Desktop(b'text'), lambda: calls.append(1)), b'text')
        self.assertEqual(calls, [1, 1])
    def test_stale_read_refused(self):
        calls = []
        def check():
            calls.append(1)
            if len(calls) == 2: raise helper.ClipboardError('desktop unavailable')
        with self.assertRaises(helper.ClipboardError): helper.read_value(Desktop(b'secret'), check)
    def test_invalid_write_never_claims(self):
        desktop = Desktop()
        with self.assertRaises(helper.ClipboardError): helper.claim_value(desktop, b'secret\x00', lambda: None)
        self.assertEqual(desktop.claims, [])
    def test_failed_claim_precommit(self):
        desktop = Desktop(); desktop.alive = False
        with self.assertRaises(helper.ClipboardError) as caught: helper.claim_value(desktop, b'', lambda: None)
        self.assertFalse(caught.exception.unknown)
    def test_native_claim_exception_is_unknown_without_payload_error(self):
        desktop = Desktop()
        desktop.claim = mock.Mock(side_effect=RuntimeError('secret payload'))
        with self.assertRaises(helper.ClipboardError) as caught:
            helper.claim_value(desktop, b'valid', lambda: None)
        self.assertTrue(caught.exception.unknown)
        self.assertNotIn('secret', str(caught.exception))
    def test_postclaim_stale_session_unknown(self):
        calls = []
        def check():
            calls.append(1)
            if len(calls) == 2: raise helper.ClipboardError('desktop unavailable')
        with self.assertRaises(helper.ClipboardError) as caught: helper.claim_value(Desktop(), b'valid', check)
        self.assertTrue(caught.exception.unknown)
    def test_bounded_stdin(self):
        self.assertEqual(helper.read_input(io.BytesIO(b'\n\n')), b'\n\n')
        with self.assertRaises(helper.ClipboardError): helper.read_input(io.BytesIO(b'x' * 1048577))
    def test_session_absent_ambiguous_remote(self):
        good = dict(Id='3', User='1000', Name='boxwarden', Active='yes', Remote='no', Type='wayland', Class='user', State='active')
        self.assertEqual(helper.choose_session([good]), '3')
        for sessions in ([], [good, dict(good, Id='4')], [dict(good, Remote='yes')], [dict(good, User='1001')]):
            with self.assertRaises(helper.ClipboardError): helper.choose_session(sessions)
    def test_other_local_graphical_sessions_refused_even_inactive_or_closing(self):
        good = dict(Id='3', User='1000', Name='boxwarden', Active='yes', Remote='no', Type='wayland', Class='user', State='active')
        for other in (dict(good, Id='4', Active='no', State='online'), dict(good, Id='4', Active='no', State='closing'), dict(good, Id='4', User='1001', Name='other', Active='no')):
            with self.assertRaises(helper.ClipboardError): helper.choose_session([good, other])
    def test_baseline_account_environment(self):
        self.assertEqual(helper.BASE_ENV['HOME'], '/home/boxwarden')
        self.assertEqual(helper.BASE_ENV['USER'], 'boxwarden')
        self.assertEqual(helper.BASE_ENV['LOGNAME'], 'boxwarden')
    def test_closed_env_rejects_stale_values(self):
        env = helper.desktop_env({'DISPLAY': ':0', 'XAUTHORITY': '/run/user/1000/auth', 'LD_PRELOAD': '/evil'})
        self.assertEqual(env['GDK_BACKEND'], 'x11'); self.assertNotIn('LD_PRELOAD', env)
        for values in ({}, {'DISPLAY':'remote:0', 'XAUTHORITY':'/run/user/1000/auth'}, {'DISPLAY':':0', 'XAUTHORITY':'/tmp/auth'}):
            with self.assertRaises(helper.ClipboardError): helper.desktop_env(values)
    def test_exact_frame(self):
        out = io.BytesIO(); value = '\n雪\n'.encode()
        helper.write_frame(out, 'ok', value)
        header, data = out.getvalue().split(b'\n', 1)
        self.assertEqual(json.loads(header), {'version':1, 'status':'ok', 'length':len(data)})
        self.assertEqual(data, value)
class SessionRaceTests(unittest.TestCase):
    GOOD = 'Id=3\nUser=1000\nName=boxwarden\nActive=yes\nRemote=no\nType=wayland\nClass=user\nState=active\n'
    def test_disappeared_ssh_session_rescans_without_losing_graphical_owner(self):
        lists = []
        def command(argv):
            if argv[1] == 'list-sessions':
                lists.append(1)
                return '3 1000 boxwarden seat0 tty2\n4 1000 boxwarden - -\n' if len(lists) == 1 else '3 1000 boxwarden seat0 tty2\n'
            if argv[2] == '4': raise helper.ClipboardError('desktop unavailable')
            return self.GOOD
        with mock.patch.object(helper, 'command', side_effect=command):
            self.assertEqual(helper.session_id(), '3')
        self.assertEqual(len(lists), 2)
    def test_repeated_session_query_failure_refuses(self):
        def command(argv):
            if argv[1] == 'list-sessions': return '3 1000 boxwarden seat0 tty2\n'
            raise helper.ClipboardError('desktop unavailable')
        with mock.patch.object(helper, 'command', side_effect=command):
            with self.assertRaises(helper.ClipboardError): helper.session_id()
    def test_replaced_graphical_identity_still_refused(self):
        session = helper.Session.__new__(helper.Session)
        session.identity = '3'; session.env = {}
        with mock.patch.object(helper, 'session_id', return_value='5'):
            with self.assertRaises(helper.ClipboardError): session.check()

class XwaylandActivationTests(unittest.TestCase):
    ENV = dict(helper.BASE_ENV, DISPLAY=':0', XAUTHORITY='/run/user/1000/auth', GDK_BACKEND='x11')
    def test_session_connects_before_binding_and_only_once(self):
        calls = []
        def wake(env):
            self.assertEqual(env, self.ENV)
            calls.append('wake')
        def bind(identity, env):
            self.assertEqual((identity, env), ('3', self.ENV))
            calls.append('bind')
            return ('exact',)
        with mock.patch.object(helper, 'session_id', return_value='3'), \
             mock.patch.object(helper, 'environment', return_value=self.ENV), \
             mock.patch.object(helper, 'activate_xwayland', side_effect=wake), \
             mock.patch.object(helper, 'desktop_binding', side_effect=bind):
            session = helper.Session()
            session.check()
        self.assertEqual(calls, ['wake', 'bind', 'bind', 'bind'])

    def test_failed_connection_never_admits_desktop_binding(self):
        with mock.patch.object(helper, 'session_id', return_value='3'), \
             mock.patch.object(helper, 'environment', return_value=self.ENV), \
             mock.patch.object(helper, 'activate_xwayland', side_effect=helper.ClipboardError('desktop display unavailable')), \
             mock.patch.object(helper, 'desktop_binding') as bind:
            with self.assertRaises(helper.ClipboardError): helper.Session()
        bind.assert_not_called()

    def test_x11_connection_closes_with_closed_environment(self):
        display = mock.Mock()
        display.XOpenDisplay.return_value = 17
        with mock.patch.dict(os.environ, {'HOST_SECRET': 'synthetic'}, clear=True), \
             mock.patch.object(helper.C, 'CDLL', return_value=display) as load:
            helper.activate_xwayland(self.ENV)
            self.assertEqual(dict(os.environ), self.ENV)
        load.assert_called_once_with('libX11.so.6')
        display.XOpenDisplay.assert_called_once_with(None)
        display.XCloseDisplay.assert_called_once_with(17)

    def test_failed_x11_open_refuses_without_claim(self):
        display = mock.Mock()
        display.XOpenDisplay.return_value = None
        with mock.patch.dict(os.environ, {}, clear=True), \
             mock.patch.object(helper.C, 'CDLL', return_value=display):
            with self.assertRaises(helper.ClipboardError): helper.activate_xwayland(self.ENV)
        display.XCloseDisplay.assert_not_called()

    def test_native_x11_open_stall_dies_at_existing_alarm(self):
        reader, writer = os.pipe()
        libc = ctypes.CDLL(None)
        libc.usleep.argtypes = [ctypes.c_uint]
        pid = os.fork()
        if pid == 0:
            os.close(reader)
            display = mock.Mock()
            def blocked_open(_name):
                os.write(writer, b's')
                libc.usleep(1000000)
                return 17
            display.XOpenDisplay.side_effect = blocked_open
            signal.setitimer(signal.ITIMER_REAL, 0.1)
            with mock.patch.object(helper.C, 'CDLL', return_value=display):
                helper.activate_xwayland(self.ENV)
            os._exit(99)
        os.close(writer)
        try:
            ready, _, _ = select.select([reader], [], [], 0.3)
            self.assertTrue(ready)
            self.assertEqual(os.read(reader, 1), b's')
            deadline = time.monotonic() + 0.5
            while time.monotonic() < deadline:
                ended, status = os.waitpid(pid, os.WNOHANG)
                if ended: break
                time.sleep(0.01)
            else: self.fail('native X11 open outlived its alarm')
            pid = 0
            self.assertTrue(os.WIFSIGNALED(status))
            self.assertEqual(os.WTERMSIG(status), signal.SIGALRM)
        finally:
            os.close(reader)
            if pid:
                try: os.kill(pid, signal.SIGKILL)
                except ProcessLookupError: pass
                os.waitpid(pid, 0)

class OwnerHandoffTests(unittest.TestCase):
    class Session:
        def check(self): pass
    def test_desktop_failure_before_claim_is_error(self):
        with mock.patch.object(helper, 'GTKClipboard', side_effect=helper.ClipboardError('display unavailable')):
            with self.assertRaises(helper.ClipboardError) as caught:
                helper.owner_write(b'valid', self.Session())
        self.assertFalse(caught.exception.unknown)
    def test_failed_claim_is_error_after_fork(self):
        desktop = Desktop(); desktop.alive = False
        with mock.patch.object(helper, 'GTKClipboard', return_value=desktop):
            with self.assertRaises(helper.ClipboardError) as caught:
                helper.owner_write(b'valid', self.Session())
        self.assertFalse(caught.exception.unknown)
    def test_successful_handoff_returns_without_owner_lifetime(self):
        desktop = Desktop(); desktop.retain = lambda check: None
        with mock.patch.object(helper, 'GTKClipboard', return_value=desktop):
            self.assertIsNone(helper.owner_write(b'valid', self.Session()))

class PreclaimLifetimeTests(unittest.TestCase):
    def collect(self, reader, timeout=0.6):
        deadline = time.monotonic() + timeout
        data = b''
        while time.monotonic() < deadline:
            ready, _, _ = select.select([reader], [], [], max(0, deadline - time.monotonic()))
            if not ready: break
            block = os.read(reader, 64)
            if not block: return data
            data += block
        self.fail('bounded owner did not finish')

    def test_stalled_preclaim_never_mutates_after_parent_timeout(self):
        reader, writer = os.pipe()
        class SlowSession:
            def check(self): time.sleep(0.12)
        class FakeGTK:
            def claim(self, value): os.write(writer, b'claim'); return True
            def retain(self, check): pass
        try:
            with mock.patch.object(helper, 'TIMEOUT', 0.03), mock.patch.object(helper, 'GTKClipboard', return_value=FakeGTK()):
                with self.assertRaises(helper.ClipboardError): helper.owner_write(b'valid', SlowSession())
            os.close(writer); writer = None
            self.assertEqual(self.collect(reader), b'')
        finally:
            os.close(reader)
            if writer is not None: os.close(writer)

    def test_native_preclaim_stall_exits_without_retaining_payload(self):
        reader, writer = os.pipe()
        child = []
        real_fork = os.fork
        def tracked_fork():
            pid = real_fork()
            if pid > 0:
                child.append(pid)
            return pid
        class NativeStall:
            def __init__(self):
                os.write(writer, b's')
                ctypes.CDLL(None).system(b'/bin/sleep 1')
            def claim(self, value):
                os.write(writer, b'c')
                return True
            def retain(self, check): pass
        try:
            with mock.patch.object(helper.os, 'fork', side_effect=tracked_fork), \
                 mock.patch.object(helper, 'GTKClipboard', NativeStall):
                with self.assertRaises(helper.ClipboardError):
                    helper.owner_write(b'valid', OwnerHandoffTests.Session(), time.monotonic() + 0.1)
            ready, _, _ = select.select([reader], [], [], 0.1)
            self.assertTrue(ready, 'native stall did not start before the deadline')
            self.assertEqual(os.read(reader, 1), b's')
            self.assertEqual(len(child), 1)
            end = time.monotonic() + 0.45
            while time.monotonic() < end:
                try:
                    pid, _ = os.waitpid(child[0], os.WNOHANG)
                except ChildProcessError:
                    pid = child[0]
                if pid == child[0]:
                    break
                time.sleep(0.01)
            self.assertEqual(pid, child[0], 'native-blocked preclaim child survived the deadline')
            child.clear()
            ready, _, _ = select.select([reader], [], [], 0)
            if ready:
                self.assertNotIn(b'c', os.read(reader, 64))
        finally:
            os.close(reader)
            os.close(writer)
            for pid in child:
                try: os.kill(pid, signal.SIGKILL)
                except ProcessLookupError: pass
                try: os.waitpid(pid, 0)
                except ChildProcessError: pass

    def test_parent_disconnect_before_claim_prevents_mutation(self):
        reader, writer = os.pipe()
        class SlowSession:
            def check(self): os.write(writer, b's'); time.sleep(0.12)
        class FakeGTK:
            def claim(self, value): os.write(writer, b'claim'); return True
            def retain(self, check): pass
        coordinator = os.fork()
        if coordinator == 0:
            os.close(reader)
            try:
                with mock.patch.object(helper, 'TIMEOUT', 0.3), mock.patch.object(helper, 'GTKClipboard', return_value=FakeGTK()):
                    helper.owner_write(b'valid', SlowSession())
            except BaseException: pass
            os._exit(0)
        os.close(writer)
        try:
            ready, _, _ = select.select([reader], [], [], 0.5)
            self.assertTrue(ready)
            self.assertEqual(os.read(reader, 1), b's')
            os.kill(coordinator, signal.SIGKILL); os.waitpid(coordinator, 0)
            self.assertEqual(self.collect(reader), b'')
        finally:
            os.close(reader)
            try: os.kill(coordinator, signal.SIGKILL)
            except ProcessLookupError: pass
            try: os.waitpid(coordinator, 0)
            except ChildProcessError: pass

    def test_committed_owner_survives_receipt_loss(self):
        reader, writer = os.pipe()
        class Session:
            def check(self): pass
        class FakeGTK:
            def __init__(self, parent_pid): self.parent_pid = parent_pid
            def claim(self, value):
                os.write(writer, b'c')
                # The mutation has happened, but its operation parent cannot
                # receive the claim receipt. This must not kill the owner.
                os.kill(self.parent_pid, signal.SIGKILL)
                time.sleep(0.02)
                return True
            def retain(self, check): time.sleep(0.04); os.write(writer, b'r')
        coordinator = os.fork()
        if coordinator == 0:
            os.close(reader)
            try:
                with mock.patch.object(helper, 'TIMEOUT', 0.3), mock.patch.object(helper, 'GTKClipboard', return_value=FakeGTK(os.getpid())):
                    helper.owner_write(b'valid', Session())
            except BaseException: pass
            os._exit(0)
        os.close(writer)
        try:
            self.assertEqual(self.collect(reader), b'cr')
            os.waitpid(coordinator, 0)
        finally:
            os.close(reader)
            try: os.kill(coordinator, signal.SIGKILL)
            except ProcessLookupError: pass
            try: os.waitpid(coordinator, 0)
            except ChildProcessError: pass

    def test_committed_owner_survives_normal_parent_return_and_deadline(self):
        reader, writer = os.pipe()
        class Session:
            def check(self): pass
        class FakeGTK:
            def claim(self, value): os.write(writer, b'c'); return True
            def retain(self, check): time.sleep(0.08); os.write(writer, b'r')
        try:
            with mock.patch.object(helper, 'TIMEOUT', 0.03), mock.patch.object(helper, 'GTKClipboard', return_value=FakeGTK()):
                helper.owner_write(b'valid', Session())
            os.close(writer); writer = None
            self.assertEqual(self.collect(reader), b'cr')
        finally:
            os.close(reader)
            if writer is not None: os.close(writer)

class NativeGTKTests(unittest.TestCase):
    def make_desktop(self, value=b'raw', fmt=8, atom=17, length=None):
        import ctypes as C
        desktop = helper.GTKClipboard.__new__(helper.GTKClipboard)
        desktop.atom = 17; desktop.clipboard = 23; desktop.owned = False
        desktop.get_callback = C.CFUNCTYPE(None, C.c_void_p, C.c_void_p, C.c_uint, C.c_void_p)
        desktop.clear_callback = C.CFUNCTYPE(None, C.c_void_p, C.c_void_p)
        desktop.read_callback = C.CFUNCTYPE(None, C.c_void_p, C.c_void_p, C.c_void_p)
        self.buffer = C.create_string_buffer(value)
        outer = self
        class GTK:
            def gtk_clipboard_request_contents(self, clipboard, target, callback, data):
                outer.assertEqual((clipboard, target), (23, 17))
                callback(clipboard, 1, data)
            def gtk_selection_data_get_length(self, selection): return len(value) if length is None else length
            def gtk_selection_data_get_format(self, selection): return fmt
            def gtk_selection_data_get_data_type(self, selection): return atom
            def gtk_selection_data_get_data(self, selection): return C.addressof(outer.buffer)
            def gtk_clipboard_set_with_data(self, clipboard, targets, count, get, clear, data):
                outer.assertEqual(count, 1)
                outer.assertEqual(targets[0].target, b'UTF8_STRING')
                self.get, self.clear = get, clear
                return 1
            def gtk_selection_data_set(self, selection, target, fmt, data, n):
                self.served = (target, fmt, C.string_at(data, n))
            def gtk_events_pending(self): return 0
        desktop.gtk = GTK()
        return desktop

    def test_native_raw_read_exact_empty_unicode_newlines(self):
        for value in (b'', '雪\n\r\n'.encode(), b'x' * 1048576):
            desktop = self.make_desktop(value)
            self.assertEqual(helper.read_value(desktop, lambda: None), value)

    def test_native_invalid_representation_and_absence(self):
        for options in ({'fmt':16}, {'atom':18}, {'length':1048577}, {'length':-1}, {'value':b'bad\xff'}, {'value':b'bad\x00'}):
            with self.assertRaises(helper.ClipboardError):
                helper.read_value(self.make_desktop(**options), lambda: None)

    def test_native_owner_serves_exact_bytes_then_loses_selection(self):
        for value in (b'', '雪\n\n'.encode()):
            desktop = self.make_desktop()
            self.assertTrue(desktop.claim(value))
            desktop.gtk.get(23, 1, 0, None)
            self.assertEqual(desktop.gtk.served, (17, 8, value))
            self.assertTrue(desktop.owned)
            desktop.gtk.clear(23, None)
            self.assertFalse(desktop.owned)

    def test_owner_stops_when_session_invalid(self):
        desktop = self.make_desktop(); desktop.owned = True
        def check(): raise helper.ClipboardError('desktop session changed')
        with self.assertRaises(helper.ClipboardError): desktop.retain(check)

    def test_owner_lost_exits_without_session_poll(self):
        desktop = self.make_desktop(); calls=[]
        desktop.retain(lambda: calls.append(1))
        self.assertEqual(calls, [])


class SharedDeadlineTests(unittest.TestCase):
    def test_absolute_deadline_conversion(self):
        with mock.patch.object(helper.time, 'time_ns', return_value=100000000000), mock.patch.object(helper.time, 'monotonic', return_value=50):
            operation, deadline = helper.operation_deadline(['write', '--deadline-unix-ns', '100250000000'])
        self.assertEqual(operation, 'write'); self.assertEqual(deadline, 50.25)
    def test_expired_and_invalid_deadline_refused(self):
        with mock.patch.object(helper.time, 'time_ns', return_value=100000000000):
            for text in ('100000000000', '99999999999', '-1', 'secret', '1' * 100):
                with self.assertRaises(helper.ClipboardError):
                    helper.operation_deadline(['write', '--deadline-unix-ns', text])
    def test_explicit_deadline_does_not_restart_owner_window(self):
        with mock.patch.object(helper, 'GTKClipboard') as desktop:
            with self.assertRaises(helper.ClipboardError): helper.owner_write(b'valid', None, time.monotonic() - 0.01)
        desktop.assert_not_called()
    def test_direct_synthetic_operation_has_bounded_default(self):
        with mock.patch.object(helper.time, 'monotonic', return_value=50):
            self.assertEqual(helper.operation_deadline(['read']), ('read', 80))

class DesktopBindingTests(unittest.TestCase):
    ENV = dict(helper.BASE_ENV, DISPLAY=':0', XAUTHORITY='/run/user/1000/auth', GDK_BACKEND='x11')
    def processes(self):
        return [dict(pid=1253, uid=1000, ppid=1047, start=1462, exe='/usr/bin/gnome-shell', argv=()),
                dict(pid=3265, uid=1000, ppid=1253, start=21544, exe='/usr/bin/Xwayland', argv=('/usr/bin/Xwayland', ':0', '-auth', '/run/user/1000/auth'))]
    def patches(self, inventory=None, pid=1253, controller=':1.4'):
        from contextlib import ExitStack
        stack=ExitStack()
        stack.enter_context(mock.patch.object(helper, 'session_controller', return_value=controller))
        stack.enter_context(mock.patch.object(helper, 'controller_pid', return_value=pid))
        stack.enter_context(mock.patch.object(helper, 'process_inventory', return_value=inventory or self.processes()))
        return stack
    def test_controller_binds_exact_current_shell_and_xserver(self):
        with self.patches():
            proof=helper.desktop_binding('1', self.ENV)
        self.assertIn(':1.4', proof)
    def test_absent_or_mismatched_controller_refuses(self):
        with self.patches(pid=999):
            with self.assertRaises(helper.ClipboardError): helper.desktop_binding('1', self.ENV)
        with self.patches(), mock.patch.object(helper, 'session_controller', side_effect=helper.ClipboardError('desktop unavailable')):
            with self.assertRaises(helper.ClipboardError): helper.desktop_binding('1', self.ENV)
    def test_extra_shell_or_xserver_and_wrong_parent_refuse(self):
        for kind in ('shell', 'xserver', 'parent', 'uid'):
            processes=self.processes()
            if kind=='shell': processes.append(dict(processes[0],pid=1254))
            elif kind=='xserver': processes.append(dict(processes[1],pid=3266))
            elif kind=='parent': processes[1]['ppid']=999
            else: processes[0]['uid']=1001
            with self.patches(processes):
                with self.assertRaises(helper.ClipboardError): helper.desktop_binding('1', self.ENV)
    def test_wrong_display_auth_or_duplicate_auth_refuse(self):
        for argv in (('/usr/bin/Xwayland', ':1', '-auth', '/run/user/1000/auth'),
                     ('/usr/bin/Xwayland', ':0', '-auth', '/run/user/1000/stale'),
                     ('/usr/bin/Xwayland', ':0', '-auth', '/run/user/1000/auth', '-auth', '/run/user/1000/auth')):
            processes=self.processes(); processes[1]['argv']=argv
            with self.patches(processes):
                with self.assertRaises(helper.ClipboardError): helper.desktop_binding('1', self.ENV)
    def test_reused_pid_and_changed_controller_refuse(self):
        for kind in ('pid','controller'):
            with self.patches():
                if kind=='pid':
                    new=self.processes();new[1]['start']+=1
                    patch=mock.patch.object(helper, 'process_inventory', side_effect=[self.processes(),new])
                else: patch=mock.patch.object(helper, 'session_controller', side_effect=[':1.4',':1.5'])
                with patch, self.assertRaises(helper.ClipboardError): helper.desktop_binding('1', self.ENV)
    def test_session_binding_change_prevents_read_and_claim(self):
        session=helper.Session.__new__(helper.Session);session.identity='1';session.env=self.ENV;session.binding=('old',)
        desktop=Desktop(b'valid')
        with mock.patch.object(helper,'session_id',return_value='1'), mock.patch.object(helper,'environment',return_value=self.ENV), mock.patch.object(helper,'desktop_binding',return_value=('new',)):
            with self.assertRaises(helper.ClipboardError): helper.read_value(desktop,session.check)
            with self.assertRaises(helper.ClipboardError): helper.claim_value(desktop,b'valid',session.check)
        self.assertEqual(desktop.claims,[])
    def test_native_x11_session_refused(self):
        session=dict(Id='1', User='1000', Name='boxwarden', Active='yes', Remote='no', Type='x11', Class='user', State='active')
        with self.assertRaises(helper.ClipboardError): helper.choose_session([session])

class ControllerRecordTests(unittest.TestCase):
    def metadata(self, directory=False, **changes):
        values=dict(st_mode=(stat.S_IFDIR|0o755) if directory else (stat.S_IFREG|0o644), st_uid=0, st_gid=0, st_nlink=1, st_dev=1, st_ino=2)
        values.update(changes); return mock.Mock(**values)
    def test_exact_protected_controller_field(self):
        def lstat(path): return self.metadata(directory=not path.endswith('/1'))
        for raw in (b'CONTROLLER=:1.4\n', b'USER=1000\nCONTROLLER=:1.4\nSTATE=active\n'):
            with mock.patch.object(helper.os,'lstat',side_effect=lstat), mock.patch.object(helper,'bounded_file',return_value=(raw,self.metadata())):
                self.assertEqual(helper.session_controller('1'),':1.4')
    def test_missing_duplicate_malformed_or_changed_controller_refused(self):
        def lstat(path): return self.metadata(directory=not path.endswith('/1'))
        for raw in (b'USER=1000\n', b'CONTROLLER=:1.4\nCONTROLLER=:1.5\n', b'CONTROLLER=secret\n', b'CONTROLLER=:1.4\x00\n'):
            with mock.patch.object(helper.os,'lstat',side_effect=lstat), mock.patch.object(helper,'bounded_file',return_value=(raw,self.metadata())):
                with self.assertRaises(helper.ClipboardError): helper.session_controller('1')
        with mock.patch.object(helper.os,'lstat',side_effect=lstat), mock.patch.object(helper,'bounded_file',return_value=(b'CONTROLLER=:1.4\n',self.metadata(st_ino=3))):
            with self.assertRaises(helper.ClipboardError): helper.session_controller('1')
    def test_unsafe_root_record_or_id_never_read(self):
        for changes in (dict(st_uid=1000), dict(st_mode=stat.S_IFLNK|0o777), dict(st_mode=stat.S_IFREG|0o666), dict(st_nlink=2)):
            def lstat(path): return self.metadata(**changes) if path.endswith('/1') else self.metadata(directory=True)
            with mock.patch.object(helper.os,'lstat',side_effect=lstat), mock.patch.object(helper,'bounded_file') as read:
                with self.assertRaises(helper.ClipboardError): helper.session_controller('1')
            read.assert_not_called()
        with mock.patch.object(helper,'bounded_file') as read:
            with self.assertRaises(helper.ClipboardError): helper.session_controller('../secret')
        read.assert_not_called()
    def test_bus_reply_is_unsigned_pid_without_extra_data(self):
        with mock.patch.object(helper,'command',return_value='u 1253\n'):
            self.assertEqual(helper.controller_pid(':1.4'),1253)
        for raw in ('u 0\n','i 1253\n','u 4294967296\n','u 1253\nsecret','u -1\n'):
            with mock.patch.object(helper,'command',return_value=raw), self.assertRaises(helper.ClipboardError) as caught:
                helper.controller_pid(':1.4')
            self.assertNotIn('secret',str(caught.exception))
    def test_metadata_file_bound_and_symlink_refusal(self):
        with tempfile.TemporaryDirectory() as directory:
            path=Path(directory)/'metadata';path.write_bytes(b'a'*11)
            with self.assertRaises(helper.ClipboardError): helper.bounded_file(str(path),10)
            link=Path(directory)/'link';link.symlink_to(path)
            with self.assertRaises(OSError): helper.bounded_file(str(link))

class PrivilegedExeInventoryTests(unittest.TestCase):
    def stat_value(self, start=42):
        return b'1047 (process) '+b' '.join([b'S',b'1']+[b'0']*17+[str(start).encode()])
    def fixture(self, paths):
        from contextlib import ExitStack, nullcontext
        from types import SimpleNamespace
        stack=ExitStack()
        stack.enter_context(mock.patch.object(helper,'command',side_effect=AssertionError('no manager exemption')))
        entries=[SimpleNamespace(name=str(pid)) for pid in paths]
        stack.enter_context(mock.patch.object(helper.os,'scandir',return_value=nullcontext(entries)))
        stack.enter_context(mock.patch.object(helper.os,'lstat',return_value=SimpleNamespace(st_mode=stat.S_IFDIR|0o700,st_uid=1000)))
        stack.enter_context(mock.patch.object(helper,'bounded_file',side_effect=lambda path,limit:(self.stat_value(),None)))
        stack.enter_context(mock.patch.object(helper.os,'readlink',side_effect=PermissionError(13,'permission denied')))
        def run(argv,**kwargs): return SimpleNamespace(stdout=paths[int(argv[-1].split('/')[2])].encode()+b'\n')
        runner=stack.enter_context(mock.patch.object(helper.subprocess,'run',side_effect=run))
        return stack,runner
    def test_systemd_and_sd_pam_actual_exes_excluded_without_exemptions(self):
        stack,runner=self.fixture({1047:'/usr/lib/systemd/systemd',1048:'/usr/lib/systemd/systemd'})
        with stack: self.assertEqual(helper.process_inventory(),[])
        self.assertEqual(runner.call_count,4)
    def test_eacces_known_shell_is_discovered_and_unknown_exe_classified(self):
        stack,runner=self.fixture({1253:'/usr/bin/gnome-shell',9999:'/usr/bin/unrelated'})
        with stack: records=helper.process_inventory()
        self.assertEqual([p['pid'] for p in records],[1253])
        argv,kwargs=runner.call_args
        self.assertEqual(argv[0],['/usr/bin/sudo','-n','--','/usr/bin/readlink','--','/proc/9999/exe'])
        self.assertEqual(kwargs['env'],helper.BASE_ENV)
        self.assertEqual(kwargs['stdin'],helper.subprocess.DEVNULL)
        self.assertEqual(kwargs['stderr'],helper.subprocess.DEVNULL)
        self.assertGreater(kwargs['timeout'],0);self.assertLessEqual(kwargs['timeout'],2)
    def test_failed_or_malformed_fallback_refuses_without_error_detail(self):
        from types import SimpleNamespace
        for output in (b'',b'/usr/bin/gnome-shell',b'/usr/bin/gnome-shell\n\n',b' relative\n',b'/secret\x00\n',b'/'+b'x'*4096+b'\n'):
            stack,_=self.fixture({1253:'/usr/bin/gnome-shell'})
            with stack,mock.patch.object(helper.subprocess,'run',return_value=SimpleNamespace(stdout=output)),self.assertRaises(helper.ClipboardError) as caught:
                helper.process_inventory()
            self.assertNotIn('secret',str(caught.exception))
        stack,_=self.fixture({1253:'/usr/bin/gnome-shell'})
        with stack,mock.patch.object(helper.subprocess,'run',side_effect=helper.subprocess.CalledProcessError(1,'secret')),self.assertRaises(helper.ClipboardError): helper.process_inventory()
    def test_pid_start_churn_and_changed_exe_refuse(self):
        from types import SimpleNamespace
        for kind in ('start','exe'):
            stack,_=self.fixture({1253:'/usr/bin/gnome-shell'})
            with stack:
                patch=mock.patch.object(helper,'bounded_file',side_effect=[(self.stat_value(42),None),(self.stat_value(43),None)]) if kind=='start' else mock.patch.object(helper.subprocess,'run',side_effect=[SimpleNamespace(stdout=b'/usr/bin/gnome-shell\n'),SimpleNamespace(stdout=b'/usr/bin/unrelated\n')])
                with patch,self.assertRaises(helper.ClipboardError): helper.process_inventory()
    def test_zombie_is_absent_but_cannot_satisfy_required_compositor_proof(self):
        stack,runner=self.fixture({1253:'/usr/bin/gnome-shell'})
        zombie=self.stat_value().replace(b') S ',b') Z ')
        with stack,mock.patch.object(helper,'bounded_file',return_value=(zombie,None)):
            self.assertEqual(helper.process_inventory(),[])
        runner.assert_not_called()
        with mock.patch.object(helper,'session_controller',return_value=':1.4'),mock.patch.object(helper,'controller_pid',return_value=1253),mock.patch.object(helper,'process_inventory',return_value=[]),self.assertRaises(helper.ClipboardError):
            helper.desktop_binding('1',DesktopBindingTests.ENV)
    def test_truncated_or_malformed_process_stat_refuses(self):
        for raw in (b'1253 (process) S 1',b'invalid'):
            stack,_=self.fixture({1253:'/usr/bin/gnome-shell'})
            with stack,mock.patch.object(helper,'bounded_file',return_value=(raw,None)),self.assertRaises(helper.ClipboardError): helper.process_inventory()

    def test_non_eacces_never_invokes_sudo_and_deadline_clamps_fallback(self):
        with mock.patch.object(helper.os,'readlink',side_effect=OSError(5,'secret')),mock.patch.object(helper.subprocess,'run') as runner,self.assertRaises(OSError): helper.process_exe('1253',time.monotonic()+1)
        runner.assert_not_called()
        from types import SimpleNamespace
        with mock.patch.object(helper.os,'readlink',side_effect=PermissionError(13,'denied')),mock.patch.object(helper.time,'monotonic',return_value=10),mock.patch.object(helper.subprocess,'run',return_value=SimpleNamespace(stdout=b'/usr/bin/gnome-shell\n')) as runner:
            self.assertEqual(helper.process_exe('1253',10.25),'/usr/bin/gnome-shell')
            self.assertEqual(runner.call_args[1]['timeout'],0.25)
        with mock.patch.object(helper.os,'readlink',side_effect=PermissionError(13,'denied')),mock.patch.object(helper.subprocess,'run') as runner,self.assertRaises(helper.ClipboardError): helper.process_exe('1253',time.monotonic()-1)
        runner.assert_not_called()
        with mock.patch.object(helper.os,'readlink') as read,self.assertRaises(helper.ClipboardError): helper.process_exe('../secret',time.monotonic()+1)
        read.assert_not_called()

if __name__ == '__main__': unittest.main()

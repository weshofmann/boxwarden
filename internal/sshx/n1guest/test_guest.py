"""Private deterministic fixtures. No real guest, network or privilege effects."""
import hashlib, importlib.util, io, json, os, pathlib, struct, tempfile, unittest

HERE = pathlib.Path(__file__).resolve().parent

def load(name):
    path = HERE / (name + '.py')
    if not path.exists():
        raise AssertionError('fixed guest component missing: ' + name)
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module

def fixture_group(root):
    # BSD temp directories inherit their parent's GID. Establish the synthetic
    # positive fixture's exact primary-group metadata; never relax admission.
    for path in [root, *root.rglob('*')]:
        os.chown(path, -1, os.getgid())

BINDING = {'version':1,'domain':'n1qualification','session_id':'123e4567-e89b-42d3-a456-426614174000',
           'backend_kind':'tart','backend_object':'test-control','generation':'123e4567-e89b-42d3-a456-426614174001'}

class GuestTests(unittest.TestCase):
    def test_framing_rejects_hash_mismatch_trailing_and_duplicate_before_writes(self):
        s = load('stager')
        binding = dict(BINDING)
        artifacts = [b'fixture-' + name.encode() for name in s.NAMES]
        header = {'binding':binding,'artifacts':[{'name':name,'length':len(data),'sha256':hashlib.sha256(data).hexdigest()} for name,data in zip(s.NAMES,artifacts)]}
        raw = json.dumps(header,separators=(',',':')).encode()
        frame = struct.pack('!I',len(raw)) + raw + b''.join(artifacts)
        self.assertEqual(s.read_frame(io.BytesIO(frame)),(header,artifacts))
        for bad in (frame+b'x', frame[:-1],struct.pack('!I',65537),struct.pack('!I',2)+b'{}',frame[:4]+raw.replace(b'"version":1',b'"version":1,"version":1')+b''.join(artifacts)):
            with self.assertRaises(s.Invalid): s.read_frame(io.BytesIO(bad))
        mutated = frame[:-1]+bytes([frame[-1]^1])
        with self.assertRaises(s.Invalid): s.read_frame(io.BytesIO(mutated))

    def test_stage_old_helper_and_absence_gate_precede_any_publication(self):
        s = load('stager')
        # macOS fixture has no Linux ACL API. The injected private fixture seam
        # substitutes only ACL admission, not staging/read/publish behavior.
        s.no_acl=lambda fd: None
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            lib = root/'usr/local/libexec'; lib.mkdir(parents=True)
            for parent in (root,root/'usr',root/'usr/local',lib): parent.chmod(0o755)
            old=b'old helper fixture'; (lib/s.NAMES[0]).write_bytes(old); (lib/s.NAMES[0]).chmod(0o755)
            artifact_bytes=[b'new-' + name.encode() for name in s.NAMES]
            header={'binding':dict(BINDING),'artifacts':[{'name':name,'length':len(data),'sha256':hashlib.sha256(data).hexdigest()} for name,data in zip(s.NAMES,artifact_bytes)]}
            active=root/'etc/ssh/boxwarden/active'; active.mkdir(parents=True)
            manifest={k:v for k,v in BINDING.items() if k!='generation'}
            manifest.update(ca_fingerprint='SHA256:'+'A'*43,principal='boxwarden-session-'+BINDING['session_id'])
            (active/'management-binding.json').write_text(json.dumps(manifest)); (active/'management-binding.json').chmod(0o600)
            fixture_group(root)
            before=sorted(p.name for p in lib.iterdir())
            with self.assertRaises(s.Invalid): s.stage(header,artifact_bytes,root,os.getuid(),os.getgid())
            self.assertEqual(before,sorted(p.name for p in lib.iterdir()))
            s.OLD_HELPER=hashlib.sha256(old).hexdigest(); s.NEW_HELPER=header['artifacts'][0]['sha256']; s.PRODUCTION=header['artifacts'][3]['sha256']
            adapter=lib/s.NAMES[2]; adapter.write_bytes(b'unexpected')
            with self.assertRaises(s.Invalid): s.stage(header,artifact_bytes,root,os.getuid(),os.getgid())
            adapter.unlink()
            result=s.stage(header,artifact_bytes,root,os.getuid(),os.getgid())
            self.assertEqual(result['code'],'staged')
            for name,data in zip(s.NAMES,artifact_bytes):
                self.assertEqual((lib/name).read_bytes(),data)
                self.assertEqual((lib/name).stat().st_mode&0o777,0o755)
            self.assertFalse((root/'run').exists())
            with self.assertRaises(s.Invalid): s.stage(header,artifact_bytes,root,os.getuid(),os.getgid())

    def test_inspector_generation_and_artifact_drift_and_namespace_absence(self):
        m=load('inspector'); m.no_acl=lambda fd: None
        with tempfile.TemporaryDirectory() as tmp:
            root=pathlib.Path(tmp);lib=root/'usr/local/libexec';lib.mkdir(parents=True)
            active=root/'etc/ssh/boxwarden/active';active.mkdir(parents=True)
            runtime=root/'run/boxwarden';runtime.mkdir(parents=True)
            for p in (root,root/'usr',root/'usr/local',lib,root/'etc',root/'etc/ssh',root/'etc/ssh/boxwarden',active,root/'run',runtime): p.chmod(0o755)
            manifest={k:v for k,v in BINDING.items() if k!='generation'}
            manifest.update(ca_fingerprint='SHA256:'+'A'*43,principal='boxwarden-session-'+BINDING['session_id'])
            for path,value in ((active/'management-binding.json',manifest),(runtime/'clipboard-generation.json',BINDING)):
                path.write_text(json.dumps(value));path.chmod(0o600)
            artifacts=[b'fixture-'+name.encode() for name in m.NAMES]
            ds=[{'name':name,'length':len(data),'sha256':hashlib.sha256(data).hexdigest()} for name,data in zip(m.NAMES,artifacts)]
            m.NEW_HELPER=ds[0]['sha256'];m.PRODUCTION=ds[3]['sha256']
            for name,data in zip(m.NAMES,artifacts): (lib/name).write_bytes(data);(lib/name).chmod(0o755)
            fixture_group(root)
            request={'binding':dict(BINDING),'artifacts':ds,'phase':'final'}
            self.assertEqual(m.inspect(request,root,os.getuid(),os.getgid())['code'],'inspected')
            bad=dict(BINDING);bad['generation']='123e4567-e89b-42d3-a456-426614174004'
            (runtime/'clipboard-generation.json').write_text(json.dumps(bad))
            with self.assertRaises(m.Invalid): m.inspect(request,root,os.getuid(),os.getgid())
            (runtime/'clipboard-generation.json').write_text(json.dumps(BINDING))
            (runtime/'n1-clipboard-diagnostic').mkdir()
            with self.assertRaises(m.Invalid): m.inspect(request,root,os.getuid(),os.getgid())
            (runtime/'n1-clipboard-diagnostic').rmdir()
            (lib/m.NAMES[1]).write_bytes(b'tampered')
            with self.assertRaises(m.Invalid): m.inspect(request,root,os.getuid(),os.getgid())
            (lib/m.NAMES[1]).write_bytes(artifacts[1]);os.link(lib/m.NAMES[1],lib/'foreign-hardlink')
            with self.assertRaises(m.Invalid): m.inspect(request,root,os.getuid(),os.getgid())

    def test_connect_receipt_binds_exact_control_ipv4(self):
        m=load('connect')
        class Socket:
            def settimeout(self,x): pass
            def connect(self,x): pass
            def close(self): pass
        result=m.connect({'binding':dict(BINDING),'control_ipv4':'192.168.64.2'},lambda *a:Socket(),clock=iter([1.,1.1]).__next__)
        self.assertEqual(result.get('control_ipv4'),'192.168.64.2')

    def test_connect_one_call_no_read_write_retry_and_fixed_errors(self):
        m=load('connect')
        class Socket:
            def __init__(self): self.calls=[]
            def settimeout(self,x): self.calls.append(('timeout',x))
            def connect(self,x): self.calls.append(('connect',x))
            def close(self): self.calls.append(('close',))
        sock=Socket(); factory_calls=[]
        def factory(*args): factory_calls.append(args); return sock
        request={'binding':dict(BINDING),'control_ipv4':'192.168.64.2'}
        result=m.connect(request,factory,clock=iter([10.,10.02]).__next__)
        self.assertEqual(result['code'],'connected')
        self.assertEqual(sock.calls,[('timeout',4.0),('connect',('192.168.64.2',22)),('close',)])
        self.assertEqual(factory_calls,[(m.socket.AF_INET,m.socket.SOCK_STREAM)])
        class Broken(Socket):
            def connect(self,x): self.calls.append(('connect',x)); raise OSError(113,'SECRET SENTINEL')
        bad=Broken(); result=m.connect(request,lambda *a:bad,clock=iter([10.,10.02]).__next__)
        self.assertEqual(result['code'],'socket_error'); self.assertEqual(result['errno'],113)
        self.assertNotIn('SECRET',json.dumps(result)); self.assertEqual(sum(c[0]=='connect' for c in bad.calls),1)

    def test_completed_connect_survives_close_and_clock_failures(self):
        m=load('connect')
        class Socket:
            def settimeout(self,x): pass
            def connect(self,x): pass
            def close(self): raise OSError('SECRET')
        result=m.connect({'binding':dict(BINDING),'control_ipv4':'192.168.64.2'},lambda *a:Socket(),clock=iter([10.,9.]).__next__)
        self.assertTrue(result['connected'])
        self.assertFalse(result['close_ok']); self.assertFalse(result['timing_ok'])
        self.assertEqual(result['code'],'connected')
        self.assertNotIn('SECRET',json.dumps(result))

    def test_connect_strict_binding_and_address_before_socket(self):
        m=load('connect')
        calls=[]
        for value in ('example.com','127.0.0.1','192.168.64.02','::1','8.8.8.8'):
            with self.assertRaises(m.Invalid): m.connect({'binding':dict(BINDING),'control_ipv4':value},lambda *a:calls.append(a))
        self.assertEqual(calls,[])
        for raw in (b'{"binding":NaN}',b'['*10000,b'{"x":'+b'9'*5000+b'}',b'{"x":1,"x":2}'):
            output=io.StringIO(); self.assertEqual(m.main([],io.BytesIO(raw),output),2)
            self.assertEqual(json.loads(output.getvalue())['code'],'invalid_binding')

    def test_controls_use_exact_fixed_dns_and_https_and_discard_response(self):
        m=load('controls'); calls=[]
        def run(argv): calls.append(argv); return 0
        result=m.controls({'binding':dict(BINDING)},run)
        self.assertEqual(result['dns'],'ok'); self.assertEqual(result['https'],'ok')
        failed=m.controls({'binding':dict(BINDING)},lambda argv: 1 if argv[0]=='/usr/bin/resolvectl' else 0)
        self.assertEqual(failed['dns'],'failed'); self.assertEqual(failed['https'],'ok')
        self.assertEqual(calls,[['/usr/bin/resolvectl','--cache=no','--synthesize=no','--zone=no','--protocol=dns','--type=A','query','example.com'],['/usr/bin/curl','--disable','--silent','--fail','--head','--proto','=https','--max-redirs','0','--connect-timeout','3','--max-time','5','--noproxy','*','https://example.com/']])

if __name__=='__main__': unittest.main()

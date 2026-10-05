import importlib.util
from pathlib import Path
import unittest
from unittest import mock
import contextlib
import hashlib
import io
import json
import os
import stat
import subprocess
import shutil
import tempfile
ROOT=Path(__file__).resolve().parent
class SupportResourcesTests(unittest.TestCase):
 def test_script_imports_leave_retained_source_unchanged(self):
  for script in (ROOT/'build_support.sh',ROOT.parent/'alpha-inspector/prepare_export_bundle.sh'):
   with self.subTest(script=script.name),tempfile.TemporaryDirectory() as temporary:
    source=Path(temporary)/'source';source.mkdir(mode=0o700)
    module=source/'support_resources.py';shutil.copyfile(ROOT/'support_resources.py',module)
    original=module.read_bytes()
    # Exercise each real script's initialization before its argument guard.
    # The following import is the same retained-source import used by both.
    header=script.read_text().split('if [[',1)[0]
    probe=header+"\npython3 - \"$1\" <<'PYPROBE'\nimport sys\nsys.path.insert(0,sys.argv[1])\nimport support_resources\nPYPROBE\n"
    env=dict(os.environ);env.pop('PYTHONDONTWRITEBYTECODE',None)
    subprocess.run(['/bin/bash','-c',probe,'fixture',str(source)],env=env,check=True)
    self.assertEqual(list(source.iterdir()),[module])
    self.assertEqual(module.read_bytes(),original)
 def test_binding_has_fixed_canonical_bytes_and_rejects_unsafe_root(self):
  spec=importlib.util.spec_from_file_location('support_resources',ROOT/'support_resources.py');module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module)
  self.assertEqual(module.binding_bytes('/Volumes/Data/project/state','work'),b'{"version":1,"state_root":"/Volumes/Data/project/state","domain":"work"}\n')
  self.assertIn(b'\\u003c',module.binding_bytes('/Volumes/Data/state <&> résumé','work'))
  self.assertIn('résumé'.encode(),module.binding_bytes('/Volumes/Data/state <&> résumé','work'))
  for root,domain in [('/Volumes/Data/../state','work'),('/state','../other'),('relative','work')]:
   with self.assertRaises(ValueError):module.binding_bytes(root,domain)
 def test_output_and_parent_are_synced_before_success_and_failure_is_retained(self):
  spec=importlib.util.spec_from_file_location('support_resources',ROOT/'support_resources.py');module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module)
  for fail_parent in (False,True):
   with self.subTest(fail_parent=fail_parent),tempfile.TemporaryDirectory() as temporary:
    root=Path(temporary);resources=root/'resources';(resources/'formatter').mkdir(parents=True)
    files={}
    for name in module.FORMATTER_NAMES:
     body=name.encode();(resources/'formatter'/name).write_bytes(body);files['formatter/'+name]=hashlib.sha256(body).hexdigest()
    config=root/'config.json';config.write_text(json.dumps({'domains':{'work':{'state_root':str(root/'state')}}}));config.chmod(0o600)
    m={'source_commit':'a'*40,'files':files,'runner_entitlements':{'com.apple.security.virtualization':True}}
    output=root/'formatter';seen=[];real_sync=os.fsync;parent_identity=root.stat().st_ino;captured=io.StringIO()
    def sync(fd):
     info=os.fstat(fd)
     if stat.S_ISDIR(info.st_mode):
      seen.append(info.st_ino)
      if fail_parent and info.st_ino==parent_identity:raise OSError('injected parent-directory sync failure')
     real_sync(fd)
    with mock.patch.object(module,'verify',return_value=m),mock.patch.object(module.os,'fsync',side_effect=sync),contextlib.redirect_stdout(captured):
     if fail_parent:
      with self.assertRaisesRegex(OSError,'injected parent-directory'):module.prepare(root,config,'work',resources,output)
     else:module.prepare(root,config,'work',resources,output)
    self.assertEqual(seen,[output.stat().st_ino,parent_identity])
    self.assertTrue((output/'manifest.json').is_file())
    self.assertEqual(bool(captured.getvalue()),not fail_parent)

if __name__=='__main__':unittest.main()

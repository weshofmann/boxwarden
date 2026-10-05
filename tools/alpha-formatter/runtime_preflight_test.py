#!/usr/bin/env python3
"""No-start verification of the signed MANAGED_RUNTIME admission contract."""
from contextlib import contextmanager
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
from managed_preflight_test import command, create_target

def main(binary,kernel,initrd,parent):
 with tempfile.TemporaryDirectory(prefix='runtime-binding-preflight.',dir=parent) as directory:
  work=Path(directory);root=work/'state';root.mkdir(mode=0o700)
  bundle=work/'bundle';bundle.mkdir(mode=0o700)
  binding=bundle/'managed-binding.json'
  doc={'version':1,'state_root':str(root),'domain':'work'}
  def publish(document=doc):
   binding.write_text(json.dumps(document,separators=(',',':'))+'\n');binding.chmod(0o600)
  publish();raw,marker=create_target(root,'work')
  argv=command(binary,'preflight-managed-bound',kernel,initrd,raw,marker)+[str(binding)]
  def expect_success():
   result=subprocess.run(argv,capture_output=True,text=True,timeout=30)
   if result.returncode or json.loads(result.stdout).get('validated') is not True:raise AssertionError(f'valid runtime binding rejected: {result.stderr}')
  def reject(label):
   result=subprocess.run(argv,capture_output=True,text=True,timeout=30)
   if result.returncode==0:raise AssertionError('unsafe runtime binding admitted: '+label)
  expect_success()
  # Mutate ACLs only on this disposable fixture, never on host ancestors.
  @contextmanager
  def acl(path,entry):
   subprocess.run(['/bin/chmod','+a',entry,str(path)],check=True)
   try:yield
   finally:subprocess.run(['/bin/chmod','-N',str(path)],check=True)
  with acl(work,'group:everyone deny delete'):
   expect_success()
  for target in (root,bundle,binding):
   with acl(target,'group:everyone deny delete'):
    reject('deny-delete ACL on strict private endpoint')
  for entry in ('group:everyone allow read','group:everyone deny delete,file_inherit'):
   with acl(work,entry):
    reject('grant or inherited ancestor ACL')
  with acl(work,'group:everyone deny delete'):
   subprocess.run(['/bin/chmod','+a','group:staff deny delete',str(work)],check=True)
   reject('multiple ancestor ACL entries')
  work.chmod(0o777);reject('writable ancestor');work.chmod(0o700)
  for change in [{'version':2},{'domain':'other'},{'state_root':str(work/'foreign')},{'extra':'field'}]:
   publish(dict(doc,**change));reject(str(change))
  publish();binding.write_text(binding.read_text().replace('"version":1','"version":1,"version":1'));reject('duplicate JSON')
  publish();binding.chmod(0o644);reject('public mode')
  publish();os.link(binding,bundle/'hardlink');reject('hardlink');(bundle/'hardlink').unlink()
  publish();binding.rename(bundle/'actual');binding.symlink_to(bundle/'actual');reject('symlink');binding.unlink();(bundle/'actual').rename(binding)
  publish();bundle.chmod(0o755);reject('non-private bundle');bundle.chmod(0o700)
  for mode in ('preflight','preflight-run','preflight-managed'):
   argv[1]=mode;reject('legacy or synthetic argv in reusable runner')
  argv[1]='preflight-managed-bound';expect_success()
 print('runtime binding preflight: exact record accepted; foreign root/domain, version, unknown/duplicate JSON, mode, hardlink, symlink, parent and legacy/synthetic argv rejected; no VM started')
if __name__=='__main__':
 if len(sys.argv)!=5:raise SystemExit('usage: runtime_preflight_test.py SIGNED_RUNNER KERNEL INITRD PRIVATE_FIXTURE_PARENT')
 main(*(Path(value) for value in sys.argv[1:]))

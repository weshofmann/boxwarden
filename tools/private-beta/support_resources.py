#!/usr/bin/env python3
"""Immutable support inventory and per-setup formatter binding publication.

The admitted Go caller validates the clean source and signed resource inventory
before invocation and checks the resulting bundle before saving a setup.
"""
import hashlib
import json
import os
from pathlib import Path
import re
import stat
import subprocess
import sys

ISO_SHA='c2610520bf582976839a1724c669e1cfed0547427be5a0ad12d457b92b46ffbe'
DEB_SHA='0a3d402fd7c7c07f63b8104d84a47378f57022e7c816190e6cc99950492d8cae'
CHECKER_SHA='e5e8f8b30641fab6e3f402c9746ce0537ad95c528e96cde2e446ca0968e63279'
KERNEL_SHA='a1586ff3cb7ced7c40dcb0aba5bf320ebb94a46d1a6505eb03157a8f9525632d'
BINDING_SOURCE=b'// Boxwarden managed runtime binding v1\n'
SELECTORS=('go.mod','internal','tools/alpha-formatter','tools/alpha-inspector','tools/private-beta/build_support.sh','tools/private-beta/prepare_support.sh','tools/private-beta/support_resources.py')
FORMATTER_NAMES=('kernel-image','formatter-initrd','alpha-formatter','e2fsck.static','alpha-formatter-host','binding.swift')
INSPECTOR_NAMES=('casper/vmlinuz','casper/initrd','kernel-image','alpha-probe','alpha-inspector')

def binding_bytes(root, domain):
 if not isinstance(root,str) or not root.startswith('/') or os.path.normpath(root)!=root or '//' in root or any(c in root for c in '\r\n\0') or re.fullmatch('[a-z][a-z0-9]{0,62}',domain) is None:
  raise ValueError('invalid runtime managed binding')
 raw=json.dumps({'version':1,'state_root':root,'domain':domain},separators=(',',':'),ensure_ascii=False)
 for character,escaped in [('<',r'\u003c'),('>',r'\u003e'),('&',r'\u0026'),('\u2028',r'\u2028'),('\u2029',r'\u2029')]:raw=raw.replace(character,escaped)
 return (raw+'\n').encode()

def digest(path):
 h=hashlib.sha256()
 with path.open('rb') as source:
  for chunk in iter(lambda:source.read(1024*1024),b''):h.update(chunk)
 return h.hexdigest()

def unique(pairs):
 result={}
 for key,value in pairs:
  if key in result:raise ValueError('duplicate support JSON key')
  result[key]=value
 return result

def private_file(path,maximum=256*1024):
 fd=os.open(path,os.O_RDONLY|os.O_NOFOLLOW|os.O_CLOEXEC)
 with os.fdopen(fd,'rb') as f:
  before=os.fstat(f.fileno())
  if not stat.S_ISREG(before.st_mode) or before.st_uid!=os.getuid() or before.st_nlink!=1 or stat.S_IMODE(before.st_mode)!=0o600 or not 0<before.st_size<=maximum:raise ValueError('support record is not bounded private data')
  raw=f.read(maximum+1);after=path.lstat()
  if (before.st_dev,before.st_ino,before.st_size,before.st_mode)!=(after.st_dev,after.st_ino,after.st_size,after.st_mode) or len(raw)!=before.st_size:raise ValueError('support record changed')
 return raw

def inventory(source):
 tracked=subprocess.check_output(['/usr/bin/git','-C',str(source),'ls-files','-z','--',*SELECTORS])
 return {os.fsdecode(name):digest(source/os.fsdecode(name)) for name in tracked.split(b'\0') if name}

def verify(source,resources):
 m=json.loads(private_file(resources/'manifest.json'),object_pairs_hook=unique)
 commit=subprocess.check_output(['/usr/bin/git','-C',str(source),'rev-parse','HEAD'],text=True).strip()
 if m['version']!=1 or m['source_commit']!=commit or m['iso_sha256']!=ISO_SHA or m['checker_deb_sha256']!=DEB_SHA or m['runner_entitlements']!={'com.apple.security.virtualization':True} or m['source_inputs']!=inventory(source):raise ValueError('prebuilt support source provenance differs')
 wanted={'formatter/'+n for n in FORMATTER_NAMES}|{'inspector/'+n for n in INSPECTOR_NAMES}
 if set(m['files'])!=wanted:raise ValueError('prebuilt support artifact inventory differs')
 for name,sha in m['files'].items():
  path=resources/name;info=path.lstat()
  if not stat.S_ISREG(info.st_mode) or info.st_nlink!=1 or info.st_uid!=os.getuid() or info.st_size<=0 or info.st_size>256*1024*1024 or digest(path)!=sha:raise ValueError('prebuilt artifact differs: '+name)
 if m['files']['formatter/kernel-image']!=KERNEL_SHA or m['files']['inspector/kernel-image']!=KERNEL_SHA or m['files']['formatter/e2fsck.static']!=CHECKER_SHA or (resources/'formatter/binding.swift').read_bytes()!=BINDING_SOURCE:raise ValueError('prebuilt fixed pins differ')
 return m

def write_private(path,raw):
 fd=os.open(path,os.O_WRONLY|os.O_CREAT|os.O_EXCL|os.O_NOFOLLOW,0o600)
 with os.fdopen(fd,'wb') as f:f.write(raw);f.flush();os.fsync(f.fileno())

def prepare(source,config,domain,resources,output):
 m=verify(source,resources)
 cfg=json.loads(private_file(config,1024*1024),object_pairs_hook=unique)
 binding=binding_bytes(cfg['domains'][domain]['state_root'],domain)
 output.mkdir(mode=0o700)
 for name in FORMATTER_NAMES:
  path=resources/'formatter'/name;raw=path.read_bytes()
  if hashlib.sha256(raw).hexdigest()!=m['files']['formatter/'+name]:raise ValueError('resource changed while copying')
  write_private(output/name,raw)
  if name in ('alpha-formatter','alpha-formatter-host'):(output/name).chmod(0o700)
 write_private(output/'managed-binding.json',binding)
 files={name:digest(output/name) for name in (*FORMATTER_NAMES,'managed-binding.json')}
 manifest={'version':2,'binding_mode':'runtime-v1','source_commit':m['source_commit'],'iso_sha256':ISO_SHA,'checker_deb_sha256':DEB_SHA,'managed_state_root':cfg['domains'][domain]['state_root'],'managed_domain':domain,'files':files,'runner_entitlements':m['runner_entitlements']}
 write_private(output/'manifest.json',(json.dumps(manifest,sort_keys=True)+'\n').encode())
 fd=os.open(output,os.O_RDONLY|os.O_DIRECTORY|os.O_NOFOLLOW)
 try:os.fsync(fd)
 finally:os.close(fd)
 # Settle the new directory entry before reporting a prepared bundle.
 # A failure retains the complete artifacts for honest inspection/recovery.
 parent_fd=os.open(output.parent,os.O_RDONLY|os.O_DIRECTORY|os.O_NOFOLLOW)
 try:os.fsync(parent_fd)
 finally:os.close(parent_fd)
 print('prepared packaged formatter artifacts: '+str(output))

if __name__=='__main__':
 if len(sys.argv)!=6:raise SystemExit('usage: support_resources.py SOURCE CONFIG DOMAIN RESOURCES NEW_OUTPUT')
 prepare(Path(sys.argv[1]),Path(sys.argv[2]),sys.argv[3],Path(sys.argv[4]),Path(sys.argv[5]))

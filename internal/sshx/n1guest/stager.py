"""Fixed clone-only staging. No path, source program or command selector."""
import hashlib, io, json, os, re, signal, stat, struct, sys
from pathlib import Path

NAMES = ('boxwarden-guest-bootstrap','boxwarden-n1-clipboard-diagnostic',
         'boxwarden-guest-clipboard.py','boxwarden-guest-clipboard.production.py',
         'boxwarden-n1-guest-metadata-observer.py','boxwarden-n1-stager.py',
         'boxwarden-n1-inspector.py','boxwarden-n1-controls.py','boxwarden-n1-tcp-probe.py')
OLD_HELPER='ecdb752197c6d36f9594d234e4035745f55e1e38c95426a78d473e5d26dadf06'
NEW_HELPER='33b12b9e293bcbdf7d6c91f933b42003ca394e7a678ac83b8d80df714d27c57e'
PRODUCTION='e38530c45a2aab705be80aad3e9096f2fce9330cb6c0b5b6623a3d76403b4dbb'
MAX_ARTIFACT=16<<20
MAX_TOTAL=40<<20
UUID=re.compile(r'[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}\Z')
class Invalid(ValueError): pass

def require(condition):
    if not condition: raise Invalid()

def pairs(items):
    result={}
    for key,value in items:
        require(key not in result); result[key]=value
    return result

def bad(_): raise Invalid()

def decode(raw,limit=4096):
    require(type(raw) is bytes and 0<len(raw)<=limit)
    try: return json.loads(raw.decode('utf-8','strict'),object_pairs_hook=pairs,parse_constant=bad)
    except (ValueError,RecursionError,UnicodeError): raise Invalid() from None

def exact(value,keys): require(type(value) is dict and set(value)==set(keys))

def binding(value):
    exact(value,('version','domain','session_id','backend_kind','backend_object','generation'))
    require(type(value['version']) is int and value['version']==1 and value['domain']=='n1qualification' and value['backend_kind']=='tart')
    for name in ('session_id','generation'): require(type(value[name]) is str and UUID.fullmatch(value[name]))
    require(value['session_id']!=value['generation'])
    require(type(value['backend_object']) is str and re.fullmatch(r'[A-Za-z0-9_-]{1,64}',value['backend_object']))
    return value

def descriptors(value):
    require(type(value) is list and len(value)==len(NAMES)); total=0
    for index,item in enumerate(value):
        exact(item,('name','length','sha256')); require(item['name']==NAMES[index])
        require(type(item['length']) is int and 0<item['length']<= (MAX_ARTIFACT if index<2 else 1<<20))
        require(type(item['sha256']) is str and re.fullmatch('[0-9a-f]{64}',item['sha256']))
        total+=item['length']
    require(total<=MAX_TOTAL)
    return value

def read_exact(stream,count):
    data=bytearray()
    while len(data)<count:
        part=stream.read(min(65536,count-len(data)))
        require(part); data.extend(part)
    return bytes(data)

def read_frame(stream):
    size=struct.unpack('!I',read_exact(stream,4))[0]; require(0<size<=65536)
    header=decode(read_exact(stream,size),65536); exact(header,('binding','artifacts'))
    binding(header['binding']); descriptors(header['artifacts']); values=[]
    for item in header['artifacts']:
        data=read_exact(stream,item['length']); require(hashlib.sha256(data).hexdigest()==item['sha256']); values.append(data)
    require(stream.read(1)==b'')
    return header,values

def no_acl(fd):
    require(not any(name in ('system.posix_acl_access','system.posix_acl_default') for name in os.listxattr(fd)))

def tuple_stat(s): return (s.st_dev,s.st_ino,s.st_mode,s.st_uid,s.st_gid,s.st_nlink,s.st_size,s.st_mtime_ns,s.st_ctime_ns)

def directory(path,uid,gid):
    # Every ancestor is opened without following links and bracketed by identity.
    path=Path(path); s=os.lstat(path)
    require(stat.S_ISDIR(s.st_mode) and s.st_uid==uid and s.st_gid==gid and not s.st_mode&0o022)
    fd=os.open(path,os.O_RDONLY|os.O_DIRECTORY|os.O_NOFOLLOW)
    try: require(tuple_stat(s)==tuple_stat(os.fstat(fd))); no_acl(fd)
    except BaseException: os.close(fd); raise
    return fd

def read_file(path,mode,uid,gid,limit=MAX_ARTIFACT):
    path=Path(path); s=os.lstat(path)
    fd=os.open(path,os.O_RDONLY|os.O_NOFOLLOW)
    try:
        require(stat.S_ISREG(s.st_mode) and s.st_uid==uid and s.st_gid==gid and stat.S_IMODE(s.st_mode)==mode and s.st_nlink==1 and 0<s.st_size<=limit)
        require(tuple_stat(s)==tuple_stat(os.fstat(fd))); no_acl(fd)
        with os.fdopen(fd,'rb',closefd=False) as stream: raw=stream.read(limit+1)
        require(len(raw)==s.st_size and tuple_stat(s)==tuple_stat(os.fstat(fd)) and tuple_stat(s)==tuple_stat(os.lstat(path)))
        return raw
    finally: os.close(fd)

def association(b,root,uid,gid,final=False):
    # Management binding predates generation support in the selected old helper.
    paths=('etc','etc/ssh','etc/ssh/boxwarden','etc/ssh/boxwarden/active')
    for relative in paths: os.close(directory(root/relative,uid,gid))
    value=decode(read_file(root/'etc/ssh/boxwarden/active/management-binding.json',0o600,uid,gid,4096))
    exact(value,('version','domain','session_id','backend_kind','backend_object','ca_fingerprint','principal'))
    require(all(value[k]==b[k] for k in ('version','domain','session_id','backend_kind','backend_object')))
    require(value['principal']=='boxwarden-session-'+b['session_id'] and type(value['ca_fingerprint']) is str and re.fullmatch(r'SHA256:[A-Za-z0-9+/]{43}',value['ca_fingerprint']))
    if final:
        for relative in ('run','run/boxwarden'): os.close(directory(root/relative,uid,gid))
        value=decode(read_file(root/'run/boxwarden/clipboard-generation.json',0o600,uid,gid,4096)); require(value==b)

def stage(header,values,root=Path('/'),uid=0,gid=0):
    exact(header,('binding','artifacts')); b=binding(header['binding']); ds=descriptors(header['artifacts'])
    require(len(values)==len(NAMES))
    for item,data in zip(ds,values): require(type(data) is bytes and len(data)==item['length'] and hashlib.sha256(data).hexdigest()==item['sha256'])
    require(ds[0]['sha256']==NEW_HELPER and ds[3]['sha256']==PRODUCTION)
    for relative in ('','usr','usr/local','usr/local/libexec'): os.close(directory(root/relative,uid,gid))
    association(b,root,uid,gid)
    lib=root/'usr/local/libexec'; dfd=directory(lib,uid,gid)
    def identity(): require((os.fstat(dfd).st_dev,os.fstat(dfd).st_ino)==(os.lstat(lib).st_dev,os.lstat(lib).st_ino))
    def absent(name):
        try: os.stat(name,dir_fd=dfd,follow_symlinks=False)
        except FileNotFoundError: return
        raise Invalid()
    try:
        require(hashlib.sha256(read_file(lib/NAMES[0],0o755,uid,gid)).hexdigest()==OLD_HELPER)
        for name in NAMES[1:]: absent(name)
        marker='.n1-stage-'+b['session_id']+'-'+b['generation']
        fd=os.open(marker,os.O_WRONLY|os.O_CREAT|os.O_EXCL|os.O_NOFOLLOW,0o600,dir_fd=dfd)
        os.fchmod(fd,0o600); os.fsync(fd); os.close(fd); os.fsync(dfd)
        staged=[]
        # A failure leaves immutable attempt evidence and staged residue; no retry.
        for index,(item,data) in enumerate(zip(ds,values)):
            identity(); temp=marker+'-'+str(index)+'.tmp'
            fd=os.open(temp,os.O_WRONLY|os.O_CREAT|os.O_EXCL|os.O_NOFOLLOW,0o600,dir_fd=dfd)
            try:
                with os.fdopen(fd,'wb',closefd=False) as stream: require(stream.write(data)==len(data)); stream.flush()
                os.fchmod(fd,0o755); os.fsync(fd)
            finally: os.close(fd)
            require(hashlib.sha256(read_file(lib/temp,0o755,uid,gid)).hexdigest()==item['sha256']); staged.append(temp)
        association(b,root,uid,gid)
        for index,(item,temp) in enumerate(zip(ds,staged)):
            identity()
            if index==0: require(hashlib.sha256(read_file(lib/NAMES[0],0o755,uid,gid)).hexdigest()==OLD_HELPER)
            else: absent(item['name'])
            require(hashlib.sha256(read_file(lib/temp,0o755,uid,gid)).hexdigest()==item['sha256'])
            os.replace(temp,item['name'],src_dir_fd=dfd,dst_dir_fd=dfd); os.fsync(dfd)
            require(hashlib.sha256(read_file(lib/item['name'],0o755,uid,gid)).hexdigest()==item['sha256'])
        identity()
    finally: os.close(dfd)
    return {'version':1,'binding':b,'code':'staged','installed':len(NAMES)}

def main(argv=None,stdin=None,stdout=None):
    argv=sys.argv[1:] if argv is None else argv; stdin=sys.stdin.buffer if stdin is None else stdin; stdout=sys.stdout if stdout is None else stdout
    result={'version':1,'code':'invalid_binding'}; code=2
    try:
        require(argv==[] and os.geteuid()==0); header,values=read_frame(stdin); result=stage(header,values); code=0
    except Exception: pass
    stdout.write(json.dumps(result,separators=(',',':'))+'\n'); stdout.flush(); return code
if __name__=='__main__':
    signal.signal(signal.SIGALRM,signal.SIG_DFL); signal.alarm(40); raise SystemExit(main())

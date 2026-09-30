//! Mandatory fixed-tree SH admission. No caller-controlled path, environment or fd.
use super::wire::VERSION;
use serde::Deserialize;
use std::ffi::{
    CString,CStr
};
use std::fs::File;
use std::io::{
    self,Read
};
use std::os::fd::{
    AsRawFd,FromRawFd
};
use std::os::unix::fs::MetadataExt;
use std::path::Path;
use std::time::{
    Duration,Instant
};
const PREFIX:&str="/Library/Boxwarden/toolchains/softnet";
const TART_EXE:&str="05b65d5c14e8b41e8e44b6d9fd1278de4bedbc8b735d9b99f3c748f76f75862d";
const TART_ARC:&str="8554ab4f7fc12afe52f9b7e3093a935673cbac737a83973d2db7a0683c814529";
fn reject()->io::Error {
    io::Error::other("diagnostic self-tree admission failed")
}
#[derive(Debug,Deserialize)]
#[serde(deny_unknown_fields)]
struct Tool {
    path:String,version:String,executable_sha256:String,archive_sha256:String
}
#[derive(Debug,Deserialize)]
#[serde(deny_unknown_fields)]
struct Group {
    id:u32,name:String,members:Vec<u32>
}
#[derive(Debug,Deserialize)]
#[serde(deny_unknown_fields)]
struct Operator {
    uid:u32,name:String,home:String
}
#[derive(Debug,Deserialize)]
#[serde(deny_unknown_fields)]
struct Manifest {
    version:u32,platform:String,macos:String,macos_build:String,tart:Tool,softnet:Tool,root_uid:u32,group:Group,operator:Operator,tart_home:String,softnet_mode:u32,installed_at:String
}
fn digest(s:&str)->bool {
    s.len()==64 && s.bytes().all(|b|b.is_ascii_digit()||(b'a'..=b'f').contains(&b))&&s.bytes().any(|b|b!=b'0')
}
fn absolute(s:&str)->bool {
    s.starts_with('/')&&s!="/"&&!s.ends_with('/')&&!s.chars().any(char::is_control)&&s[1..].split('/').all(|x|!matches!(x,""|"."|".."))
}
fn name(s:&str)->bool {
    (1..=255).contains(&s.len())&&s.bytes().all(|b|b.is_ascii_alphanumeric()||[b'.',b'_',b'-'].contains(&b))
}
fn platform(release:&str,build:&str)->bool {
    (release=="26.6.2"&&build=="25G83")||(release=="27.0.1"&&build=="26A434")
}
fn timestamp(s:&str)->bool {
    // Installation timestamps are Go RFC3339Nano. Reject zero/ambiguous/invalid dates.
    let bytes=s.as_bytes();
    if bytes.len()<20 || !bytes.is_ascii() {
        return false;
    }
    if bytes[4]!=b'-'||bytes[7]!=b'-'||bytes[10]!=b'T'||bytes[13]!=b':'||bytes[16]!=b':'||!(0..19).filter(|i|![4,7,10,13,16].contains(i)).all(|i|bytes[i].is_ascii_digit()) {
        return false;
    }
    let num=|r:std::ops::Range<usize>|s[r].parse::<u32>().ok();
    let (Some(y),Some(m),Some(d),Some(h),Some(mi),Some(se))=(num(0..4),num(5..7),num(8..10),num(11..13),num(14..16),num(17..19))else{
        return false;
    };
    let leap=y%4==0&&(y%100!=0||y%400==0);
    let days=match m{
        1|3|5|7|8|10|12=>31,4|6|9|11=>30,2=>if leap{
            29
        }else{
            28
        },_=>0
    };
    if y==0||d==0||d>days||h>23||mi>59||se>59{
        return false;
    }
    let mut rest=&s[19..];
    let mut nonzero_fraction=false;
    if let Some(f)=rest.strip_prefix('.') {
        let n=f.bytes().take_while(u8::is_ascii_digit).count();
        if !(1..=9).contains(&n){
            return false;
        }
        nonzero_fraction=f[..n].bytes().any(|b|b!=b'0');
        rest=&f[n..];
    }
    let first_day=y==1&&m==1&&d==1&&!nonzero_fraction;
    let local=(h*3600+mi*60+se)as i32;
    if rest=="Z" {
        return !(first_day&&local==0);
    }
    if rest.len()!=6||!matches!(rest.as_bytes()[0],b'+'|b'-')||rest.as_bytes()[3]!=b':'||![1,2,4,5].iter().all(|&i|rest.as_bytes()[i].is_ascii_digit()) {
        return false;
    }
    let (Ok(oh),Ok(om))=(rest[1..3].parse::<i32>(),rest[4..].parse::<i32>())else{
        return false;
    };
    if oh>23||om>59{
        return false;
    }
    let offset=(oh*3600+om*60)*if rest.as_bytes()[0]==b'+'{
        1
    }else{
        -1
    };
    !(first_day&&local==offset)
}
impl Manifest {
    fn valid(&self,path:&str,hash:&str,uid:u32,release:&str,build:&str)->bool {
        self.version==2&&self.platform=="darwin"&&platform(&self.macos,&self.macos_build)&&platform(release,build)
        &&((self.macos==release&&self.macos_build==build)||(release=="27.0.1"&&build=="26A434"&&self.macos=="26.6.2"&&self.macos_build=="25G83"))
        &&self.tart.version=="2.32.1"&&self.tart.executable_sha256==TART_EXE&&self.tart.archive_sha256==TART_ARC&&absolute(&self.tart.path)
        &&self.softnet.path==path&&self.softnet.version==VERSION&&self.softnet.executable_sha256==hash&&digest(&self.softnet.archive_sha256)
        &&self.root_uid==0&&self.softnet_mode==0o4550&&self.group.name=="boxwarden-operators"&&self.group.members==[uid]
        &&self.operator.uid==uid&&uid>0&&name(&self.operator.name)&&absolute(&self.operator.home)&&absolute(&self.tart_home)&&timestamp(&self.installed_at)
    }
}
#[derive(Clone,Copy,Debug,PartialEq,Eq)]
struct Identity {
    dev:u64,ino:u64,uid:u32,gid:u32,mode:u32,links:u64,size:u64,mtime:i64,mn:i64,ctime:i64,cn:i64
}
fn identity(f:&File)->io::Result<Identity> {
    let m=f.metadata()?;
    Ok(Identity{
        dev:m.dev(),ino:m.ino(),uid:m.uid(),gid:m.gid(),mode:m.mode(),links:m.nlink(),size:m.size(),mtime:m.mtime(),mn:m.mtime_nsec(),ctime:m.ctime(),cn:m.ctime_nsec()
    })
}
fn metadata_valid(i:Identity,mode:u32,gid:u32,zero:bool,acl:bool)->bool {
    i.uid==0&&i.gid==gid&&i.mode&((libc::S_IFMT as u32)|0o7777)==((libc::S_IFREG as u32)|mode)&&i.links==1&&(!zero||i.size==0)&&!acl
}
fn tart_metadata_valid(i:Identity,acl:bool)->bool {
    i.mode&(libc::S_IFMT as u32)==(libc::S_IFREG as u32)&&i.mode&0o6000==0&&i.mode&0o111!=0&&i.links==1&&!acl
}
fn meta(f:&File,mode:u32,gid:u32,zero:bool)->io::Result<Identity>{
    let i=identity(f)?;
    if !metadata_valid(i,mode,gid,zero,has_acl(f)?){
        return Err(reject());
    }Ok(i)
}
fn open_at(dir:&File,n:&str,directory:bool)->io::Result<File>{
    let n=CString::new(n).map_err(|_|reject())?;
    let fd=unsafe{
        libc::openat(dir.as_raw_fd(),n.as_ptr(),libc::O_RDONLY|libc::O_NOFOLLOW|libc::O_CLOEXEC|libc::O_NONBLOCK|if directory{
            libc::O_DIRECTORY
        }else{
            0
        })
    };
    if fd<0{
        Err(reject())
    }else{
        Ok(unsafe{
            File::from_raw_fd(fd)
        })
    }
}
fn protected_dir(path:&str)->io::Result<File>{
    let fd=unsafe{
        libc::open(c"/".as_ptr(),libc::O_RDONLY|libc::O_DIRECTORY|libc::O_CLOEXEC|libc::O_NOFOLLOW)
    };
    if fd<0{
        return Err(reject());
    }
    let mut dir=unsafe{
        File::from_raw_fd(fd)
    };
    let check=|f:&File|->io::Result<()>{
        let i=identity(f)?;
        if i.uid!=0||i.gid!=0||i.mode&(libc::S_IFMT as u32)!=(libc::S_IFDIR as u32)||i.mode&0o7022!=0||has_acl(f)?{
            return Err(reject());
        }Ok(())
    };
    check(&dir)?;
    for n in path.strip_prefix('/').ok_or_else(reject)?.split('/') {
        if n.is_empty()||n=="."||n==".."{
            return Err(reject());
        }dir=open_at(&dir,n,true)?;
        check(&dir)?;
    }
    Ok(dir)
}
fn nofollow_dir(path:&str)->io::Result<File>{
    let fd=unsafe{
        libc::open(c"/".as_ptr(),libc::O_RDONLY|libc::O_DIRECTORY|libc::O_CLOEXEC|libc::O_NOFOLLOW)
    };
    if fd<0{
        return Err(reject());
    }
    let mut dir=unsafe{
        File::from_raw_fd(fd)
    };
    for n in path.strip_prefix('/').ok_or_else(reject)?.split('/') {
        if n.is_empty()||n=="."||n==".."{
            return Err(reject());
        }dir=open_at(&dir,n,true)?;
    }
    Ok(dir)
}
fn hash_file(f:&mut File)->io::Result<String>{
    let mut h=ring::digest::Context::new(&ring::digest::SHA256);
    let mut buf=[0;     65536];
    let mut total=0usize;
    loop{
        let n=f.read(&mut buf)?;
        if n==0{
            break;
        }total+=n;
        if total>128*1024*1024{
            return Err(reject());
        }h.update(&buf[..n]);
    }Ok(h.finish().as_ref().iter().map(|b|format!("{b:02x}")).collect())
}
#[cfg(target_os="macos")]
unsafe extern "C" {
    fn acl_get_fd_np(fd:libc::c_int,typ:libc::c_int)->*mut libc::c_void;
    fn acl_get_entry(acl:*mut libc::c_void,entry_id:libc::c_int,entry:*mut *mut libc::c_void)->libc::c_int;
    fn acl_free(obj:*mut libc::c_void)->libc::c_int;
}
#[cfg(target_os="macos")]
fn has_acl(f:&File)->io::Result<bool>{
    unsafe{
        let acl=acl_get_fd_np(f.as_raw_fd(),0x100);
        if acl.is_null(){
            // ENOENT is Darwin's no extended ACL representation.
            if io::Error::last_os_error().raw_os_error()==Some(libc::ENOENT){
                return Ok(false);
            }return Err(reject());
        }
        let mut entry=std::ptr::null_mut();
        let rc=acl_get_entry(acl,0,&mut entry);
        let error=io::Error::last_os_error().raw_os_error().unwrap_or(0);
        let freed=acl_free(acl);
        acl_entry_result(rc,error,freed)
    }
}
// Darwin differs from Linux: success returns zero. This helper is called ONLY
// after a successful ACL getter/allocator and fixed ACL_FIRST_ENTRY on that valid
// object. Under those preconditions -1/EINVAL is the zero-entry case. Other
// getter/free/check failures refuse. Capture errno before acl_free.
// Apple Libc posix1e/acl_entry.c:108-136 and Apple's acl_get_entry(3).
fn acl_entry_result(rc:i32,error:i32,freed:i32)->io::Result<bool>{
    if freed!=0{
        return Err(reject());
    }
    if rc==0{
        return Ok(true);
    }
    if rc==-1&&error==libc::EINVAL{
        return Ok(false);
    }
    Err(reject())
}
#[cfg(not(target_os="macos"))]
fn has_acl(_:&File)->io::Result<bool>{
    Err(reject())
}
#[cfg(target_os="macos")]
#[allow(deprecated)] // Exact libSystem declaration; no new unpinned dependency.
fn image_path()->io::Result<String>{
    let mut b=[0u8;     4096];
    let n=unsafe{
        libc::proc_pidpath(libc::getpid(),b.as_mut_ptr().cast(),b.len()as u32)
    };
    if n<=0||n as usize>=b.len(){
        return Err(reject());
    }
    let p=CStr::from_bytes_until_nul(&b).map_err(|_|reject())?.to_str().map_err(|_|reject())?.to_owned();
    let mut dyld=[0u8;     4096];
    let mut len=dyld.len()as u32;
    if unsafe{
        libc::_NSGetExecutablePath(dyld.as_mut_ptr().cast(),&mut len)
    }!=0{
        return Err(reject());
    }
    let q=CStr::from_bytes_until_nul(&dyld).map_err(|_|reject())?.to_str().map_err(|_|reject())?;
    if p!=q||!absolute(&p){
        return Err(reject());
    }Ok(p)
}
#[cfg(not(target_os="macos"))]
fn image_path()->io::Result<String>{
    Err(reject())
}
#[cfg(target_os="macos")]
fn sysctl(name:&CStr)->io::Result<String>{
    let mut b=[0u8;     128];
    let mut n=b.len();
    if unsafe{
        libc::sysctlbyname(name.as_ptr(),b.as_mut_ptr().cast(),&mut n,std::ptr::null_mut(),0)
    }!=0||n==0||n>b.len(){
        return Err(reject());
    }CStr::from_bytes_with_nul(&b[..n]).map_err(|_|reject())?.to_str().map(str::to_owned).map_err(|_|reject())
}
#[cfg(not(target_os="macos"))]
fn sysctl(_:&CStr)->io::Result<String>{
    Err(reject())
}
/// Body-owned resources (including construction failures and unwinding) finish
/// before the guard is released. Proxy additionally borrows the real guard.
pub fn held_scope<G,R>(guard:G,body:impl FnOnce(&G)->R)->R {
    let result=body(&guard);
    drop(guard);
    result
}
/// Owns the admitted inode until after actual Proxy/Host/Interface teardown.
/// Local construction precedes Proxy; explicit `drop(proxy)` precedes `drop(guard)`.
pub struct LaunchGuard {
    lock:File,dir:File,dir_path:String,dir_id:Identity,lock_id:Identity, image:File,image_id:Identity,manifest:File,manifest_id:Identity
}
impl LaunchGuard {
    pub fn acquire()->io::Result<Self>{
        let path=image_path()?;
        let dirpath=Path::new(&path).parent().and_then(Path::to_str).ok_or_else(reject)?;
        let hash=Path::new(dirpath).file_name().and_then(|p|p.to_str()).ok_or_else(reject)?;
        if !digest(hash)||path!=format!("{PREFIX}/{VERSION}/{hash}/softnet"){
            return Err(reject());
        }
        let dir=protected_dir(dirpath)?;
        let mut manifest=open_at(&dir,"manifest.json",false)?;
        let manifest_id=meta(&manifest,0o444,0,false)?;
        if manifest_id.size==0||manifest_id.size>65536{
            return Err(reject());
        }let mut data=Vec::new();
        (&mut manifest).take(65537).read_to_end(&mut data)?;
        if data.len()>65536{
            return Err(reject());
        }
        let mut decoder=serde_json::Deserializer::from_slice(&data);
        let m=Manifest::deserialize(&mut decoder).map_err(|_|reject())?;
        decoder.end().map_err(|_|reject())?;
        let uid=unsafe{
            libc::getuid()
        };
        let release=sysctl(c"kern.osproductversion")?;
        let build=sysctl(c"kern.osversion")?;
        if unsafe{
            libc::geteuid()
        }!=0||!m.valid(&path,hash,uid,&release,&build){
            return Err(reject());
        }
        current_operator(&m)?;
        let mut image=open_at(&dir,"softnet",false)?;
        let image_id=meta(&image,0o4550,m.group.id,false)?;
        if hash_file(&mut image)?!=hash||identity(&image)?!=image_id||identity(&manifest)?!=manifest_id{
            return Err(reject());
        }
        // Verify the immutable paired Tart without executing it or trusting a path string alone.
        let tdir=nofollow_dir(Path::new(&m.tart.path).parent().and_then(Path::to_str).ok_or_else(reject)?)?;
        let mut tart=open_at(&tdir,Path::new(&m.tart.path).file_name().and_then(|p|p.to_str()).ok_or_else(reject)?,false)?;
        let ti=identity(&tart)?;
        if !tart_metadata_valid(ti,has_acl(&tart)?)||hash_file(&mut tart)?!=TART_EXE||identity(&tart)?!=ti{
            return Err(reject());
        }
        let lock=open_at(&dir,"launch.lock",false)?;
        let lock_id=meta(&lock,0o440,m.group.id,true)?;
        if unsafe{
            libc::flock(lock.as_raw_fd(),libc::LOCK_SH|libc::LOCK_NB)
        }!=0{
            return Err(reject());
        }
        let dir_id=identity(&dir)?;
        let g=Self{
            lock,dir,dir_path:dirpath.into(),dir_id,lock_id,image,image_id,manifest,manifest_id
        };
        g.revalidate()?;
        Ok(g)
    }
    pub fn revalidate(&self)->io::Result<()> {
        if identity(&self.dir)?!=self.dir_id||identity(&protected_dir(&self.dir_path)?)?!=self.dir_id||image_path()?!=format!("{}/softnet",self.dir_path){
            return Err(reject());
        }
        let entries=std::fs::read_dir(&self.dir_path)?.take(4).collect::<Result<Vec<_>,_>>()?;
        if entries.len()!=3||entries.iter().any(|e|!matches!(e.file_name().to_str(),Some("softnet"|"manifest.json"|"launch.lock"))){
            return Err(reject());
        }
        for (n,f,id) in [("launch.lock",&self.lock,self.lock_id),("softnet",&self.image,self.image_id),("manifest.json",&self.manifest,self.manifest_id)]{
            if identity(f)?!=id||identity(&open_at(&self.dir,n,false)?)?!=id||has_acl(f)?{
                return Err(reject());
            }
        }
        Ok(())
    }
}
/// Bounded fixed read-only DirectoryService calls; no shell, ambient env or arbitrary output.
fn directory(args:&[&str])->io::Result<String>{
    use std::process::{
        Command,Stdio
    };
    let mut child=Command::new("/usr/bin/dscl").args(args).env_clear().env("LC_ALL","C").env("LANG","C").stdin(Stdio::null()).stdout(Stdio::piped()).stderr(Stdio::null()).spawn()?;
    let mut out=child.stdout.take().ok_or_else(reject)?;
    let flags=unsafe{
        libc::fcntl(out.as_raw_fd(),libc::F_GETFL)
    };
    if flags<0||unsafe{
        libc::fcntl(out.as_raw_fd(),libc::F_SETFL,flags|libc::O_NONBLOCK)
    }<0{
        let _=child.kill();
        let _=child.wait();
        return Err(reject());
    }
    let started=Instant::now();
    let mut bytes=Vec::new();
    let mut buf=[0;     4096];
    let mut eof=false;
    let mut status=None;
    loop {
        match out.read(&mut buf){
            Ok(0)=>eof=true,Ok(n)=>{
                bytes.extend_from_slice(&buf[..n]);
                if bytes.len()>16384{
                    let _=child.kill();
                    let _=child.wait();
                    return Err(reject());
                }
            },Err(e)if e.kind()==io::ErrorKind::WouldBlock=>{
            },Err(_)=>{
                let _=child.kill();
                let _=child.wait();
                return Err(reject());
            }
        }
        if status.is_none() {
            match child.try_wait(){
                Ok(s)=>status=s,Err(_)=>{
                    let _=child.kill();
                    let _=child.wait();
                    return Err(reject());
                }
            }
        }
        if let Some(s)=status {
            if !s.success(){
                return Err(reject());
            }if eof{
                return String::from_utf8(bytes).map_err(|_|reject());
            }
        }
        if started.elapsed()>Duration::from_secs(5){
            let _=child.kill();
            let _=child.wait();
            return Err(reject());
        }
        std::thread::sleep(Duration::from_millis(1));
    }
}
fn attrs(output:&str)->io::Result<std::collections::BTreeMap<String,Vec<String>>>{
    let mut map:std::collections::BTreeMap<String,Vec<String>>=std::collections::BTreeMap::new();
    let mut current=String::new();
    for line in output.lines(){
        if line.trim().is_empty(){
            continue;
        }if line.starts_with(char::is_whitespace){
            if current.is_empty(){
                return Err(reject());
            }map.get_mut(&current).ok_or_else(reject)?.extend(line.split_whitespace().map(str::to_owned));
        }else{
            let(k,v)=line.split_once(':').ok_or_else(reject)?;
            if k.is_empty()||k.chars().any(char::is_whitespace)||map.contains_key(k){
                return Err(reject());
            }current=k.into();
            map.insert(current.clone(),v.split_whitespace().map(str::to_owned).collect::<Vec<_>>());
        }
    }
    Ok(map)
}
fn exact_directory_state(m:&Manifest,user_output:&str,group_output:&str,users_output:&str,groups_output:&str)->io::Result<()> {
    let uid=m.operator.uid;
    let u=attrs(user_output)?;
    let single=|a:&std::collections::BTreeMap<String,Vec<String>>,k:&str|->io::Result<String>{
        let v=a.get(k).ok_or_else(reject)?;
        if v.len()!=1{
            return Err(reject());
        }Ok(v[0].clone())
    };
    if single(&u,"UniqueID")?!=uid.to_string()||!u.get("RecordName").is_some_and(|v|v.contains(&m.operator.name)&&v.iter().collect::<std::collections::BTreeSet<_>>().len()==v.len()){
        return Err(reject());
    }
    let guid=single(&u,"GeneratedUID")?;
    if guid.is_empty(){
        return Err(reject());
    }
    let g=attrs(group_output)?;
    if single(&g,"RecordName")?!=m.group.name||single(&g,"PrimaryGroupID")?!=m.group.id.to_string()||g.get("NestedGroups").is_some_and(|v|!v.is_empty())||g.get("GroupMembership")!=Some(&vec![m.operator.name.clone()])||g.get("GroupMembers")!=Some(&vec![guid]){
        return Err(reject());
    }
    for (kind,required,output) in [("/Users",m.operator.name.as_str(),users_output),("/Groups",m.group.name.as_str(),groups_output)]{
        let mut seen=std::collections::BTreeSet::new();
        let mut found=false;
        for line in output.lines(){
            let f=line.split_whitespace().collect::<Vec<_>>();
            if f.is_empty(){
                continue;
            }if f.len()!=2||!name(f[0])||!seen.insert(f[0]){
                return Err(reject());
            }let gid=f[1].parse::<u32>().map_err(|_|reject())?;
            if f[0]==required{
                found=true;
                if kind=="/Groups"&&gid!=m.group.id{
                    return Err(reject());
                }
            }
            if gid==m.group.id&&f[0]!=required{
                return Err(reject());
            }
        }if !found{
            return Err(reject());
        }
    }
    Ok(())
}
fn current_operator(m:&Manifest)->io::Result<()> {
    let uid=m.operator.uid;
    let user=format!("/Users/{}",m.operator.name);
    exact_directory_state(m,
    &directory(&["/Local/Default","-read",&user,"RecordName","UniqueID","GeneratedUID"] )?,
    &directory(&["/Local/Default","-read","/Groups/boxwarden-operators"] )?,
    &directory(&["/Search","-list","/Users","PrimaryGroupID"] )?,
    &directory(&["/Search","-list","/Groups","PrimaryGroupID"] )?)?;
    let pw=unsafe{
        libc::getpwuid(uid)
    };
    if pw.is_null(){
        return Err(reject());
    }unsafe{
        if CStr::from_ptr((*pw).pw_name).to_str().map_err(|_|reject())?!=m.operator.name||CStr::from_ptr((*pw).pw_dir).to_str().map_err(|_|reject())?!=m.operator.home{
            return Err(reject());
        }
    }
    let mut groups=[0 as libc::gid_t;     256];
    let n=unsafe{
        libc::getgroups(groups.len()as i32,groups.as_mut_ptr())
    };
    if n<=0||n as usize>groups.len()||!groups[..n as usize].contains(&m.group.id){
        return Err(reject());
    }
    let home=nofollow_dir(&m.tart_home)?;
    let metadata=home.metadata()?;
    if !metadata.is_dir()||metadata.uid()!=uid||metadata.mode()&0o077!=0||has_acl(&home)?{
        return Err(reject());
    }
    Ok(())
}
#[cfg(test)]
mod tests {
    use super::*;
    fn manifest()->Manifest {
        let hash="a".repeat(64);
        Manifest{
            version:2,platform:"darwin".into(),macos:"27.0.1".into(),macos_build:"26A434".into(),tart:Tool{
                path:"/Users/operator/Library/ApplicationSupport/Boxwarden/tart".into(),version:"2.32.1".into(),executable_sha256:TART_EXE.into(),archive_sha256:TART_ARC.into()
            },softnet:Tool{
                path:format!("{PREFIX}/{VERSION}/{hash}/softnet"),version:VERSION.into(),executable_sha256:hash,archive_sha256:"b".repeat(64)
            },root_uid:0,group:Group{
                id:1000,name:"boxwarden-operators".into(),members:vec![501]
            },operator:Operator{
                uid:501,name:"operator".into(),home:"/Users/operator".into()
            },tart_home:"/Volumes/Private/tart".into(),softnet_mode:0o4550,installed_at:"2026-09-30T12:00:00.123456789Z".into()
        }
    }
    #[test]
    fn complete_manifest_platform_path_operator_pair_and_archive_syntax(){
        let mut m=manifest();
        assert!(m.valid(&m.softnet.path,&m.softnet.executable_sha256,501,"27.0.1","26A434"));
        m.macos="26.6.2".into();
        m.macos_build="25G83".into();
        assert!(m.valid(&m.softnet.path,&m.softnet.executable_sha256,501,"27.0.1","26A434"));
        assert!(!m.valid(&m.softnet.path,&m.softnet.executable_sha256,501,"26.6.2","26A434"));
        m.softnet.archive_sha256="0".repeat(64);
        assert!(!m.valid(&m.softnet.path,&m.softnet.executable_sha256,501,"27.0.1","26A434"));
        for path in ["/stage/softnet","/Library/Boxwarden/toolchains/softnet/other/softnet"]{
            assert!(!manifest().valid(path,&"a".repeat(64),501,"27.0.1","26A434"));
        }m=manifest();
        m.group.members.push(502);
        assert!(!m.valid(&m.softnet.path,&m.softnet.executable_sha256,501,"27.0.1","26A434"));
    }
    #[test]
    fn duplicate_unknown_missing_manifest_and_date_rejection(){
        let text=r#"{"version":2,"version":2}"#;
        assert!(serde_json::from_str::<Manifest>(text).is_err());
        assert!(serde_json::from_str::<Manifest>(r#"{"unknown":1}"#).is_err());
        for s in ["0000-01-01T00:00:00Z","0001-01-01T00:00:00+00:00","0001-01-01T01:00:00+01:00","0001-01-01T00:00:00.000-00:00","2026-02-30T00:00:00Z","2026-01-01T25:00:00Z","2026-01-01T00:00:00.1234567890Z"]{
            assert!(!timestamp(s));
        }assert!(timestamp("2024-02-29T00:00:00-06:00"));
        assert!(timestamp("0001-01-01T00:00:00.000000001+00:00"));
    }
    fn r1_offset_assertions(bad:&[&str]) {
        for offset in bad {
            let date=format!("2026-09-30T12:00:00{offset}");
            assert!(!timestamp(&date),"malformed offset admitted: {offset}");
            let mut m=manifest();
            m.installed_at=date;
            assert!(!m.valid(&m.softnet.path,&m.softnet.executable_sha256,501,"27.0.1","26A434"));
        }
        for date in ["2026-09-30T12:00:00+01:30","2026-09-30T12:00:00-06:45","2026-09-30T12:00:00+23:59","2026-09-30T12:00:00-00:00","0001-01-01T00:00:00.000000001+00:00"] {
            assert!(timestamp(date));
            let mut m=manifest();
            m.installed_at=date.into();
            assert!(m.valid(&m.softnet.path,&m.softnet.executable_sha256,501,"27.0.1","26A434"));
        }
        for date in ["0001-01-01T00:00:00+00:00","0001-01-01T01:00:00+01:00","0001-01-01T00:00:00.000-00:00"] {
            assert!(!timestamp(date));
        }
    }
    #[test]
    fn r1_offset_hour_components_require_decimal_digits() {
        r1_offset_assertions(&["+-1:00","++1:00","--1:00","-+1:00","+24:00","-24:00","+a1:00"]);
    }
    #[test]
    fn r1_offset_minute_components_require_decimal_digits() {
        r1_offset_assertions(&["+00:-1","+00:+1","-00:-1","-00:+1","+00:60","-00:60","+00:a1"]);
    }
    #[test]
    fn ordinary_fixture_shared_exclusive_lock_and_inode_replacement(){
        let root=std::env::temp_dir().join(format!("n1-lock-fixture-{}-{:?}",std::process::id(),std::thread::current().id()));
        std::fs::create_dir(&root).unwrap();
        let p=root.join("launch.lock");
        let a=std::fs::OpenOptions::new().create_new(true).read(true).write(true).open(&p).unwrap();
        let b=File::open(&p).unwrap();
        assert_eq!(unsafe{
            libc::flock(a.as_raw_fd(),libc::LOCK_SH|libc::LOCK_NB)
        },0);
        assert_eq!(unsafe{
            libc::flock(b.as_raw_fd(),libc::LOCK_EX|libc::LOCK_NB)
        },-1);
        let before=identity(&a).unwrap();
        drop(a);
        assert_eq!(unsafe{
            libc::flock(b.as_raw_fd(),libc::LOCK_EX|libc::LOCK_NB)
        },0);
        let c=File::open(&p).unwrap();
        assert_eq!(unsafe{
            libc::flock(c.as_raw_fd(),libc::LOCK_SH|libc::LOCK_NB)
        },-1);
        std::fs::remove_file(&p).unwrap();
        assert!(File::open(&p).is_err());
        let new=std::fs::OpenOptions::new().create_new(true).write(true).open(&p).unwrap();
        assert_ne!(before.ino,identity(&new).unwrap().ino);
        drop(new);
        drop(c);
        drop(b);
        std::fs::remove_file(&p).unwrap();
        std::fs::remove_dir(root).unwrap();
    }
    #[test]
    fn protected_metadata_rejects_mode_owner_gid_acl_links_and_content(){
        let i=Identity{
            dev:1,ino:2,uid:0,gid:1000,mode:(libc::S_IFREG as u32)|0o440,links:1,size:0,mtime:0,mn:0,ctime:0,cn:0
        };
        assert!(metadata_valid(i,0o440,1000,true,false));
        for n in 0..6{
            let mut bad=i;
            let mut acl=false;
            match n{
                0=>bad.uid=501,1=>bad.gid=1,2=>bad.mode|=2,3=>bad.links=2,4=>bad.size=1,_=>acl=true
            };
            assert!(!metadata_valid(bad,0o440,1000,true,acl));
        }
    }
    #[test]
    fn tart_operator_owned_direct_fixture_is_admitted_without_root_rule(){
        let i=Identity{
            dev:1,ino:2,uid:501,gid:20,mode:(libc::S_IFREG as u32)|0o755,links:1,size:1,mtime:0,mn:0,ctime:0,cn:0
        };
        assert!(tart_metadata_valid(i,false));
        let mut bad=i;
        bad.mode|=0o4000;
        assert!(!tart_metadata_valid(bad,false));
        assert!(!tart_metadata_valid(i,true));
    }
    #[test]
    fn darwin_acl_return_zero_means_present_and_empty_einval_is_absent(){
        assert!(acl_entry_result(0,0,0).unwrap());
        assert!(!acl_entry_result(-1,libc::EINVAL,0).unwrap());
        assert!(acl_entry_result(-1,libc::EIO,0).is_err());
        assert!(acl_entry_result(0,0,-1).is_err());
    }
    #[cfg(target_os="macos")]
    #[test]
    fn native_in_memory_acl_and_ordinary_noacl_descriptor_controls(){
        unsafe extern "C" {
            fn acl_init(count:libc::c_int)->*mut libc::c_void;
            fn acl_create_entry(acl:*mut *mut libc::c_void,entry:*mut *mut libc::c_void)->libc::c_int;
        }
        unsafe{
            let mut acl=acl_init(1);
            assert!(!acl.is_null());
            let mut entry=std::ptr::null_mut();
            let rc=acl_get_entry(acl,0,&mut entry);
            let e=io::Error::last_os_error().raw_os_error().unwrap_or(0);
            assert!(!acl_entry_result(rc,e,0).unwrap());
            assert_eq!(acl_create_entry(&mut acl,&mut entry),0);
            assert_eq!(acl_get_entry(acl,0,&mut entry),0);
            assert!(acl_entry_result(0,0,0).unwrap());
            assert_eq!(acl_free(acl),0);
        }
        let path=std::env::temp_dir().join(format!("n1-acl-read-control-{}",std::process::id()));
        let file=std::fs::OpenOptions::new().create_new(true).read(true).write(true).open(&path).unwrap();
        assert!(!has_acl(&file).unwrap());
        unsafe extern "C" {
            fn acl_set_tag_type(entry:*mut libc::c_void,tag:libc::c_int)->libc::c_int;
            fn acl_set_qualifier(entry:*mut libc::c_void,q:*const libc::c_void)->libc::c_int;
            fn acl_set_fd(fd:libc::c_int,acl:*mut libc::c_void)->libc::c_int;
        }
        unsafe {
            let mut acl=acl_init(1);
            assert!(!acl.is_null());
            let mut entry=std::ptr::null_mut();
            assert_eq!(acl_create_entry(&mut acl,&mut entry),0);
            assert_eq!(acl_set_tag_type(entry,1),0);
            let id=[1u8;             16];
            assert_eq!(acl_set_qualifier(entry,id.as_ptr().cast()),0);
            assert_eq!(acl_set_fd(file.as_raw_fd(),acl),0);
            assert_eq!(acl_free(acl),0);
        }
        assert!(has_acl(&file).unwrap());
        drop(file);
        std::fs::remove_file(path).unwrap();
    }
    #[test]
    fn held_scope_keeps_guard_through_partial_full_error_and_unwind_resource_drop(){
        use std::rc::Rc;
        use std::cell::RefCell;
        struct G(Rc<RefCell<Vec<&'static str>>>);
        impl Drop for G{
            fn drop(&mut self){
                self.0.borrow_mut().push("guard");
            }
        }
        struct Resource(Rc<RefCell<Vec<&'static str>>>);
        impl Drop for Resource{
            fn drop(&mut self){
                assert!(!self.0.borrow().contains(&"guard"));
                self.0.borrow_mut().push("resource");
            }
        }
        for phase in 0..4 {
            let log=Rc::new(RefCell::new(Vec::new()));
            let outcome=std::panic::catch_unwind(std::panic::AssertUnwindSafe(||held_scope(G(log.clone()),|guard|{
                let _partial=Resource(guard.0.clone());if phase==0{
                    return Err(());
                }let _full=Resource(guard.0.clone());if phase==1{
                    return Err(());
                }if phase==2{
                    panic!("synthetic unwind");
                }Ok(())
            })));
            if phase==2{
                assert!(outcome.is_err());
            }else{
                assert_eq!(outcome.unwrap().is_ok(),phase==3);
            }let values=log.borrow();
            assert_eq!(values.last(),Some(&"guard"));
            assert_eq!(values.iter().filter(|s|**s=="resource").count(),if phase==0{
                1
            }else{
                2
            });
        }
    }
    #[test]
    fn exact_directory_service_membership_nested_primary_alias_and_incomplete_controls() {
        let m=manifest();
        let u="RecordName: operator alias\nUniqueID: 501\nGeneratedUID: GUID\n";
        let g="RecordName: boxwarden-operators\nPrimaryGroupID: 1000\nGroupMembership: operator\nGroupMembers: GUID\n";
        let users="operator 20\nother 21\n";
        let groups="boxwarden-operators 1000\nother 20\n";
        assert!(exact_directory_state(&m,u,g,users,groups).is_ok());
        for bad in [g.replace("GroupMembership: operator","GroupMembership: operator other"),g.replace("GroupMembers: GUID","GroupMembers: GUID OTHER"),format!("{g}NestedGroups: NESTED\n"),g.replace("GroupMembers: GUID\n",""),g.replace("GroupMembership: operator\n",""),g.replace("RecordName: boxwarden-operators","RecordName: alias"),g.replace("PrimaryGroupID: 1000","PrimaryGroupID: 1001"),format!("{g}RecordName: boxwarden-operators\n")] {
            assert!(exact_directory_state(&m,u,&bad,users,groups).is_err());
        }
        for bad in ["operator 20\nother 1000\n","other 21\n","operator 20\noperator 20\n","operator\n","operator 20 extra\n",""] {
            assert!(exact_directory_state(&m,u,g,bad,groups).is_err());
        }
        for bad in ["boxwarden-operators 1000\nalias 1000\n","boxwarden-operators 20\n","other 20\n",""] {
            assert!(exact_directory_state(&m,u,g,users,bad).is_err());
        }
        for bad in [u.replace("UniqueID: 501","UniqueID: 502"),u.replace("GeneratedUID: GUID\n",""),u.replace("RecordName: operator alias","RecordName: operator operator"),u.replace("RecordName: operator alias","RecordName: alias")] {
            assert!(exact_directory_state(&m,&bad,g,users,groups).is_err());
        }
    }
}

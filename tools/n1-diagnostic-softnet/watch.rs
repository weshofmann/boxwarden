//! One private fd1 watch, nonblocking and finite. Diagnostic failures never select forwarding.
use super::{
    boxwarden_policy::Policy, dispatch::Observer, wire::*
};
use std::time::{Instant, SystemTime, UNIX_EPOCH};
use std::io;
// Resource cap only: trusted coordinator approval remains a separate authority.
const PREARM_CAP_NS: u64 = 1_800_000_000_000;
#[derive(Clone, Copy)]
struct Reading { wall: u64, continuous: u64, offset: u64 }
struct ClockGuard { last: Reading, wall_deadline: u64, continuous_deadline: u64, invalid: bool }
impl ClockGuard {
    fn new(anchor: Reading) -> io::Result<Self> {
        Ok(Self { last: anchor,
            wall_deadline: anchor.wall.checked_add(PREARM_CAP_NS).ok_or_else(clock_error)?,
            continuous_deadline: anchor.continuous.checked_add(PREARM_CAP_NS).ok_or_else(clock_error)?, invalid: false })
    }
    fn check(&mut self, reading: io::Result<Reading>, prearm: bool) -> io::Result<Reading> {
        let reading = reading.and_then(|r| {
            if self.invalid || r.wall < self.last.wall || r.continuous < self.last.continuous || r.offset < self.last.offset
                || (prearm && (r.wall >= self.wall_deadline || r.continuous >= self.continuous_deadline)) {
                Err(clock_error())
            } else { Ok(r) }
        });
        match reading {
            Ok(r) => { self.last = r; Ok(r) },
            Err(e) => { self.invalid = true; Err(e) },
        }
    }
}
fn clock_error() -> io::Error { io::Error::other("diagnostic clock invalid") }
fn scale_ticks(ticks: u64, numerator: u32, denominator: u32) -> io::Result<u64> {
    if numerator == 0 || denominator == 0 { return Err(clock_error()); }
    let ns = u128::from(ticks).checked_mul(u128::from(numerator)).ok_or_else(clock_error)? / u128::from(denominator);
    ns.try_into().map_err(|_| clock_error())
}
#[cfg(target_os="macos")]
fn continuous_now() -> io::Result<u64> {
    unsafe extern "C" { fn mach_continuous_time() -> u64; }
    let mut info = libc::mach_timebase_info { numer: 0, denom: 0 };
    if unsafe { libc::mach_timebase_info(&mut info) } != 0 { return Err(clock_error()); }
    scale_ticks(unsafe { mach_continuous_time() }, info.numer, info.denom)
}
#[cfg(target_os="linux")]
fn continuous_now() -> io::Result<u64> {
    let mut ts: libc::timespec = unsafe { std::mem::zeroed() };
    if unsafe { libc::clock_gettime(libc::CLOCK_BOOTTIME, &mut ts) } != 0 || ts.tv_sec < 0 || !(0..1_000_000_000).contains(&ts.tv_nsec) {
        return Err(clock_error());
    }
    (ts.tv_sec as u64).checked_mul(1_000_000_000).and_then(|n| n.checked_add(ts.tv_nsec as u64)).ok_or_else(clock_error)
}
#[cfg(not(any(target_os="macos",target_os="linux")))]
fn continuous_now() -> io::Result<u64> { Err(clock_error()) }
fn native_reading(epoch: Instant) -> io::Result<Reading> {
    let wall = SystemTime::now().duration_since(UNIX_EPOCH).map_err(|_| clock_error())?.as_nanos().try_into().map_err(|_| clock_error())?;
    let continuous = continuous_now()?;
    let offset = epoch.elapsed().as_nanos().try_into().map_err(|_| clock_error())?;
    Ok(Reading { wall, continuous, offset })
}
#[derive(Clone, Copy, PartialEq, Eq)]
enum Phase {
    Admitted, AwaitArm, Watching, Closed, Invalid
}
pub struct WatchChannel {
    selector: Selector, mac: [u8;     6], gateway: [u8;     4], fd: i32,
    phase: Phase, clock: Box<dyn FnMut() -> io::Result<Reading>>, guard: ClockGuard, hello_at: u64, start: u64, deadline: u64,
    incoming: Vec<u8>, output_bytes: usize, pub observer: Observer,
}
impl WatchChannel {
    pub fn admit(selector: Selector, mac: [u8;6]) -> io::Result<Self> {
        Self::admit_socket(selector,mac,1)
    }
    fn admit_socket(selector: Selector, mac: [u8;6], fd: i32) -> io::Result<Self> {
        let epoch = Instant::now();
        Self::admit_with_clock(selector, mac, fd, Box::new(move || native_reading(epoch)))
    }
    fn admit_with_clock(selector: Selector, mac: [u8;6], fd: i32, mut clock: Box<dyn FnMut() -> io::Result<Reading>>) -> io::Result<Self> {
        validate_socket(fd)?;
        let guard = ClockGuard::new(clock()?)?;
        Ok(Self {
            selector,mac,gateway:[0;4],fd,phase:Phase::Admitted,clock,guard,hello_at:0,start:0,deadline:0,incoming:Vec::with_capacity(MAX_FRAME+5),output_bytes:0,observer:Observer::default()
        })
    }
    pub fn hello_after_privdrop(&mut self, gateway: [u8;4]) {
        if self.phase!=Phase::Admitted {
            self.invalidate(13);
            return;
        }
        let Some(reading) = self.read_clock() else { return; };
        self.gateway=gateway;
        self.hello_at=reading.offset;
        self.phase=Phase::AwaitArm;
        let h=Hello {
            version:1,kind:"HELLO".into(),generation:self.selector.generation.clone(),nonce:self.selector.nonce.clone(),candidate_mac:self.mac,gateway
        };
        if !self.send(&h) {
            self.invalidate(12);
        }
    }
    fn read_clock(&mut self) -> Option<Reading> {
        let prearm = matches!(self.phase, Phase::Admitted | Phase::AwaitArm);
        match self.guard.check((self.clock)(), prearm) {
            Ok(r) => Some(r),
            Err(_) => {
                self.observer.fail(9);
                self.observer.fail(12);
                self.observer.arm = None;
                self.phase = Phase::Invalid;
                None
            }
        }
    }
    fn invalidate(&mut self, flag: usize) {
        self.observer.fail(flag);
        if self.phase!=Phase::Watching && self.phase!=Phase::Closed {
            self.phase=Phase::Invalid;
        }
    }
    fn send<T:serde::Serialize>(&mut self, value:&T) -> bool {
        let Ok(b)=frame(value) else {
            return false;
        };
        if self.output_bytes.checked_add(b.len()).is_none_or(|n|n>MAX_OUTPUT) {
            return false;
        }
        self.output_bytes+=b.len();
        let n=unsafe {
            libc::send(self.fd,b.as_ptr().cast(),b.len(),libc::MSG_DONTWAIT)
        };
        n==b.len() as isize // no queued retry after partial write/backpressure/error
    }
    pub fn service(&mut self, policy:&mut Policy, policy_now:u64) {
        let Some(reading) = self.read_clock() else { return; };
        let mut now = reading.offset;
        if self.phase==Phase::Admitted {
            return;
        }
        // read_clock checked the immutable admission cap before receiving queued bytes.
        // Bounded one read at every boundary. Any post-ARM byte is invalid; never resynchronize.
        if self.phase!=Phase::Invalid {
            let mut b=[0u8;             MAX_FRAME+5];
            let n=unsafe {
                libc::recv(self.fd,b.as_mut_ptr().cast(),b.len(),libc::MSG_DONTWAIT)
            };
            if n==0 {
                self.invalidate(12);
            }else if n<0 {
                if io::Error::last_os_error().kind()!=io::ErrorKind::WouldBlock {
                    self.invalidate(12);
                }
            }else if self.phase!=Phase::AwaitArm {
                self.invalidate(13);
            }else {
                if self.incoming.len()+n as usize>MAX_FRAME+4 {
                    self.invalidate(12);
                    self.incoming.clear();
                }else{
                    self.incoming.extend_from_slice(&b[..n as usize]);
                    if self.incoming.len()>=4 {
                        let len=u32::from_be_bytes(self.incoming[..4].try_into().unwrap())as usize;
                        if len==0 || len>MAX_FRAME || self.incoming.len()>len+4 {
                            self.invalidate(13);
                            self.incoming.clear();
                        }
                        else if self.incoming.len()==len+4 {
                            let arm=decode::<Arm>(&self.incoming[4..]);
                            self.incoming.clear();
                            // Decode is bounded, but cannot carry a buffered ARM across expiry.
                            let Some(decoded_at) = self.read_clock() else { return; };
                            now = decoded_at.offset;
                            match arm {
                                Ok(a) if a.valid(&self.selector,self.mac,self.gateway) && policy.candidate_lease_valid(a.candidate.address,a.candidate.mac,a.gateway,policy_now)=>{
                                    let Some(deadline) = now.checked_add(u64::from(a.duration_ms) * 1_000_000) else {
                                        self.observer.fail(9); self.phase = Phase::Invalid; return;
                                    };
                                    self.start=now;
                                    self.deadline=deadline;
                                    policy.bind_candidate(std::net::Ipv4Addr::from(a.candidate.address));
                                    let armed=Armed{
                                        version:1,kind:"ARMED".into(),generation:a.generation.clone(),nonce:a.nonce.clone(),operation_id:a.operation_id.clone(),candidate:a.candidate.clone(),control:a.control.clone(),gateway:a.gateway,duration_ms:a.duration_ms,control_provenance:a.control_provenance.clone(),armed_offset_ns:now,candidate_lease_valid:true
                                    };
                                    self.observer.activate(a);
                                    self.phase=Phase::Watching;
                                    if !self.send(&armed) {
                                        self.invalidate(12);
                                    }
                                }, _=>self.invalidate(13),
                            }
                        }
                    }
                }
            }
        }
        if self.phase==Phase::Watching {
            let a=self.observer.arm.as_ref().unwrap();
            let valid=policy.candidate_lease_valid(a.candidate.address,a.candidate.mac,a.gateway,policy_now);
            if !valid {
                self.invalidate(11);
            }
            if now>=self.deadline {
                // Scheduling tolerance is finite; the observer has already closed before next dispatch.
                if now.saturating_sub(self.deadline)>100_000_000 {
                    self.invalidate(10);
                }
                let summary=self.observer.summary(self.start,now,valid);
                self.phase=Phase::Closed;
                if let Some(s)=summary {
                    if !self.send(&s) {
                        self.observer.fail(12);
                    }
                }
                self.observer.arm=None;
                // counters/loss survive, no more interval tickets
            }
        }
    }
    #[cfg(test)]
    fn service_at(&mut self, policy: &mut Policy, policy_now: u64, now: u64) {
        let original = std::mem::replace(&mut self.clock, Box::new(move || Ok(Reading { wall: now, continuous: now, offset: now })));
        self.service(policy, policy_now);
        self.clock = original;
    }
    pub fn failed_read(&mut self) {
        self.observer.fail(7);
    }
    pub fn shutdown(&mut self) {
        if self.phase==Phase::Watching {
            self.observer.fail(10);
        }else if self.phase!=Phase::Closed {
            self.observer.fail(0);
        }
    }
}
fn validate_socket(fd:i32)->io::Result<()> {
    let reject=||io::Error::other("diagnostic fd1 admission failed");
    let flags=unsafe{
        libc::fcntl(fd,libc::F_GETFL)
    };
    let fdflags=unsafe{
        libc::fcntl(fd,libc::F_GETFD)
    };
    if flags<0 || flags&libc::O_NONBLOCK==0 || flags&libc::O_ACCMODE!=libc::O_RDWR || fdflags<0 || fdflags&libc::FD_CLOEXEC!=0 {
        return Err(reject());
    }
    let mut typ=0i32;
    let mut n=std::mem::size_of::<i32>()as libc::socklen_t;
    if unsafe{
        libc::getsockopt(fd,libc::SOL_SOCKET,libc::SO_TYPE,(&mut typ as *mut i32).cast(),&mut n)
    }!=0 || typ!=libc::SOCK_STREAM || n as usize!=std::mem::size_of::<i32>() {
        return Err(reject());
    }
    for peer in [false,true] {
        let mut addr:libc::sockaddr_un=unsafe{
            std::mem::zeroed()
        };
        let mut len=std::mem::size_of_val(&addr)as libc::socklen_t;
        let rc=unsafe {
            if peer {
                libc::getpeername(fd,(&mut addr as *mut libc::sockaddr_un).cast(),&mut len)
            }else{
                libc::getsockname(fd,(&mut addr as *mut libc::sockaddr_un).cast(),&mut len)
            }
        };
        if rc!=0 || addr.sun_family as i32!=libc::AF_UNIX || !unnamed(&addr,len as usize) {
            return Err(reject());
        }
    }
    #[cfg(target_os="macos")]
    {
        let one=1i32;
        if unsafe{
            libc::setsockopt(fd,libc::SOL_SOCKET,libc::SO_NOSIGPIPE,(&one as *const i32).cast(),std::mem::size_of::<i32>()as libc::socklen_t)
        }!=0{
            return Err(reject());
        }
    }
    Ok(())
}
fn unnamed(addr:&libc::sockaddr_un,len:usize)->bool {
    #[cfg(target_os="macos")]
    let shape=len==16&&addr.sun_len==16;
    #[cfg(not(target_os="macos"))]
    let shape=len==2;
    shape&&addr.sun_path.iter().all(|b|*b==0)
}
#[cfg(test)]
mod tests {
    use super::*;
    use crate::tests::{
        arm,leased,GA,CM
    };
    use std::os::fd::AsRawFd;
    use std::os::unix::net::UnixStream;
    use std::io::{
        Read,Write
    };
    fn pair()->(WatchChannel,UnixStream,UnixStream){
        let (child,parent)=UnixStream::pair().unwrap();
        child.set_nonblocking(true).unwrap();
        parent.set_nonblocking(true).unwrap();
        unsafe{
            assert_eq!(libc::fcntl(child.as_raw_fd(),libc::F_SETFD,0),0);
        }
        let a=arm();
        let s=Selector{
            generation:a.generation,nonce:a.nonce
        };
        let channel=WatchChannel::admit_with_clock(s,CM,child.as_raw_fd(),Box::new(||Ok(Reading { wall: 0, continuous: 0, offset: 0 }))).unwrap();
        (channel,child,parent)
    }
    fn receipt<T:for<'de>serde::Deserialize<'de>>(parent:&mut UnixStream)->T{
        let mut b=[0u8;         MAX_FRAME+4];
        let n=parent.read(&mut b).unwrap();
        assert!(n>=4);
        let len=u32::from_be_bytes(b[..4].try_into().unwrap())as usize;
        assert_eq!(n,len+4);
        decode(&b[4..n]).unwrap()
    }
    #[test]
    fn exact_socket_roles_reject_blocking_pipe_and_cloexec(){
        let(a,b)=UnixStream::pair().unwrap();
        assert!(validate_socket(a.as_raw_fd()).is_err());
        a.set_nonblocking(true).unwrap();
        assert!(validate_socket(a.as_raw_fd()).is_err());
        unsafe{
            libc::fcntl(a.as_raw_fd(),libc::F_SETFD,0);
        }assert!(validate_socket(a.as_raw_fd()).is_ok());
        assert!(validate_socket(-1).is_err());
        drop(b);
    }
    #[test]
    fn one_arm_idle_and_busy_boundaries_close_at_finite_deadline(){
        for busy in [false,true]{
            let(mut c,_child,mut p)=pair();
            let mut policy=leased();
            c.hello_after_privdrop(GA);
            let h:Hello=receipt(&mut p);
            assert_eq!(h.kind,"HELLO");
            p.write_all(&frame(&arm()).unwrap()).unwrap();
            c.service_at(&mut policy,1,1_000_000);
            let r:Armed=receipt(&mut p);
            assert_eq!(r.kind,"ARMED");
            assert_eq!(r.armed_offset_ns,1_000_000);
            if busy{
                for n in 1..1000{
                    c.service_at(&mut policy,1,1_000_000+n*1_000_000);
                }
            }else{
                c.service_at(&mut policy,1,900_000_000);
            }
            c.service_at(&mut policy,1,1_001_000_000);
            let s:Summary=receipt(&mut p);
            assert!(s.complete);
            assert_eq!(s.kind,"SUMMARY");
            assert_eq!(s.end_offset_ns-s.armed_offset_ns,1_000_000_000);
            assert!(matches!(c.phase,Phase::Closed));
            assert!(c.observer.arm.is_none());
            c.service_at(&mut policy,1,2_000_000_000);
            assert!(p.read(&mut[0;1]).is_err());
        }
    }
    #[test]
    fn partial_arm_duplicate_rearm_bad_order_lease_and_clock_are_sticky(){
        for failure in 0..5 {
            let(mut c,_child,mut p)=pair();
            let mut policy=leased();
            c.hello_after_privdrop(GA);
            let _:Hello=receipt(&mut p);
            let b=frame(&arm()).unwrap();
            p.write_all(&b[..3]).unwrap();
            c.service_at(&mut policy,1,10);
            assert!(matches!(c.phase,Phase::AwaitArm));
            p.write_all(&b[3..]).unwrap();
            c.service_at(&mut policy,1,20);
            let _:Armed=receipt(&mut p);
            match failure{
                0=>{
                    p.write_all(&b).unwrap();
                    c.service_at(&mut policy,1,30);
                },1=>c.service_at(&mut policy,700,30),2=>c.service_at(&mut policy,1,19),3=>c.failed_read(),_=>c.observer.fail(0)
            }
            c.service_at(&mut policy,1,1_000_000_020);
            if failure == 2 {
                assert!(matches!(c.phase,Phase::Invalid));
                assert!(c.observer.loss && c.observer.arm.is_none());
                assert_eq!(p.read(&mut [0;1]).unwrap_err().kind(),io::ErrorKind::WouldBlock);
                continue;
            }
            let s:Summary=receipt(&mut p);
            assert!(!s.complete);
            assert!(s.loss);
        }
    }
    #[test]
    fn malformed_extra_eof_arm_timeout_and_oversize_never_arm(){
        for mode in 0..5{
            let(mut c,_child,mut p)=pair();
            let mut policy=leased();
            c.hello_after_privdrop(GA);
            let _:Hello=receipt(&mut p);
            match mode{
                0=>p.write_all(&0u32.to_be_bytes()).unwrap(),1=>p.write_all(&4097u32.to_be_bytes()).unwrap(),2=>{
                    let mut b=frame(&arm()).unwrap();
                    b.push(0);
                    p.write_all(&b).unwrap();
                },3=>{
                    p.shutdown(std::net::Shutdown::Write).unwrap();
                },_=>{
                }
            }
            c.service_at(&mut policy,1,if mode==4 { PREARM_CAP_NS } else { 10 });
            assert!(matches!(c.phase,Phase::Invalid));
            assert!(c.observer.loss);
            assert!(c.observer.arm.is_none());
        }
    }
    #[test]
    fn queue_backpressure_never_retries_and_forwarding_remains_available(){
        let(mut c,child,mut p)=pair();
        let mut policy=leased();
        c.hello_after_privdrop(GA);
        let _:Hello=receipt(&mut p);
        let b=frame(&arm()).unwrap();
        p.write_all(&b).unwrap();
        c.service_at(&mut policy,1,10);
        let _:Armed=receipt(&mut p);
        let fill=[0u8;         4096];
        loop{
            let n=unsafe{
                libc::send(child.as_raw_fd(),fill.as_ptr().cast(),fill.len(),libc::MSG_DONTWAIT)
            };
            if n<0{
                break;
            }
        }
        c.service_at(&mut policy,1,1_000_000_010);
        assert!(matches!(c.phase,Phase::Closed));
        assert!(c.observer.invalid[12]);
        let prior=c.output_bytes;
        c.service_at(&mut policy,1,2_000_000_010);
        assert_eq!(c.output_bytes,prior);
        let mut writes=0;
        let packet=crate::tests::arp(crate::tests::CA,GA,CM,crate::tests::GM,1);
        super::super::dispatch::dispatch_guest(&mut policy,&packet,1,&mut c.observer,||Ok::<_,u8>((vec![GA.into()],vec![])),|_|false,|f|{
            writes+=1;Ok::<_,u8>(f.len())
        },|_|super::super::dispatch::WriteError::IoOther).unwrap();
        assert_eq!(writes,1);
    }
    #[test]
    fn activated_watch_and_dispatch_preserve_canonical_policy_state(){
        let(mut c,_child,mut peer)=pair();
        let mut policy=leased();
        let mut oracle=crate::tests::oracle();
        c.hello_after_privdrop(GA);
        let _:Hello=receipt(&mut peer);
        peer.write_all(&frame(&arm()).unwrap()).unwrap();
        c.service_at(&mut policy,1,10);
        let _:Armed=receipt(&mut peer);
        for (i,target) in [GA,crate::tests::PA,GA,crate::tests::PA].into_iter().enumerate(){
            c.service_at(&mut policy,1,20+i as u64);
            let f=crate::tests::arp(crate::tests::CA,target,CM,crate::tests::GM,1);
            let mut writes=0;
            let actual=super::super::dispatch::dispatch_guest(&mut policy,&f,1,&mut c.observer,||Ok::<_,u8>((vec![GA.into()],vec![])),|_|false,|b|{
                writes+=1;Ok::<_,u8>(b.len())
            },|_|super::super::dispatch::WriteError::IoOther).unwrap();
            let expected=oracle.forward_with_refresh(&f,1,||Ok::<_,u8>((vec![GA.into()],vec![])),|_|false,|_|Ok(())).unwrap();
            assert_eq!(actual,expected);
            assert_eq!(writes,usize::from(expected));
        }
        // The service observes expiry without clearing it. The next canonical
        // choosing dispatch still performs exactly the original expiry mutation.
        c.service_at(&mut policy,700,30);
        let f=crate::tests::arp(crate::tests::CA,GA,CM,crate::tests::GM,1);
        assert_eq!(policy.guest(&f,&[GA.into()],700)as u8,oracle.guest(&f,&[GA.into()],700)as u8);
        assert_eq!(policy.metadata().lease,2);
        c.service_at(&mut policy,700,1_000_000_010);
        let summary:Summary=receipt(&mut peer);
        assert!(!summary.complete);
        assert!(summary.invalid_flags[11]);
    }
    fn r1_assert_timeout_is_sticky_and_forwarding_passive(c:&mut WatchChannel,peer:&mut UnixStream,policy:&mut Policy,hello_bytes:usize) {
        assert!(matches!(c.phase,Phase::Invalid));
        assert!(c.observer.loss&&c.observer.invalid[12]);
        assert!(c.observer.arm.is_none());
        assert_eq!(c.output_bytes,hello_bytes);
        c.service_at(policy,1,PREARM_CAP_NS+1);
        assert!(matches!(c.phase,Phase::Invalid));
        assert!(c.observer.arm.is_none());
        assert_eq!(c.output_bytes,hello_bytes);
        assert_eq!(peer.read(&mut[0;1]).unwrap_err().kind(),io::ErrorKind::WouldBlock);
        let mut oracle=crate::tests::oracle();
        for target in [GA,crate::tests::PA] {
            let packet=crate::tests::arp(crate::tests::CA,target,CM,crate::tests::GM,1);
            let mut writes=0;
            let actual=super::super::dispatch::dispatch_guest(policy,&packet,1,&mut c.observer,||Ok::<_,u8>((vec![GA.into()],vec![])),|_|false,|bytes|{
                writes+=1;Ok::<_,u8>(bytes.len())
            },|_|super::super::dispatch::WriteError::IoOther).unwrap();
            let expected=oracle.forward_with_refresh(&packet,1,||Ok::<_,u8>((vec![GA.into()],vec![])),|_|false,|_|Ok(())).unwrap();
            assert_eq!(actual,expected);
            assert_eq!(writes,usize::from(expected));
        }
        let packet=crate::tests::arp(crate::tests::CA,GA,CM,crate::tests::GM,1);
        assert_eq!(policy.guest(&packet,&[GA.into()],700)as u8,oracle.guest(&packet,&[GA.into()],700)as u8);
        assert_eq!(policy.metadata().lease,2);
        // watch did not clear the actual lease
    }
    #[test]
    fn r1_hello_timeout_rejects_complete_arm_before_activation() {
        let(mut c,_child,mut peer)=pair();
        let mut policy=leased();
        c.hello_after_privdrop(GA);
        let _:Hello=receipt(&mut peer);
        c.hello_at=10;
        let hello_bytes=c.output_bytes;
        peer.write_all(&frame(&arm()).unwrap()).unwrap();
        c.service_at(&mut policy,1,PREARM_CAP_NS+1);
        r1_assert_timeout_is_sticky_and_forwarding_passive(&mut c,&mut peer,&mut policy,hello_bytes);
    }
    #[test]
    fn r1_hello_timeout_rejects_partial_arm_completed_after_expiry() {
        let(mut c,_child,mut peer)=pair();
        let mut policy=leased();
        c.hello_after_privdrop(GA);
        let _:Hello=receipt(&mut peer);
        c.hello_at=10;
        let hello_bytes=c.output_bytes;
        let bytes=frame(&arm()).unwrap();
        peer.write_all(&bytes[..bytes.len()-1]).unwrap();
        c.service_at(&mut policy,1,c.hello_at+1_000_000_000);
        assert!(matches!(c.phase,Phase::AwaitArm));
        assert!(c.observer.arm.is_none());
        peer.write_all(&bytes[bytes.len()-1..]).unwrap();
        c.service_at(&mut policy,1,PREARM_CAP_NS+1);
        r1_assert_timeout_is_sticky_and_forwarding_passive(&mut c,&mut peer,&mut policy,hello_bytes);
    }
    #[test]
    fn slice0_fixed_cap_preserves_before_boundary_admission() {
        for elapsed in [60_000_000_000,PREARM_CAP_NS-11] {
            let(mut c,_child,mut peer)=pair();
            let mut policy=leased();
            c.hello_after_privdrop(GA);
            let _:Hello=receipt(&mut peer);
            c.hello_at=10;
            peer.write_all(&frame(&arm()).unwrap()).unwrap();
            c.service_at(&mut policy,1,c.hello_at+elapsed);
            let armed:Armed=receipt(&mut peer);
            assert_eq!(armed.armed_offset_ns,c.hello_at+elapsed);
            assert!(matches!(c.phase,Phase::Watching));
            assert!(c.observer.arm.is_some());
            c.service_at(&mut policy,1,c.hello_at+elapsed+1_000_000_000);
            let summary:Summary=receipt(&mut peer);
            assert!(summary.complete);
        }
    }
    #[test]
    fn slice0_timely_hello_delayed_arm_over_thirty_seconds() {
        let(mut c,_child,mut peer)=pair();
        let mut policy=leased();
        c.hello_after_privdrop(GA);
        let _:Hello=receipt(&mut peer);
        peer.write_all(&frame(&arm()).unwrap()).unwrap();
        c.service_at(&mut policy,1,60_000_000_000);
        assert!(matches!(c.phase,Phase::Watching),"timely HELLO must allow one ARM after 30 seconds within the fixed admission cap");
        let armed:Armed=receipt(&mut peer);
        assert_eq!(armed.armed_offset_ns,60_000_000_000);
    }
    fn reading(wall:u64,continuous:u64,offset:u64)->Reading { Reading {wall,continuous,offset} }
    fn readings(c:&mut WatchChannel,values:Vec<io::Result<Reading>>) {
        let mut queue:std::collections::VecDeque<_>=values.into();
        c.clock=Box::new(move||queue.pop_front().expect("bounded clock-read contract"));
    }
    fn refused(c:&WatchChannel,peer:&mut UnixStream,hello_bytes:usize) {
        assert!(matches!(c.phase,Phase::Invalid));
        assert!(c.observer.loss && c.observer.arm.is_none());
        assert_eq!(c.output_bytes,hello_bytes);
        assert_eq!(peer.read(&mut [0;1]).unwrap_err().kind(),io::ErrorKind::WouldBlock);
    }
    #[test]
    fn slice0_queued_complete_exact_after_and_suspend_caps_refuse_before_recv() {
        for stamp in [reading(PREARM_CAP_NS,PREARM_CAP_NS,PREARM_CAP_NS),
            reading(PREARM_CAP_NS+1,PREARM_CAP_NS+1,PREARM_CAP_NS+1),
            reading(60_000_000_000,PREARM_CAP_NS,60_000_000_000),
            reading(PREARM_CAP_NS,60_000_000_000,60_000_000_000)] {
            let(mut c,child,mut peer)=pair();let mut policy=leased();
            c.hello_after_privdrop(GA);let _:Hello=receipt(&mut peer);let hello_bytes=c.output_bytes;
            let bytes=frame(&arm()).unwrap();peer.write_all(&bytes).unwrap();
            readings(&mut c,vec![Ok(stamp)]);c.service(&mut policy,1);
            refused(&c,&mut peer,hello_bytes);
            // No queued byte was consumed at expired admission.
            let mut queued=vec![0;bytes.len()];assert_eq!(unsafe {libc::recv(child.as_raw_fd(),queued.as_mut_ptr().cast(),queued.len(),libc::MSG_DONTWAIT)},bytes.len() as isize);
            assert_eq!(queued,bytes);
            readings(&mut c,vec![Ok(reading(PREARM_CAP_NS-1,PREARM_CAP_NS-1,PREARM_CAP_NS-1))]);c.service(&mut policy,1);
            refused(&c,&mut peer,hello_bytes);
        }
    }
    #[test]
    fn slice0_partial_final_bytes_at_cap_do_not_renew_admission() {
        let(mut c,_child,mut peer)=pair();let mut policy=leased();
        c.hello_after_privdrop(GA);let _:Hello=receipt(&mut peer);let hello_bytes=c.output_bytes;
        let bytes=frame(&arm()).unwrap();peer.write_all(&bytes[..bytes.len()-1]).unwrap();
        c.service_at(&mut policy,1,PREARM_CAP_NS-1);assert_eq!(c.incoming.len(),bytes.len()-1);
        peer.write_all(&bytes[bytes.len()-1..]).unwrap();c.service_at(&mut policy,1,PREARM_CAP_NS);
        refused(&c,&mut peer,hello_bytes);assert_eq!(c.incoming.len(),bytes.len()-1);
    }
    #[test]
    fn slice0_postdecode_expiry_error_regression_and_interval_overflow_never_bind() {
        for (first,second) in [
            (reading(PREARM_CAP_NS-1,PREARM_CAP_NS-1,100),Ok(reading(PREARM_CAP_NS,PREARM_CAP_NS,101))),
            (reading(100,100,100),Ok(reading(99,101,101))),
            (reading(100,100,100),Ok(reading(101,99,101))),
            (reading(100,100,100),Ok(reading(101,101,99))),
            (reading(100,100,100),Err(clock_error())),
            (reading(1,1,u64::MAX-1),Ok(reading(2,2,u64::MAX-1))),
        ] {
            let(mut c,_child,mut peer)=pair();let mut policy=leased();
            c.hello_after_privdrop(GA);let _:Hello=receipt(&mut peer);let hello_bytes=c.output_bytes;
            // A foreign sentinel makes an unintended bind visible to the real choosing branch.
            policy.bind_candidate(crate::tests::PA.into());
            let before=policy.metadata();peer.write_all(&frame(&arm()).unwrap()).unwrap();
            readings(&mut c,vec![Ok(first),second]);c.service(&mut policy,1);
            refused(&c,&mut peer,hello_bytes);assert!(c.incoming.is_empty());
            assert_eq!(policy.metadata(),before);
            let packet=crate::tests::arp(crate::tests::CA,GA,CM,crate::tests::GM,1);
            let mut oracle=crate::tests::oracle();
            assert_eq!(policy.guest(&packet,&[GA.into()],1)as u8,oracle.guest(&packet,&[GA.into()],1)as u8);
            assert_eq!(policy.metadata().lease,4,"actual choosing branch must remain unbound after refused ARM");
        }
    }
    #[test]
    fn slice0_native_and_admission_clock_failures_and_checked_scaling() {
        let epoch=Instant::now();let a=native_reading(epoch).unwrap();let b=native_reading(epoch).unwrap();
        assert!(b.wall>=a.wall && b.continuous>=a.continuous && b.offset>=a.offset);
        assert_eq!(scale_ticks(10,3,2).unwrap(),15);
        for (ticks,num,den) in [(1,0,1),(1,1,0),(u64::MAX,2,1)] { assert!(scale_ticks(ticks,num,den).is_err()); }
        for anchor in [reading(u64::MAX,0,0),reading(0,u64::MAX,0)] { assert!(ClockGuard::new(anchor).is_err()); }
        let(c,child,_peer)=pair();
        assert!(WatchChannel::admit_with_clock(c.selector.clone(),CM,child.as_raw_fd(),Box::new(||Err(clock_error()))).is_err());
        assert!(WatchChannel::admit_with_clock(c.selector.clone(),CM,child.as_raw_fd(),Box::new(||Ok(reading(u64::MAX,0,0)))).is_err());
    }
    #[test]
    fn slice0_native_production_admission_service_and_local_offsets() {
        let(c,child,mut peer)=pair();
        let mut c=WatchChannel::admit_socket(c.selector,CM,child.as_raw_fd()).unwrap();
        let mut policy=leased();c.hello_after_privdrop(GA);let _:Hello=receipt(&mut peer);
        peer.write_all(&frame(&arm()).unwrap()).unwrap();c.service(&mut policy,1);
        let armed:Armed=receipt(&mut peer);assert!(armed.armed_offset_ns>=c.hello_at);
        assert!(matches!(c.phase,Phase::Watching));assert!(c.observer.arm.is_some());
        c.shutdown();assert!(c.observer.loss);
    }
    #[test]
    fn slice0_late_hello_and_clock_fail_then_valid_are_sticky() {
        let(mut c,_child,mut peer)=pair();readings(&mut c,vec![Ok(reading(PREARM_CAP_NS,PREARM_CAP_NS,1))]);
        c.hello_after_privdrop(GA);refused(&c,&mut peer,0);
        for fault in [Err(clock_error()),Ok(reading(99,101,101)),Ok(reading(101,99,101)),Ok(reading(101,101,99))] {
            let(mut c,_child,mut peer)=pair();let mut policy=leased();
            c.hello_after_privdrop(GA);let _:Hello=receipt(&mut peer);let hello_bytes=c.output_bytes;
            readings(&mut c,vec![Ok(reading(100,100,100)),fault,Ok(reading(102,102,102))]);
            c.service(&mut policy,1);assert!(matches!(c.phase,Phase::AwaitArm));
            c.service(&mut policy,1);refused(&c,&mut peer,hello_bytes);
            peer.write_all(&frame(&arm()).unwrap()).unwrap();c.service(&mut policy,1);refused(&c,&mut peer,hello_bytes);
        }
    }
    #[test]
    fn slice0_delayed_hello_cannot_restart_anchor_and_one_arm_cannot_rearm() {
        let(mut c,_child,mut peer)=pair();let mut policy=leased();
        readings(&mut c,vec![Ok(reading(PREARM_CAP_NS-1,PREARM_CAP_NS-1,PREARM_CAP_NS-1)),Ok(reading(PREARM_CAP_NS,PREARM_CAP_NS,PREARM_CAP_NS))]);
        c.hello_after_privdrop(GA);let _:Hello=receipt(&mut peer);let hello_bytes=c.output_bytes;
        peer.write_all(&frame(&arm()).unwrap()).unwrap();c.service(&mut policy,1);refused(&c,&mut peer,hello_bytes);
        let(mut c,_child,mut peer)=pair();let mut policy=leased();c.hello_after_privdrop(GA);let _:Hello=receipt(&mut peer);
        peer.write_all(&frame(&arm()).unwrap()).unwrap();c.service_at(&mut policy,1,60_000_000_000);let _:Armed=receipt(&mut peer);
        peer.write_all(&frame(&arm()).unwrap()).unwrap();c.service_at(&mut policy,1,60_000_000_001);
        assert!(c.observer.invalid[13]);c.service_at(&mut policy,1,61_000_000_000);let summary:Summary=receipt(&mut peer);assert!(!summary.complete);
        peer.write_all(&frame(&arm()).unwrap()).unwrap();c.service_at(&mut policy,1,62_000_000_000);assert!(matches!(c.phase,Phase::Closed));
        assert!(c.observer.arm.is_none());assert_eq!(peer.read(&mut [0;1]).unwrap_err().kind(),io::ErrorKind::WouldBlock);
    }
    #[test]
    fn anonymous_sockaddr_negative_shapes(){
        let mut a:libc::sockaddr_un=unsafe{
            std::mem::zeroed()
        };
        a.sun_family=libc::AF_UNIX as _;
        #[cfg(target_os="macos")] {
            a.sun_len=16;
            assert!(unnamed(&a,16));
            a.sun_len=15;
            assert!(!unnamed(&a,16));
            a.sun_len=16;
            assert!(!unnamed(&a,15));
            a.sun_path[0]=1;
            assert!(!unnamed(&a,16));
        }#[cfg(not(target_os="macos"))]{
            assert!(unnamed(&a,2));
            assert!(!unnamed(&a,3));
            a.sun_path[0]=1;
            assert!(!unnamed(&a,2));
        }
    }
}

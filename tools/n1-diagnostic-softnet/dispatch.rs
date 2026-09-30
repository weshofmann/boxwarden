//! Actual forwarding seam; injected closures preserve canonical ordering/results.
use super::boxwarden_policy::{
    Policy, Decision, DecisionMetadata
};
use super::wire::{
    Arm, Counters, Summary
};
use std::net::Ipv4Addr;
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum WriteError {
    None, VmnetFailure, VmnetMemFailure, VmnetInvalidArgument, VmnetSetupIncomplete, VmnetInvalidAccess, VmnetPacketTooBig, VmnetBufferExhausted, VmnetTooManyPackets, VmnetSharingServiceBusy, VmnetUnknownStatus, IoEnobufs, IoWouldBlock, IoInterrupted, IoOther
}
#[derive(Debug)]
pub enum DispatchError<R,W> {
    Refresh(R), Write(W)
}
#[derive(Clone, Copy, Debug)]
struct Ticket {
    id: u16, dir: usize, row: Option<usize>
}
#[derive(Default)]
pub struct Observer {
    pub arm: Option<Arm>, pub counters: Counters, pub invalid: [bool;     14],
    pub overflow: bool, pub loss: bool, pending: [Option<Ticket>;     2],
}
fn inc(v: &mut u16, overflow: &mut bool) {
    if *v >= 4096 {
        *overflow = true;
    } else {
        *v += 1;
    }
}
fn bytes_add(v: &mut u64, n: usize, overflow: &mut bool) {
    match v.checked_add(n as u64) {
        Some(n) => *v=n, None => *overflow=true
    }
}
impl Observer {
    pub fn activate(&mut self, arm: Arm) {
        self.arm=Some(arm);
    }
    pub fn fail(&mut self, flag: usize) {
        self.invalid[flag]=true;
        self.loss=true;
    }
    fn begin(&mut self, dir: usize, frame: &[u8]) -> Option<Ticket> {
        if self.arm.is_none() {
            return None;
        }
        if self.pending[dir].is_some() {
            self.fail(1);
        }
        let n=if dir==0 {
            &mut self.counters.vm_dispatch_started
        } else {
            &mut self.counters.host_dispatch_started
        };
        if *n>=4096 {
            self.overflow=true;
            self.loss=true;
            return None;
        } *n+=1;
        let id=*n;
        let row=self.classify(dir,frame);
        if let Some(r)=row {
            if dir==0 {
                inc(&mut self.counters.vm_identified[r],&mut self.overflow);
            } else {
                inc(&mut self.counters.host_reply_class[r],&mut self.overflow);
            }
        }
        let t=Ticket{
            id,dir,row
        };
        self.pending[dir]=Some(t);
        Some(t)
    }
    fn classify(&mut self, dir: usize, f: &[u8]) -> Option<usize> {
        let a=self.arm.as_ref()?;
        if f.len()<14 {
            self.fail(3);
            return None;
        }
        let (src,dst)=if dir==0 {
            (a.candidate.address,a.control.address)
        }else{
            (a.control.address,a.candidate.address)
        };
        let typ=u16::from_be_bytes([f[12],f[13]]);
        let p=&f[14..];
        if typ==0x0806 {
            if p.len()<28 {
                self.fail(4);
                return None;
            }
            if p[14..18]!=src || p[24..28]!=dst {
                return None;
            }
            if p[..6]!=[0,1,8,0,6,4] || !matches!(p.len(),28|46) {
                inc(&mut self.counters.recognized_unsupported_pair[dir],&mut self.overflow);
                self.fail(5);
                return None;
            }
            let opcode=u16::from_be_bytes([p[6],p[7]]);
            if !matches!(opcode,1|2) {
                inc(&mut self.counters.recognized_unsupported_pair[dir],&mut self.overflow);
                self.fail(5);
                return None;
            }
            if dir==0 {
                return Some(if opcode==1 {
                    0
                }else{
                    1
                });
            }
            if opcode==1 {
                inc(&mut self.counters.host_pair_arp_request,&mut self.overflow);
                return None;
            }
            let expected=f[6..12]==a.control.mac && p[8..14]==a.control.mac;
            let dest=if f[..6]==a.candidate.mac {
                0
            }else if f[..6]==[255;             6] {
                1
            }else{
                2
            };
            return Some(dest+if expected {
                0
            }else{
                3
            });
        }
        if typ==0x0800 {
            if p.len()<20 {
                self.fail(4);
                return None;
            }
            if p[12..16]!=src || p[16..20]!=dst {
                return None;
            }
            let ihl=(p[0]&15) as usize*4;
            let len=u16::from_be_bytes([p[2],p[3]])as usize;
            if ihl<20 || len<ihl || p.len()<len {
                inc(&mut self.counters.recognized_truncated_pair[dir],&mut self.overflow);
                self.fail(6);
                return None;
            }
            if p[0]!=0x45 || p[6]&0xbf!=0 || p[7]!=0 {
                inc(&mut self.counters.recognized_unsupported_pair[dir],&mut self.overflow);
                self.fail(5);
                return None;
            }
            if dir==0 && p[9]==6 {
                if len<40 {
                    inc(&mut self.counters.recognized_truncated_pair[0],&mut self.overflow);
                    self.fail(6);
                    return None;
                }
                let t=&p[20..len];
                if t[2..4]==[0,22] && t[13]&0x12==2 {
                    return Some(2);
                }
            }
        }
        None
    }
    fn refresh(&mut self, ticket: Option<Ticket>, result: usize) {
        if let Some(t)=ticket {
            inc(&mut self.counters.refresh_results[t.dir][result],&mut self.overflow);
            if result==2 {
                self.loss=true;
            }
        }
    }
    fn decision(&mut self, ticket: Option<Ticket>, d: Decision, m: DecisionMetadata, fallback: Option<bool>) {
        if let Some(t)=ticket {
            inc(&mut self.counters.vm_policy_decisions[match d {
                Decision::Allow=>0,Decision::Deny=>1,Decision::Fallback=>2
            }],&mut self.overflow);
            inc(&mut self.counters.vm_arp_evaluation[m.arp_reason],&mut self.overflow);
            if let Some(f)=fallback {
                inc(&mut self.counters.vm_fallback_results[usize::from(f)],&mut self.overflow);
            }
            if let Some(r)=t.row {
                inc(&mut self.counters.vm_lease_state[r][m.lease],&mut self.overflow);
                inc(&mut self.counters.vm_target_predicates[r][0][m.gateway_equal],&mut self.overflow);
                inc(&mut self.counters.vm_target_predicates[r][1][m.local_contains],&mut self.overflow);
            }
        }
    }
    fn unevaluated(&mut self,t:Option<Ticket>) {
        if let Some(Ticket{
            dir:0,row:Some(r),..
        })=t {
            inc(&mut self.counters.vm_lease_state[r][0],&mut self.overflow);
            for c in 0..2 {
                inc(&mut self.counters.vm_target_predicates[r][c][0],&mut self.overflow);
            }
        }
    }
    fn end(&mut self, t: Option<Ticket>, outcome: usize, write: Option<(usize,Option<usize>,WriteError)>) {
        let Some(t)=t else {
            return;
        };
        if !self.pending[t.dir].is_some_and(|p|p.id==t.id) {
            self.fail(2);
            return;
        }
        self.pending[t.dir]=None;
        inc(if t.dir==0 {
            &mut self.counters.vm_dispatch_completed
        }else{
            &mut self.counters.host_dispatch_completed
        },&mut self.overflow);
        if let Some(r)=t.row {
            if t.dir==0 {
                inc(&mut self.counters.vm_outcomes[r][outcome],&mut self.overflow);
            }else{
                inc(&mut self.counters.host_class_outcomes[r][outcome],&mut self.overflow);
            }
            if let Some((requested,returned,error))=write {
                let (attempts,bytes,errors)=if t.dir==0 {
                    (&mut self.counters.vm_write_attempts[r],&mut self.counters.vm_write_bytes[r],&mut self.counters.vm_write_errors)
                }else{
                    (&mut self.counters.host_write_attempts[r],&mut self.counters.host_write_bytes[r],&mut self.counters.host_write_errors)
                };
                inc(attempts,&mut self.overflow);
                bytes_add(&mut bytes[0],requested,&mut self.overflow);
                if let Some(n)=returned {
                    bytes_add(&mut bytes[1],n,&mut self.overflow);
                }
                inc(&mut errors[error as usize],&mut self.overflow);
            }
        }
        if let Some((requested,Some(returned),_))=write {
            if requested!=returned {
                self.fail(8);
            }
        }
    }
    pub fn dropped_outer(&mut self, dir: usize, frame: &[u8]) {
        let t=self.begin(dir,frame);
        self.refresh(t,0);
        if dir==0 {
            self.unevaluated(t);
        }self.fail(3);
        self.end(t,if dir==0 {
            1
        }else{
            0
        },None);
    }
    pub fn accounting(&self) -> bool {
        let c=&self.counters;
        let sum=|a:&[u16]|a.iter().map(|n|*n as u32).sum::<u32>();
        if self.pending.iter().any(Option::is_some) || c.vm_dispatch_started!=c.vm_dispatch_completed || c.host_dispatch_started!=c.host_dispatch_completed {
            return false;
        }
        if sum(&c.refresh_results[0])!=c.vm_dispatch_started as u32 || sum(&c.refresh_results[1])!=c.host_dispatch_started as u32 {
            return false;
        }
        for r in 0..3 {
            if sum(&c.vm_outcomes[r])!=c.vm_identified[r] as u32 || sum(&c.vm_lease_state[r])!=c.vm_identified[r] as u32 {
                return false;
            }
            for col in 0..2 {
                if sum(&c.vm_target_predicates[r][col])!=c.vm_identified[r] as u32 {
                    return false;
                }
            }
            if c.vm_write_attempts[r] as u32!=sum(&c.vm_outcomes[r][3..6]) {
                return false;
            }
        }
        for r in 0..6 {
            if sum(&c.host_class_outcomes[r])!=c.host_reply_class[r] as u32 || c.host_write_attempts[r] as u32!=sum(&c.host_class_outcomes[r][2..6]) {
                return false;
            }
        }
        true
    }
    pub fn summary(&mut self, start:u64,end:u64,lease_valid:bool) -> Option<Summary> {
        if self.pending.iter().any(Option::is_some) {
            self.fail(1);
        }
        if !self.accounting() {
            self.fail(2);
        }
        if !lease_valid {
            self.fail(11);
        }
        let a=self.arm.as_ref()?;
        Some(Summary{
            version:1,kind:"SUMMARY".into(),generation:a.generation.clone(),nonce:a.nonce.clone(),operation_id:a.operation_id.clone(),candidate:a.candidate.clone(),control:a.control.clone(),gateway:a.gateway,duration_ms:a.duration_ms,control_provenance:a.control_provenance.clone(),armed_offset_ns:start,end_offset_ns:end,candidate_lease_valid:lease_valid,coverage_scope:"identified_pair_headers".into(),zero_count_attribution:false,packet_count_unobserved:true,complete:lease_valid&&!self.overflow&&!self.loss&&!self.invalid.iter().any(|b|*b),overflow:self.overflow,loss:self.loss,invalid_flags:self.invalid,counters:self.counters.clone()
        })
    }
}
pub fn dispatch_guest<R,W>(policy:&mut Policy,frame:&[u8],now:u64,observer:&mut Observer,
refresh:impl FnOnce()->Result<(Vec<Ipv4Addr>,Vec<Ipv4Addr>),R>,fallback:impl FnOnce(&[u8])->bool,
write:impl FnOnce(&[u8])->Result<usize,W>,category:impl FnOnce(&W)->WriteError)->Result<bool,DispatchError<R,W>> {
    let t=observer.begin(0,frame);
    let (locals,broadcasts)=match refresh(){
        Ok(v)=>{
            observer.refresh(t,1);
            v
        },Err(e)=>{
            observer.refresh(t,2);
            observer.unevaluated(t);
            observer.end(t,2,None);
            return Err(DispatchError::Refresh(e));
        }
    };
    let decision=policy.guest_with_broadcasts(frame,&locals,&broadcasts,now);
    let m=policy.metadata();
    let (allowed,f)=match decision {
        Decision::Allow=>(true,None),Decision::Deny=>(false,None),Decision::Fallback=>{
            let b=fallback(frame);
            (b,Some(b))
        }
    };
    observer.decision(t,decision,m,f);
    if !allowed {
        observer.end(t,if t.is_some_and(|t|matches!(t.row,Some(0|1)))&&m.arp_reason==4 {
            0
        }else{
            1
        },None);
        return Ok(false);
    }
    match write(frame) {
        Ok(n)=>{
            observer.end(t,if n==frame.len(){
                3
            }else{
                4
            },Some((frame.len(),Some(n),WriteError::None)));
            Ok(true)
        },Err(e)=>{
            let c=category(&e);
            observer.end(t,5,Some((frame.len(),None,c)));
            Err(DispatchError::Write(e))
        }
    }
}
pub fn dispatch_host<R,W>(policy:&mut Policy,frame:&[u8],observer:&mut Observer,
allowlist:impl FnOnce(&[u8])->bool,refresh:impl FnOnce()->Result<(Vec<Ipv4Addr>,Vec<Ipv4Addr>),R>,now:impl FnOnce()->u64,
write:impl FnOnce(&[u8])->Result<usize,W>,category:impl FnOnce(&W)->WriteError,enobufs:&mut bool)->Result<(),DispatchError<R,W>> {
    let t=observer.begin(1,frame);
    if !allowlist(frame) {
        observer.refresh(t,0);
        observer.end(t,0,None);
        return Ok(());
    }
    let locals=match refresh(){
        Ok(v)=>{
            observer.refresh(t,1);
            v
        },Err(e)=>{
            observer.refresh(t,2);
            observer.end(t,1,None);
            return Err(DispatchError::Refresh(e));
        }
    };
    let _=policy.host(frame,&locals.0,now());
    // literal canonical return is deliberately ignored
    match write(frame) {
        Ok(n)=>{
            observer.end(t,if n==frame.len(){
                2
            }else{
                3
            },Some((frame.len(),Some(n),WriteError::None)));
            Ok(())
        },Err(e)=>{
            let c=category(&e);
            observer.end(t,if c==WriteError::IoEnobufs {
                5
            }else{
                4
            },Some((frame.len(),None,c)));
            if c==WriteError::IoEnobufs {
                *enobufs=true;
                Ok(())
            }else{
                Err(DispatchError::Write(e))
            }
        }
    }
}
#[cfg(test)]
mod tests {
    use super::*;
    #[test]fn missing_pending_and_mismatched_tickets_cannot_complete(){
        let mut o=Observer::default();
        assert!(o.summary(0,1,true).is_none());
        o.activate(crate::tests::arm());
        let f=crate::tests::arp(crate::tests::CA,crate::tests::PA,crate::tests::CM,crate::tests::PM,1);
        let t=o.begin(0,&f).unwrap();
        assert!(!o.summary(0,1,true).unwrap().complete);
        assert!(o.invalid[1]);
        let mut wrong=t;
        wrong.id+=1;
        o.end(Some(wrong),1,None);
        assert!(o.invalid[2]);
        assert!(o.loss);
    }
}

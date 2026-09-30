//! Trial-only passive accounting; not linked into any admitted Softnet artifact.
use std::net::Ipv4Addr;

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Session {
    pub domain: String,
    pub session: String,
    pub generation: String,
    pub backend: String,
    pub address: Ipv4Addr,
    pub mac: [u8; 6],
}
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Watch {
    candidate: Session,
    peer: Session,
    gateway: Ipv4Addr,
    start: u64,
    end: u64,
}
#[derive(Clone, Copy, Debug)]
pub enum GuestOutcome { PeerTargetDenied, OtherDenied, RefreshError, Written, WriteError }
#[derive(Clone, Copy, Debug)]
pub enum ReturnOutcome { Delivered, WriteError, Enobufs }
#[derive(Clone, Copy, Default, Debug, PartialEq, Eq)]
pub struct Counts {
    pub received: u32, pub requests: u32, pub replies: u32,
    pub peer_target_denied: u32, pub other_denied: u32, pub refresh_error: u32,
    pub allowed: u32, pub write_attempt: u32, pub write_success: u32, pub write_error: u32,
    pub return_reply: u32, pub return_delivered: u32, pub return_error: u32, pub return_enobufs: u32,
}
#[derive(Clone, Debug)]
pub struct Snapshot { pub watch: Watch, pub counts: Counts, pub complete: bool, pub overflow: bool }
pub struct Observer {
    watch: Watch, counts: Counts, overflow: bool,
    pending_guest: Option<u32>, pending_return: Option<u32>,
}
pub struct GuestTicket { watch: Watch, serial: u32 }
pub struct ReturnTicket { watch: Watch, serial: u32 }
impl Watch {
    pub fn new(candidate: Session, peer: Session, gateway: Ipv4Addr, start: u64, end: u64) -> Result<Self, &'static str> {
        for session in [&candidate, &peer] {
            if session.domain != "n1qualification"
                || !uuid(&session.session) || !uuid(&session.generation)
                || session.session == session.generation
                || session.backend.is_empty() || session.backend.len() > 128
                || !session.backend.bytes().all(|c| c.is_ascii_alphanumeric() || c == b'-' || c == b'_')
                || !session.address.is_private() || session.mac == [0; 6] || session.mac[0] & 1 != 0
            { return Err("invalid immutable session binding"); }
        }
        if candidate.session == peer.session || candidate.generation == peer.generation
            || candidate.backend == peer.backend || candidate.address == peer.address || candidate.mac == peer.mac
            || !gateway.is_private() || gateway == candidate.address || gateway == peer.address
            || !end.checked_sub(start).is_some_and(|duration| duration > 0 && duration <= 30)
        { return Err("invalid pair or observation window"); }
        Ok(Self { candidate, peer, gateway, start, end })
    }
}
fn uuid(s: &str) -> bool {
    s.len() == 36 && s.bytes().enumerate().all(|(i, c)| {
        if [8, 13, 18, 23].contains(&i) { c == b'-' }
        else { c.is_ascii_digit() || (b'a'..=b'f').contains(&c) }
    }) && s.bytes().any(|c| c != b'0' && c != b'-')
}
// Reads only Ethernet and the fixed 28-byte ARP header. Never retains bytes.
fn arp_matches(frame: &[u8], sender: &Session, target: &Session) -> Option<u8> {
    if (frame.len() != 42 && frame.len() != 60)
        || frame[6..12] != sender.mac || frame[12..20] != [8, 6, 0, 1, 8, 0, 6, 4]
        || frame[20] != 0 || !matches!(frame[21], 1 | 2)
        || frame[22..28] != sender.mac || frame[28..32] != sender.address.octets()
        || frame[38..42] != target.address.octets()
    { return None; }
    Some(frame[21])
}
impl Observer {
    pub fn new(watch: Watch) -> Self { Self {
        watch, counts: Counts::default(), overflow: false, pending_guest: None, pending_return: None,
    } }
    /// Count real ingress before refresh/decision/write, without deciding policy.
    pub fn begin_guest(&mut self, frame: &[u8], now: u64) -> Option<GuestTicket> {
        if now < self.watch.start || now >= self.watch.end { return None; }
        let op = arp_matches(frame, &self.watch.candidate, &self.watch.peer)?;
        if self.counts.received == 4096 || self.pending_guest.is_some() { self.overflow = true; return None; }
        self.counts.received += 1;
        if op == 1 { self.counts.requests += 1; } else { self.counts.replies += 1; }
        self.pending_guest = Some(self.counts.received);
        Some(GuestTicket { watch: self.watch.clone(), serial: self.counts.received })
    }
    /// Called AFTER actual forwarding. Obtain the reason from the actual policy
    /// branch, never infer PeerTargetDenied from an errno or target address alone.
    pub fn complete_guest(&mut self, ticket: GuestTicket, result: GuestOutcome) {
        if ticket.watch != self.watch || self.pending_guest != Some(ticket.serial) { self.overflow = true; return; }
        self.pending_guest = None;
        match result {
            GuestOutcome::PeerTargetDenied => self.counts.peer_target_denied += 1,
            GuestOutcome::OtherDenied => self.counts.other_denied += 1,
            GuestOutcome::RefreshError => self.counts.refresh_error += 1,
            GuestOutcome::Written | GuestOutcome::WriteError => {
                self.counts.allowed += 1; self.counts.write_attempt += 1;
                if matches!(result, GuestOutcome::Written) { self.counts.write_success += 1; }
                else { self.counts.write_error += 1; }
            }
        }
    }
    /// Count real vmnet ingress before VM::write.
    pub fn begin_return(&mut self, frame: &[u8], now: u64) -> Option<ReturnTicket> {
        if now < self.watch.start || now >= self.watch.end
            || arp_matches(frame, &self.watch.peer, &self.watch.candidate) != Some(2)
            || frame[..6] != self.watch.candidate.mac
        { return None; }
        if self.counts.return_reply == 4096 || self.pending_return.is_some() { self.overflow = true; return None; }
        self.counts.return_reply += 1;
        self.pending_return = Some(self.counts.return_reply);
        Some(ReturnTicket { watch: self.watch.clone(), serial: self.counts.return_reply })
    }
    /// Called AFTER actual VM::write, including upstream's swallowed ENOBUFS.
    pub fn complete_return(&mut self, ticket: ReturnTicket, result: ReturnOutcome) {
        if ticket.watch != self.watch || self.pending_return != Some(ticket.serial) { self.overflow = true; return; }
        self.pending_return = None;
        match result {
            ReturnOutcome::Delivered => self.counts.return_delivered += 1,
            ReturnOutcome::WriteError => self.counts.return_error += 1,
            ReturnOutcome::Enobufs => self.counts.return_enobufs += 1,
        }
    }
    pub fn snapshot(&self, now: u64) -> Snapshot { Snapshot {
        watch: self.watch.clone(), counts: self.counts,
        complete: now >= self.watch.end && self.pending_guest.is_none() && self.pending_return.is_none(), overflow: self.overflow,
    } }
    #[cfg(test)]
    fn guest(&mut self, frame: &[u8], now: u64, result: GuestOutcome) {
        if let Some(ticket) = self.begin_guest(frame, now) { self.complete_guest(ticket, result); }
    }
    #[cfg(test)]
    fn returned(&mut self, frame: &[u8], now: u64, result: ReturnOutcome) {
        if let Some(ticket) = self.begin_return(frame, now) { self.complete_return(ticket, result); }
    }
}
impl Snapshot {
    /// Arithmetic consistency, not authenticity or a qualification verdict.
    pub fn validate(&self, expected: &Watch) -> Result<(), &'static str> {
        if self.watch != *expected || !self.complete || self.overflow { return Err("unbound, incomplete or overflowed observation"); }
        let c = self.counts;
        let sum = |values: &[u32]| values.iter().map(|v| u64::from(*v)).sum::<u64>();
        if c.received > 4096 || c.return_reply > 4096
            || u64::from(c.received) != sum(&[c.requests, c.replies])
            || u64::from(c.received) != sum(&[c.peer_target_denied, c.other_denied, c.refresh_error, c.allowed])
            || c.allowed != c.write_attempt
            || u64::from(c.write_attempt) != sum(&[c.write_success, c.write_error])
            || u64::from(c.return_reply) != sum(&[c.return_delivered, c.return_error, c.return_enobufs])
        { return Err("contradictory or unfinished boundary accounting"); }
        Ok(())
    }
}

#[cfg(test)]
#[path = "../n1-softnet/policy.rs"]
mod policy;
#[cfg(test)]
mod tests {
    use super::*;
    use std::cell::Cell;
    const C: Ipv4Addr = Ipv4Addr::new(192, 168, 64, 3);
    const P: Ipv4Addr = Ipv4Addr::new(192, 168, 64, 2);
    const G: Ipv4Addr = Ipv4Addr::new(192, 168, 64, 1);
    const CM: [u8; 6] = [2, 0, 0, 0, 0, 3];
    const PM: [u8; 6] = [2, 0, 0, 0, 0, 2];
    fn session(last: char, address: Ipv4Addr, mac: [u8; 6]) -> Session {
        Session { domain: "n1qualification".into(), session: format!("00000000-0000-4000-8000-00000000000{last}"),
            generation: format!("10000000-0000-4000-8000-00000000000{last}"), backend: format!("owned-{last}"), address, mac }
    }
    fn watch() -> Watch { Watch::new(session('3', C, CM), session('2', P, PM), G, 100, 110).unwrap() }
    fn arp(src: Ipv4Addr, dst: Ipv4Addr, mac: [u8; 6], op: u8) -> Vec<u8> {
        let mut f = vec![0; 42];
        f[..6].copy_from_slice(if op == 1 { &[255; 6] } else { &CM });
        f[6..12].copy_from_slice(&mac); f[12..22].copy_from_slice(&[8, 6, 0, 1, 8, 0, 6, 4, 0, op]);
        f[22..28].copy_from_slice(&mac); f[28..32].copy_from_slice(&src.octets());
        f[38..42].copy_from_slice(&dst.octets()); f
    }
    fn trusted_ack() -> Vec<u8> {
        // BOOTP reply, Ethernet hardware, client MAC, assigned C; DHCP ACK,
        // 600-second lease, exact server G, end. IPv4 UDP zero checksum is legal.
        let mut data = vec![0; 240];
        data[..3].copy_from_slice(&[2, 1, 6]);
        data[16..20].copy_from_slice(&C.octets()); data[28..34].copy_from_slice(&CM);
        data[236..240].copy_from_slice(&[99, 130, 83, 99]);
        data.extend([53, 1, 5, 51, 4, 0, 0, 2, 88, 54, 4, 192, 168, 64, 1, 255]);
        let mut f = vec![0; 42 + data.len()];
        f[..6].copy_from_slice(&CM); f[6..12].copy_from_slice(&[2, 0, 0, 0, 0, 1]);
        f[12..14].copy_from_slice(&[8, 0]); f[14] = 0x45;
        f[16..18].copy_from_slice(&((28 + data.len()) as u16).to_be_bytes());
        f[22] = 64; f[23] = 17; f[26..30].copy_from_slice(&G.octets());
        f[30..34].copy_from_slice(&[255; 4]); f[34..38].copy_from_slice(&[0, 67, 0, 68]);
        f[38..40].copy_from_slice(&((8 + data.len()) as u16).to_be_bytes()); f[42..].copy_from_slice(&data);
        let mut sum: u32 = f[14..34].chunks_exact(2).map(|p| u32::from(u16::from_be_bytes([p[0], p[1]]))).sum();
        while sum > 65535 { sum = (sum & 65535) + (sum >> 16); }
        f[24..26].copy_from_slice(&(!(sum as u16)).to_be_bytes()); f
    }
    #[test]
    fn rejects_unbound_pair_and_unbounded_watch() {
        let c = session('3', C, CM); let p = session('2', P, PM);
        assert!(Watch::new(c.clone(), c.clone(), G, 100, 110).is_err());
        assert!(Watch::new(c.clone(), p.clone(), G, 100, 131).is_err());
        assert!(Watch::new(c.clone(), p.clone(), G, 100, 100).is_err());
        let mut bad = c.clone(); bad.generation = "stale-or-unbound".into();
        assert!(Watch::new(bad, p.clone(), G, 100, 110).is_err());
        let mut bad = p.clone(); bad.domain = "work".into();
        assert!(Watch::new(c.clone(), bad, G, 100, 110).is_err());
        assert!(Watch::new(c, p, G, 100, 110).is_ok());
    }
    #[test]
    fn exact_peer_and_window_only_no_payload_retention() {
        let w = watch(); let mut o = Observer::new(w.clone());
        let frame = arp(C, P, CM, 1);
        o.guest(&frame, 99, GuestOutcome::Written);
        o.guest(&frame, 110, GuestOutcome::Written);
        o.guest(&arp(C, G, CM, 1), 101, GuestOutcome::Written);
        o.guest(&arp(C, P, PM, 1), 101, GuestOutcome::Written);
        o.guest(&frame[..41], 101, GuestOutcome::Written);
        o.guest(&frame, 101, GuestOutcome::PeerTargetDenied);
        let mut padded = frame.clone(); padded.resize(60, 0xa5);
        o.guest(&padded, 102, GuestOutcome::PeerTargetDenied);
        o.guest(&arp(C, P, CM, 2), 103, GuestOutcome::OtherDenied);
        let s = o.snapshot(110); s.validate(&w).unwrap();
        assert_eq!((s.counts.received, s.counts.requests, s.counts.replies), (3, 2, 1));
        assert_eq!((s.counts.peer_target_denied, s.counts.other_denied, s.counts.write_attempt), (2, 1, 0));
    }
    #[test]
    fn distinguishes_refresh_vmnet_and_return_delivery_failures() {
        let w = watch(); let mut o = Observer::new(w.clone()); let f = arp(C, P, CM, 1);
        for result in [GuestOutcome::RefreshError, GuestOutcome::Written, GuestOutcome::WriteError] { o.guest(&f, 101, result); }
        let r = arp(P, C, PM, 2);
        for result in [ReturnOutcome::Delivered, ReturnOutcome::WriteError, ReturnOutcome::Enobufs] { o.returned(&r, 102, result); }
        o.returned(&arp(G, C, PM, 2), 102, ReturnOutcome::Delivered);
        let s = o.snapshot(110); s.validate(&w).unwrap();
        assert_eq!((s.counts.refresh_error, s.counts.write_attempt, s.counts.write_success, s.counts.write_error), (1, 2, 1, 1));
        assert_eq!((s.counts.return_reply, s.counts.return_delivered, s.counts.return_error, s.counts.return_enobufs), (3, 1, 1, 1));
    }
    #[test]
    fn contradictory_incomplete_or_overflowed_snapshot_is_not_evidence() {
        let w = watch(); let mut o = Observer::new(w.clone()); let f = arp(C, P, CM, 1);
        o.guest(&f, 101, GuestOutcome::PeerTargetDenied);
        assert!(o.snapshot(109).validate(&w).is_err());
        let s = o.snapshot(110);
        for bad in [Counts { write_attempt: 1, ..s.counts }, Counts { received: 2, ..s.counts }, Counts { write_success: 1, ..s.counts }] {
            assert!(Snapshot { counts: bad, ..s.clone() }.validate(&w).is_err());
        }
        let mut foreign = w.clone(); foreign.peer = session('4', P, PM);
        assert!(s.validate(&foreign).is_err());
        for _ in 0..4096 { o.guest(&f, 101, GuestOutcome::PeerTargetDenied); }
        let s = o.snapshot(110); assert!(s.overflow); assert!(s.validate(&w).is_err());
        assert_eq!(s.counts.received, 4096);
    }
    #[test]
    fn observation_does_not_change_actual_policy_or_call_write_for_peer_denial() {
        let w = watch(); let mut o = Observer::new(w.clone());
        let f = arp(C, P, CM, 1);
        // No lease: the real pinned parser denies. The observer must retain this
        // as OtherDenied rather than falsely attributing it to the peer ceiling.
        let mut original = policy::Policy::new(CM, G); let mut observed = policy::Policy::new(CM, G);
        let fallback_calls = Cell::new(0); let write_calls = Cell::new(0);
        let baseline = original.forward_with_refresh(&f, 101, || Ok::<_, &str>((vec![G], vec![])), |_| panic!("ARP must not call fallback"), |_| panic!("denied ARP must not write"));
        let result = observed.forward_with_refresh(&f, 101, || Ok::<_, &str>((vec![G], vec![])), |_| { fallback_calls.set(fallback_calls.get()+1); false }, |_| { write_calls.set(write_calls.get()+1); Ok(()) });
        o.guest(&f, 101, GuestOutcome::OtherDenied);
        assert_eq!(result, baseline); assert_eq!(result, Ok(false));
        assert_eq!((fallback_calls.get(), write_calls.get()), (0, 0));
        assert_eq!(o.snapshot(110).counts.other_denied, 1);
        let failed = observed.forward_with_refresh(&f, 101, || Err::<(Vec<Ipv4Addr>, Vec<Ipv4Addr>), _>("refresh failed"), |_| panic!("no fallback on refresh error"), |_| panic!("no write on refresh error"));
        o.guest(&f, 102, GuestOutcome::RefreshError);
        assert_eq!(failed, Err("refresh failed")); o.snapshot(110).validate(&w).unwrap();
    }
    #[test]
    fn leased_peer_request_is_actually_denied_without_fallback_or_write() {
        let w = watch(); let mut o = Observer::new(w.clone());
        let mut p = policy::Policy::new(CM, G);
        p.host(&trusted_ack(), &[G], 100);
        // Establish lease admission independently: the otherwise identical
        // gateway ARP is permitted by the real parser.
        assert_eq!(p.guest(&arp(C, G, CM, 1), &[G], 101), policy::Decision::Allow);
        let f = arp(C, P, CM, 1);
        let result = p.forward_with_refresh(&f, 101, || Ok::<_, &str>((vec![G], vec![])),
            |_| panic!("peer ARP never falls back"), |_| panic!("peer ARP never writes"));
        assert_eq!(result, Ok(false));
        // Future wiring must obtain this reason from the actual validated
        // target-denial branch; this fixture does not implement that hook.
        o.guest(&f, 101, GuestOutcome::PeerTargetDenied);
        let s = o.snapshot(110); s.validate(&w).unwrap();
        assert_eq!((s.counts.peer_target_denied, s.counts.write_attempt), (1, 0));
    }
    #[test]
    fn actual_allowed_dispatch_preserves_exact_bytes_and_writer_error() {
        let w = watch(); let mut o = Observer::new(w.clone()); let f = arp(C, P, CM, 1);
        let mut p = policy::Policy::new(CM, G); p.host(&trusted_ack(), &[G], 100);
        // Deliberately classify P as host-local in this deterministic input:
        // actual N1 allows ARP, proving the observer never dictates denial.
        let calls = Cell::new(0);
        let result = p.forward_with_refresh(&f, 101, || Ok::<_, &str>((vec![G, P], vec![])),
            |_| panic!("allowed ARP never falls back"), |bytes| {
                calls.set(calls.get()+1); assert_eq!(bytes, f.as_slice()); Err("writer failed")
            });
        o.guest(&f, 101, GuestOutcome::WriteError);
        assert_eq!(result, Err("writer failed")); assert_eq!(calls.get(), 1);
        let s = o.snapshot(110); s.validate(&w).unwrap();
        assert_eq!((s.counts.peer_target_denied, s.counts.write_attempt, s.counts.write_error), (0, 1, 1));
    }
    #[test]
    fn ingress_without_completed_dispatch_is_incomplete_even_after_deadline() {
        let w = watch(); let mut o = Observer::new(w.clone());
        let ticket = o.begin_guest(&arp(C, P, CM, 1), 101).unwrap();
        assert!(o.snapshot(110).validate(&w).is_err());
        o.complete_guest(ticket, GuestOutcome::PeerTargetDenied);
        o.snapshot(110).validate(&w).unwrap();
        assert_eq!((o.snapshot(110).counts.received, o.snapshot(110).counts.peer_target_denied), (1, 1));
        let ticket = o.begin_return(&arp(P, C, PM, 2), 102).unwrap();
        assert!(o.snapshot(110).validate(&w).is_err());
        o.complete_return(ticket, ReturnOutcome::Enobufs);
        o.snapshot(110).validate(&w).unwrap();
        assert_eq!(o.snapshot(110).counts.return_enobufs, 1);
    }
}

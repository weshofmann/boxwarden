//! Frozen Task3b R1 wire catalogue. Direct exact serde structs preserve duplicates.
use serde::{
    Deserialize, Serialize
};
pub const VERSION: &str = "0.19.0-boxwarden-n1-diagnostic.1";
pub const MAX_FRAME: usize = 4096;
pub const MAX_OUTPUT: usize = 16384;
#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct Selector {
    pub generation: String, pub nonce: String
}
impl Selector {
    pub fn parse(s: &str) -> Option<Self> {
        let (generation, nonce) = s.strip_prefix("@boxwarden-n1-diagnostic:")?.split_once(':')?;
        if !uuid(generation) || !uuid(nonce) {
            return None;
        }
        Some(Self {
            generation: generation.into(), nonce: nonce.into()
        })
    }
}
pub fn uuid(s: &str) -> bool {
    s.len() == 36 && s.bytes().enumerate().all(|(i,b)| if [8,13,18,23].contains(&i) {
        b == b'-'
    } else {
        b.is_ascii_digit() || (b'a'..=b'f').contains(&b)
    })
    && s.bytes().any(|b| b != b'0' && b != b'-')
}
pub fn private(a: [u8;4]) -> bool {
    a[0] == 10 || (a[0] == 172 && (16..=31).contains(&a[1])) || (a[0] == 192 && a[1] == 168)
}
pub fn mac(a: [u8;6]) -> bool {
    a != [0;     6] && a[0] & 1 == 0
}
#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct SessionBinding {
    pub domain: String, pub session_id: String, pub generation: String,
    pub backend_kind: String, pub backend_object: String, pub address: [u8;     4], pub mac: [u8;     6],
}
impl SessionBinding {
    pub fn valid(&self) -> bool {
        self.domain == "n1qualification" && uuid(&self.session_id) && uuid(&self.generation)
        && self.backend_kind == "tart" && (1..=128).contains(&self.backend_object.len())
        && self.backend_object.bytes().all(|b| b.is_ascii_alphanumeric() || b == b'_' || b == b'-')
        && private(self.address) && mac(self.mac)
    }
}
#[derive(Clone, Debug, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct Hello {
    pub version: u8, pub kind: String, pub generation: String, pub nonce: String, pub candidate_mac: [u8;     6], pub gateway: [u8;     4]
}
#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct Arm {
    pub version: u8, pub kind: String, pub generation: String, pub nonce: String,
    pub operation_id: String, pub candidate: SessionBinding, pub control: SessionBinding,
    pub gateway: [u8;     4], pub duration_ms: u32, pub control_provenance: String,
}
impl Arm {
    pub fn valid(&self, s: &Selector, own_mac: [u8;6], gateway: [u8;4]) -> bool {
        self.version == 1 && self.kind == "ARM" && self.generation == s.generation && self.nonce == s.nonce
        && uuid(&self.operation_id) && self.candidate.valid() && self.control.valid()
        && self.candidate.generation == s.generation && self.candidate.generation != self.control.generation
        && self.candidate.session_id != self.control.session_id && self.candidate.backend_object != self.control.backend_object
        && self.candidate.address != self.control.address && self.candidate.mac != self.control.mac
        && self.candidate.mac == own_mac && self.gateway == gateway && private(gateway)
        && self.candidate.address != gateway && self.control.address != gateway
        && (1000..=30000).contains(&self.duration_ms) && self.control_provenance == "host_backend_pinned_owner"
    }
}
#[derive(Clone, Debug, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct Armed {
    pub version: u8, pub kind: String, pub generation: String, pub nonce: String,
    pub operation_id: String, pub candidate: SessionBinding, pub control: SessionBinding,
    pub gateway: [u8;     4], pub duration_ms: u32, pub control_provenance: String,
    pub armed_offset_ns: u64, pub candidate_lease_valid: bool,
}
#[derive(Clone, Debug, Default, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct Counters {
    pub vm_dispatch_started: u16, pub vm_dispatch_completed: u16,
    pub host_dispatch_started: u16, pub host_dispatch_completed: u16,
    pub vm_identified: [u16;     3], pub vm_outcomes: [[u16;     6];     3], pub vm_policy_decisions: [u16;     3],
    pub vm_arp_evaluation: [u16;     5], pub vm_fallback_results: [u16;     2], pub vm_write_attempts: [u16;     3],
    pub vm_write_bytes: [[u64;     2];     3], pub vm_write_errors: [u16;     15],
    pub host_reply_class: [u16;     6], pub host_class_outcomes: [[u16;     6];     6], pub host_write_attempts: [u16;     6],
    pub host_write_bytes: [[u64;     2];     6], pub host_write_errors: [u16;     15],
    pub recognized_unsupported_pair: [u16;     2], pub recognized_truncated_pair: [u16;     2],
    pub refresh_results: [[u16;     3];     2], pub vm_lease_state: [[u16;     5];     3],
    pub vm_target_predicates: [[[u16;     3];     2];     3], pub host_pair_arp_request: u16,
}
#[derive(Clone, Debug, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct Summary {
    pub version: u8, pub kind: String, pub generation: String, pub nonce: String,
    pub operation_id: String, pub candidate: SessionBinding, pub control: SessionBinding,
    pub gateway: [u8;     4], pub duration_ms: u32, pub control_provenance: String,
    pub armed_offset_ns: u64, pub end_offset_ns: u64, pub candidate_lease_valid: bool,
    pub coverage_scope: String, pub zero_count_attribution: bool, pub packet_count_unobserved: bool,
    pub complete: bool, pub overflow: bool, pub loss: bool, pub invalid_flags: [bool;     14], pub counters: Counters,
}
/// Lexical restrictions only. JSON grammar and exact-field/type decoding belong to serde.
/// None of the admitted identities need escapes; integer spelling must be canonical.
pub fn decode<T: for<'de> Deserialize<'de>>(bytes: &[u8]) -> Result<T, ()> {
    if bytes.is_empty() || bytes.len() > MAX_FRAME || !bytes.is_ascii() {
        return Err(());
    }
    let mut quoted = false;
    let mut i = 0;
    while i < bytes.len() {
        let b = bytes[i];
        if b == b'"' {
            quoted = !quoted;
            i += 1;
            continue;
        }
        if b == b'\\' || (quoted && b < 32) {
            return Err(());
        }
        if !quoted && b == b'-' {
            return Err(());
        }
        if !quoted && b.is_ascii_digit() {
            let start = i;
            while i < bytes.len() && bytes[i].is_ascii_digit() {
                i += 1;
            }
            if (i-start > 1 && bytes[start] == b'0') || bytes.get(i).is_some_and(|b| [b'.',b'e',b'E'].contains(b)) {
                return Err(());
            }
            continue;
        }
        i += 1;
    }
    let mut d = serde_json::Deserializer::from_slice(bytes);
    let v = T::deserialize(&mut d).map_err(|_| ())?;
    d.end().map_err(|_| ())?;
    Ok(v)
}
pub fn frame<T: Serialize>(value: &T) -> Result<Vec<u8>, ()> {
    let p = serde_json::to_vec(value).map_err(|_| ())?;
    if p.is_empty() || p.len() > MAX_FRAME {
        return Err(());
    }
    let mut v = (p.len() as u32).to_be_bytes().to_vec();
    v.extend(p);
    Ok(v)
}

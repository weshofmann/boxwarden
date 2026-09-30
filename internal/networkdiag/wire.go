//go:build (n1diagnostic || n1clipboarddiagnostic) && !n1candidate

// Package networkdiag contains only bounded public diagnostic metadata. It is
// outside the generic guest helper graph and has no packet/credential fields.
package networkdiag

import (
	"errors"
	"regexp"
)

const MaxFrame = 4096
const MaxChildOutput = 16384

var ErrMetadata = errors.New("diagnostic network metadata refused")
var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
var objectPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

func UUID(s string) bool {
	return uuidPattern.MatchString(s) && s != "00000000-0000-0000-0000-000000000000"
}
func Private(a [4]uint8) bool {
	return a[0] == 10 || a[0] == 172 && a[1] >= 16 && a[1] <= 31 || a[0] == 192 && a[1] == 168
}
func MAC(a [6]uint8) bool { return a != [6]uint8{} && a[0]&1 == 0 }

type Binding struct {
	Domain        string   `json:"domain"`
	SessionID     string   `json:"session_id"`
	Generation    string   `json:"generation"`
	BackendKind   string   `json:"backend_kind"`
	BackendObject string   `json:"backend_object"`
	Address       [4]uint8 `json:"address"`
	MAC           [6]uint8 `json:"mac"`
}
type Inspection struct {
	Binding        Binding `json:"binding"`
	PinFingerprint string  `json:"pin_fingerprint"`
	ConfigSHA256   string  `json:"config_sha256"`
	ObservedUnixNS uint64  `json:"observed_unix_ns"`
}

func (i Inspection) Valid() bool {
	return i.Binding.Valid() && regexp.MustCompile(`^SHA256:[A-Za-z0-9+/]{43}$`).MatchString(i.PinFingerprint) && regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(i.ConfigSHA256) && i.ObservedUnixNS > 0
}
func (b Binding) Valid() bool {
	return b.Domain == "n1qualification" && UUID(b.SessionID) && UUID(b.Generation) && b.BackendKind == "tart" && objectPattern.MatchString(b.BackendObject) && Private(b.Address) && MAC(b.MAC)
}

type Hello struct {
	Version      uint8    `json:"version"`
	Kind         string   `json:"kind"`
	Generation   string   `json:"generation"`
	Nonce        string   `json:"nonce"`
	CandidateMAC [6]uint8 `json:"candidate_mac"`
	Gateway      [4]uint8 `json:"gateway"`
}
type Arm struct {
	Version           uint8    `json:"version"`
	Kind              string   `json:"kind"`
	Generation        string   `json:"generation"`
	Nonce             string   `json:"nonce"`
	OperationID       string   `json:"operation_id"`
	Candidate         Binding  `json:"candidate"`
	Control           Binding  `json:"control"`
	Gateway           [4]uint8 `json:"gateway"`
	DurationMS        uint32   `json:"duration_ms"`
	ControlProvenance string   `json:"control_provenance"`
}

func (a Arm) Valid(generation, nonce string, mac [6]uint8, gateway [4]uint8) bool {
	return UUID(generation) && UUID(nonce) && a.Version == 1 && a.Kind == "ARM" && a.Generation == generation && a.Nonce == nonce && UUID(a.OperationID) && a.Candidate.Valid() && a.Control.Valid() && a.Candidate.Generation == generation && a.Candidate.Generation != a.Control.Generation && a.Candidate.SessionID != a.Control.SessionID && a.Candidate.BackendObject != a.Control.BackendObject && a.Candidate.Address != a.Control.Address && a.Candidate.MAC != a.Control.MAC && a.Candidate.MAC == mac && a.Gateway == gateway && Private(gateway) && a.Candidate.Address != gateway && a.Control.Address != gateway && a.DurationMS >= 1000 && a.DurationMS <= 30000 && a.ControlProvenance == "host_backend_pinned_owner"
}

type Armed struct {
	Version             uint8    `json:"version"`
	Kind                string   `json:"kind"`
	Generation          string   `json:"generation"`
	Nonce               string   `json:"nonce"`
	OperationID         string   `json:"operation_id"`
	Candidate           Binding  `json:"candidate"`
	Control             Binding  `json:"control"`
	Gateway             [4]uint8 `json:"gateway"`
	DurationMS          uint32   `json:"duration_ms"`
	ControlProvenance   string   `json:"control_provenance"`
	ArmedOffsetNS       uint64   `json:"armed_offset_ns"`
	CandidateLeaseValid bool     `json:"candidate_lease_valid"`
}
type Counters struct {
	VMDispatchStarted         uint16          `json:"vm_dispatch_started"`
	VMDispatchCompleted       uint16          `json:"vm_dispatch_completed"`
	HostDispatchStarted       uint16          `json:"host_dispatch_started"`
	HostDispatchCompleted     uint16          `json:"host_dispatch_completed"`
	VMIdentified              [3]uint16       `json:"vm_identified"`
	VMOutcomes                [3][6]uint16    `json:"vm_outcomes"`
	VMPolicyDecisions         [3]uint16       `json:"vm_policy_decisions"`
	VMARPEvaluation           [5]uint16       `json:"vm_arp_evaluation"`
	VMFallbackResults         [2]uint16       `json:"vm_fallback_results"`
	VMWriteAttempts           [3]uint16       `json:"vm_write_attempts"`
	VMWriteBytes              [3][2]uint64    `json:"vm_write_bytes"`
	VMWriteErrors             [15]uint16      `json:"vm_write_errors"`
	HostReplyClass            [6]uint16       `json:"host_reply_class"`
	HostClassOutcomes         [6][6]uint16    `json:"host_class_outcomes"`
	HostWriteAttempts         [6]uint16       `json:"host_write_attempts"`
	HostWriteBytes            [6][2]uint64    `json:"host_write_bytes"`
	HostWriteErrors           [15]uint16      `json:"host_write_errors"`
	RecognizedUnsupportedPair [2]uint16       `json:"recognized_unsupported_pair"`
	RecognizedTruncatedPair   [2]uint16       `json:"recognized_truncated_pair"`
	RefreshResults            [2][3]uint16    `json:"refresh_results"`
	VMLeaseState              [3][5]uint16    `json:"vm_lease_state"`
	VMTargetPredicates        [3][2][3]uint16 `json:"vm_target_predicates"`
	HostPairARPRequest        uint16          `json:"host_pair_arp_request"`
}
type Summary struct {
	Version               uint8    `json:"version"`
	Kind                  string   `json:"kind"`
	Generation            string   `json:"generation"`
	Nonce                 string   `json:"nonce"`
	OperationID           string   `json:"operation_id"`
	Candidate             Binding  `json:"candidate"`
	Control               Binding  `json:"control"`
	Gateway               [4]uint8 `json:"gateway"`
	DurationMS            uint32   `json:"duration_ms"`
	ControlProvenance     string   `json:"control_provenance"`
	ArmedOffsetNS         uint64   `json:"armed_offset_ns"`
	EndOffsetNS           uint64   `json:"end_offset_ns"`
	CandidateLeaseValid   bool     `json:"candidate_lease_valid"`
	CoverageScope         string   `json:"coverage_scope"`
	ZeroCountAttribution  bool     `json:"zero_count_attribution"`
	PacketCountUnobserved bool     `json:"packet_count_unobserved"`
	Complete              bool     `json:"complete"`
	Overflow              bool     `json:"overflow"`
	Loss                  bool     `json:"loss"`
	InvalidFlags          [14]bool `json:"invalid_flags"`
	Counters              Counters `json:"counters"`
}

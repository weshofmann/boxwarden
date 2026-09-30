//go:build n1clipboarddiagnostic

package clipboarddiag

import (
	"bytes"
	"encoding/json"
	"regexp"
	"slices"
)

const MaxOverlayBytes = 14260

var hexPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

type Closure struct {
	Version      int    `json:"version"`
	HeaderDigest string `json:"header_digest"`
	Direction    string `json:"direction"`
	ElapsedMS    int64  `json:"elapsed_ms"`
}

func (c Closure) Validate(o Operation) error {
	if c.Version != 1 || c.HeaderDigest != HeaderDigest(o) || c.Direction != o.Direction || c.ElapsedMS < 0 || c.ElapsedMS >= 60000 {
		return ErrMetadata
	}
	return nil
}
func EncodeClosure(c Closure) ([]byte, error) {
	if c.Version != 1 || !hexPattern.MatchString(c.HeaderDigest) || (c.Direction != "read" && c.Direction != "write") || c.ElapsedMS < 0 || c.ElapsedMS >= 60000 {
		return nil, ErrMetadata
	}
	raw, err := json.Marshal(map[string]any{"version": c.Version, "header_digest": c.HeaderDigest, "direction": c.Direction, "elapsed_ms": c.ElapsedMS})
	return append(raw, '\n'), err
}
func DecodeClosure(raw []byte, o Operation) (Closure, error) {
	var c Closure
	if StrictDecode(raw, &c, 512) != nil || c.Validate(o) != nil {
		return Closure{}, ErrMetadata
	}
	want, _ := EncodeClosure(c)
	if !bytes.Equal(raw, want) {
		return Closure{}, ErrMetadata
	}
	return c, nil
}

type OverlayRecord struct {
	Event        string  `json:"event"`
	AtMS         int64   `json:"at_ms"`
	HeaderDigest string  `json:"header_digest"`
	Lane         string  `json:"lane"`
	Digest       *string `json:"digest,omitempty"`
	Value        *uint64 `json:"value,omitempty"`
}
type NativeOwner struct {
	PID        uint32 `json:"pid"`
	StartTicks uint64 `json:"start_ticks"`
	UID        uint32 `json:"uid"`
	State      string `json:"state"`
}
type OverlayReceipt struct {
	Version       int             `json:"version"`
	Binding       Operation       `json:"binding"`
	HeaderDigest  string          `json:"header_digest"`
	CoverageScope string          `json:"coverage_scope"`
	Complete      bool            `json:"complete"`
	OwnerCode     string          `json:"owner_code"`
	Owner         *NativeOwner    `json:"owner"`
	Records       []OverlayRecord `json:"records"`
	Closure       *Closure        `json:"closure"`
}

var overlayEvents = map[string]bool{}

func init() {
	for _, e := range []string{"session_init", "session_ready", "session_failed", "session_check", "session_check_ok", "session_check_failed", "binding", "owner_pid", "owner_start", "claim_begin", "claim_ok", "claim_refused", "claim_failed", "owner_get", "owner_clear", "retain_begin", "retain_return", "retain_failed", "read_begin", "read_request", "read_complete", "read_failed", "text_absent", "text_invalid", "metadata_owner", "metadata_targets", "metadata_unavailable", "native_read_absent", "native_read_invalid", "native_read_complete", "native_read_failed", "trace_saturated", "frame_ok", "frame_error", "frame_unknown", "frame_failed", "on_claim_ok", "ack_ok", "ack_error", "ack_unknown", "ack_failed", "owner_checkpoint", "trace_begin", "trace_end", "trace_incomplete", "input_begin"} {
		overlayEvents[e] = true
	}
}
func (r OverlayReceipt) Validate(o Operation) error {
	if r.Version != 1 || r.Binding != o || r.HeaderDigest != HeaderDigest(o) || r.CoverageScope != "collected_progression_prefix" || r.Records == nil || len(r.Records) > 64 {
		return ErrMetadata
	}
	if r.OwnerCode != "not_applicable" && r.OwnerCode != "live" && r.OwnerCode != "unverifiable" && r.OwnerCode != "retention_ended" {
		return ErrMetadata
	}
	if r.Owner != nil && (r.Owner.PID == 0 || r.Owner.StartTicks == 0 || r.Owner.UID != 1000 || r.Owner.State != "live") {
		return ErrMetadata
	}
	if r.OwnerCode == "live" && r.Owner == nil {
		return ErrMetadata
	}
	if (r.OwnerCode == "not_applicable" || r.OwnerCode == "unverifiable") && r.Owner != nil {
		return ErrMetadata
	}
	var lastParent, lastOwner int64
	parentCount, ownerCount := 0, 0
	for _, e := range r.Records {
		if !overlayEvents[e.Event] || e.HeaderDigest != r.HeaderDigest || e.AtMS < 0 || e.AtMS >= 60000 {
			return ErrMetadata
		}
		switch e.Lane {
		case "parent":
			if e.AtMS < lastParent {
				return ErrMetadata
			}
			lastParent = e.AtMS
			parentCount++
		case "owner":
			if e.AtMS < lastOwner {
				return ErrMetadata
			}
			lastOwner = e.AtMS
			ownerCount++
		default:
			return ErrMetadata
		}
		wantsValue := e.Event == "owner_pid" || e.Event == "owner_start" || e.Event == "metadata_owner" || e.Event == "metadata_targets"
		if (e.Value != nil) != wantsValue || (e.Digest != nil) != (e.Event == "binding") {
			return ErrMetadata
		}
		if e.Digest != nil && !hexPattern.MatchString(*e.Digest) {
			return ErrMetadata
		}
		if e.Value != nil {
			v := *e.Value
			if (e.Event != "owner_start" && v > 0xffffffff) || ((e.Event == "owner_pid" || e.Event == "owner_start") && v == 0) || (e.Event == "metadata_targets" && v > 1) {
				return ErrMetadata
			}
		}
	}
	if parentCount > 32 || ownerCount > 32 {
		return ErrMetadata
	}
	raw, _ := json.Marshal(r)
	if len(raw)+1 > MaxOverlayBytes {
		return ErrMetadata
	}
	if r.Closure != nil && r.Closure.Validate(o) != nil {
		return ErrMetadata
	}
	if r.Complete && (!r.progression() || r.Closure == nil) {
		return ErrMetadata
	}
	if r.Complete {
		for _, e := range r.Records {
			if e.Lane == "parent" && e.Event == "trace_end" && e.AtMS > r.Closure.ElapsedMS {
				return ErrMetadata
			}
		}
		if o.Direction == "write" && r.OwnerCode != "live" {
			return ErrMetadata
		}
		if o.Direction == "read" && (r.OwnerCode != "not_applicable" || r.Owner != nil) {
			return ErrMetadata
		}
	}
	return nil
}
func (r OverlayReceipt) progression() bool {
	parent, owner := []string{}, []string{}
	claimSeen := false
	var pid, ticks uint64
	native := 0
	for _, e := range r.Records {
		if e.Lane == "parent" {
			parent = append(parent, e.Event)
			continue
		}
		if e.Event == "owner_get" {
			if !claimSeen {
				return false
			}
			continue
		}
		if e.Event == "claim_begin" {
			claimSeen = true
		}
		owner = append(owner, e.Event)
		if e.Event == "owner_pid" {
			pid = *e.Value
		}
		if e.Event == "owner_start" {
			ticks = *e.Value
		}
		if e.Event == "metadata_owner" && *e.Value != 0 {
			native++
		}
	}
	if r.Binding.Direction == "read" {
		return len(owner) == 0 && slices.Equal(parent, []string{"trace_begin", "session_init", "binding", "session_ready", "read_begin", "metadata_owner", "metadata_targets", "read_request", "session_check", "session_check_ok", "native_read_complete", "session_check", "session_check_ok", "read_complete", "frame_ok", "trace_end"})
	}
	if !slices.Equal(parent, []string{"trace_begin", "input_begin", "session_init", "binding", "session_ready", "frame_ok", "trace_end"}) {
		return false
	}
	required := []string{"session_check", "session_check_ok", "owner_pid", "owner_start", "claim_begin", "claim_ok", "on_claim_ok", "session_check", "session_check_ok", "ack_ok", "retain_begin", "metadata_owner", "owner_checkpoint"}
	if len(owner) < len(required) || !slices.Equal(owner[:len(required)], required) || native != 1 || r.Owner == nil || uint64(r.Owner.PID) != pid || r.Owner.StartTicks != ticks {
		return false
	}
	tail := owner[len(required):]
	if len(tail)%2 != 0 {
		return false
	}
	for i, e := range tail {
		if e != []string{"session_check", "session_check_ok"}[i%2] {
			return false
		}
	}
	return true
}
func DecodeOverlay(raw []byte, o Operation) (OverlayReceipt, error) {
	var r OverlayReceipt
	if StrictDecode(raw, &r, MaxOverlayBytes) != nil || r.Validate(o) != nil {
		return OverlayReceipt{}, ErrMetadata
	}
	return r, nil
}

type SyntheticResult struct {
	FixtureID      string `json:"fixture_id"`
	ExpectedSHA256 string `json:"expected_sha256"`
	Length         int    `json:"length"`
	Equal          bool   `json:"equal"`
}

func (s SyntheticResult) Validate() error {
	if s.FixtureID != "n1_clipboard_control_v1" || s.ExpectedSHA256 != "9096af926f38bc69facaa4d383ba789f106a6506fadf6cedd170616b882f88e7" || s.Length < 0 || s.Length > 1048576 {
		return ErrMetadata
	}
	return nil
}

type InvocationReceipt struct {
	Version   int             `json:"version"`
	Binding   Operation       `json:"binding"`
	Outcome   string          `json:"outcome"`
	Complete  bool            `json:"complete"`
	Fragments []Fragment      `json:"fragments"`
	Synthetic SyntheticResult `json:"synthetic"`
}

func (r InvocationReceipt) Validate() error {
	if r.Version != 1 || r.Binding.Validate(false) != nil || r.Synthetic.Validate() != nil || (r.Outcome != "unchanged" && r.Outcome != "committed" && r.Outcome != "unknown") || len(r.Fragments) > 2 {
		return ErrMetadata
	}
	for _, f := range r.Fragments {
		if f.Validate() != nil || f.Binding != r.Binding || f.Origin == "guest" {
			return ErrMetadata
		}
	}
	admitted, complete := MergeHost(r.Binding, r.Fragments...)
	if len(admitted) != len(r.Fragments) || (r.Complete && !complete) {
		return ErrMetadata
	}
	raw, _ := json.Marshal(r)
	if len(raw)+1 > MaxFragmentBytes {
		return ErrMetadata
	}
	return nil
}

type GuestCollection struct {
	Version   int             `json:"version"`
	Binding   Operation       `json:"binding"`
	Complete  bool            `json:"complete"`
	Bootstrap *Fragment       `json:"bootstrap"`
	Overlay   *OverlayReceipt `json:"overlay"`
}

func (g GuestCollection) Validate(o Operation) error {
	if g.Version != 1 || g.Binding != o {
		return ErrMetadata
	}
	if g.Bootstrap != nil && (g.Bootstrap.Validate() != nil || g.Bootstrap.Binding != o || g.Bootstrap.Origin != "guest") {
		return ErrMetadata
	}
	if g.Overlay != nil && g.Overlay.Validate(o) != nil {
		return ErrMetadata
	}
	if g.Complete && (g.Bootstrap == nil || !g.Bootstrap.Complete || g.Overlay == nil || !g.Overlay.Complete) {
		return ErrMetadata
	}
	raw, _ := json.Marshal(g)
	if len(raw)+1 > MaxCollectionBytes {
		return ErrMetadata
	}
	return nil
}

// CollectionReceipt.Complete describes admitted bounded metadata coverage plus
// the frozen native successful progression prefix. It does not establish caller
// acknowledgement, transfer outcome, fixture equality or owner lifetime. An
// invocation may remain unknown while its failure-layer metadata is complete.
type CollectionReceipt struct {
	Version   int             `json:"version"`
	Binding   Operation       `json:"binding"`
	Complete  bool            `json:"complete"`
	Fragments []Fragment      `json:"fragments"`
	Overlay   *OverlayReceipt `json:"overlay"`
}

func (r CollectionReceipt) Validate() error {
	if r.Version != 1 || r.Binding.Validate(false) != nil || len(r.Fragments) > 3 {
		return ErrMetadata
	}
	seen := map[string]bool{}
	var host []Fragment
	guestComplete := false
	for _, f := range r.Fragments {
		if f.Validate() != nil || f.Binding != r.Binding || seen[f.Origin] {
			return ErrMetadata
		}
		seen[f.Origin] = true
		if f.Origin == "guest" {
			guestComplete = f.Complete
		} else {
			host = append(host, f)
		}
	}
	admitted, hostComplete := MergeHost(r.Binding, host...)
	if len(admitted) != len(host) {
		return ErrMetadata
	}
	if r.Overlay != nil && r.Overlay.Validate(r.Binding) != nil {
		return ErrMetadata
	}
	if r.Complete && (!hostComplete || !guestComplete || r.Overlay == nil || !r.Overlay.Complete) {
		return ErrMetadata
	}
	raw, _ := json.Marshal(r)
	if len(raw)+1 > MaxCollectionBytes {
		return ErrMetadata
	}
	return nil
}

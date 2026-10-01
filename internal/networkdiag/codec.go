//go:build (n1diagnostic || n1clipboarddiagnostic) && !n1candidate

package networkdiag

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"io"
	"reflect"
	"strconv"
)

// JSON grammar is handled by encoding/json. Exact typed token traversal adds
// required/duplicate fields and exact array lengths that Unmarshal omits.
func Decode(raw []byte, out any) error {
	if len(raw) == 0 || len(raw) > MaxFrame {
		return ErrMetadata
	}
	quoted := false
	for i := 0; i < len(raw); i++ {
		b := raw[i]
		if b >= 128 || b == '\\' {
			return ErrMetadata
		}
		if b == '"' {
			quoted = !quoted
			continue
		}
		if quoted {
			if b < 32 {
				return ErrMetadata
			}
			continue
		}
		if b == '-' {
			return ErrMetadata
		}
		if b >= '0' && b <= '9' {
			start := i
			for i+1 < len(raw) && raw[i+1] >= '0' && raw[i+1] <= '9' {
				i++
			}
			if i > start && raw[start] == '0' {
				return ErrMetadata
			}
			if i+1 < len(raw) && (raw[i+1] == '.' || raw[i+1] == 'e' || raw[i+1] == 'E') {
				return ErrMetadata
			}
		}
	}
	v := reflect.ValueOf(out)
	if v.Kind() != reflect.Pointer || v.IsNil() {
		return ErrMetadata
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	tmp := reflect.New(v.Elem().Type()).Elem()
	if tokenValue(d, tmp) != nil {
		return ErrMetadata
	}
	if _, err := d.Token(); err != io.EOF {
		return ErrMetadata
	}
	v.Elem().Set(tmp)
	return nil
}
func tokenValue(d *json.Decoder, v reflect.Value) error {
	tok, err := d.Token()
	if err != nil {
		return ErrMetadata
	}
	switch v.Kind() {
	case reflect.Struct:
		if tok != json.Delim('{') {
			return ErrMetadata
		}
		fields := map[string]int{}
		for i := 0; i < v.NumField(); i++ {
			f := v.Type().Field(i)
			name := f.Tag.Get("json")
			if name == "" {
				name = f.Name
			}
			fields[name] = i
		}
		seen := map[string]bool{}
		for d.More() {
			key, err := d.Token()
			if err != nil {
				return ErrMetadata
			}
			name, ok := key.(string)
			i, exists := fields[name]
			if !ok || !exists || seen[name] {
				return ErrMetadata
			}
			seen[name] = true
			if tokenValue(d, v.Field(i)) != nil {
				return ErrMetadata
			}
		}
		end, err := d.Token()
		if err != nil || end != json.Delim('}') || len(seen) != len(fields) {
			return ErrMetadata
		}
	case reflect.Array:
		if tok != json.Delim('[') {
			return ErrMetadata
		}
		for i := 0; i < v.Len(); i++ {
			if !d.More() || tokenValue(d, v.Index(i)) != nil {
				return ErrMetadata
			}
		}
		if d.More() {
			return ErrMetadata
		}
		end, err := d.Token()
		if err != nil || end != json.Delim(']') {
			return ErrMetadata
		}
	case reflect.String:
		s, ok := tok.(string)
		if !ok {
			return ErrMetadata
		}
		v.SetString(s)
	case reflect.Bool:
		b, ok := tok.(bool)
		if !ok {
			return ErrMetadata
		}
		v.SetBool(b)
	case reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, ok := tok.(json.Number)
		if !ok {
			return ErrMetadata
		}
		value, err := strconv.ParseUint(string(n), 10, v.Type().Bits())
		if err != nil || v.Kind() == reflect.Uint16 && value > 4096 {
			return ErrMetadata
		}
		v.SetUint(value)
	default:
		return ErrMetadata
	}
	return nil
}
func Encode(value any) ([]byte, error) {
	raw, err := json.Marshal(value)
	if err != nil || len(raw) == 0 || len(raw) > MaxFrame {
		return nil, ErrMetadata
	}
	return raw, nil
}
func Frame(value any) ([]byte, error) {
	raw, err := Encode(value)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 4+len(raw))
	binary.BigEndian.PutUint32(out, uint32(len(raw)))
	copy(out[4:], raw)
	return out, nil
}
func ReadFrame(r io.Reader) ([]byte, error) {
	var prefix [4]byte
	if _, err := io.ReadFull(r, prefix[:]); err != nil {
		return nil, ErrMetadata
	}
	size := binary.BigEndian.Uint32(prefix[:])
	if size == 0 || size > MaxFrame {
		return nil, ErrMetadata
	}
	raw := make([]byte, size)
	if _, err := io.ReadFull(r, raw); err != nil {
		return nil, ErrMetadata
	}
	return raw, nil
}
func echo(a Arm, version uint8, kind, generation, nonce, operation string, candidate, control Binding, gateway [4]uint8, duration uint32, provenance string, expectedKind string) bool {
	return version == 1 && kind == expectedKind && generation == a.Generation && nonce == a.Nonce && operation == a.OperationID && candidate == a.Candidate && control == a.Control && gateway == a.Gateway && duration == a.DurationMS && provenance == a.ControlProvenance
}
func (r Armed) Matches(a Arm) bool {
	return echo(a, r.Version, r.Kind, r.Generation, r.Nonce, r.OperationID, r.Candidate, r.Control, r.Gateway, r.DurationMS, r.ControlProvenance, "ARMED") && r.CandidateLeaseValid
}
func sum(values []uint16) uint32 {
	var s uint32
	for _, n := range values {
		s += uint32(n)
	}
	return s
}
func (c Counters) Arithmetic() bool {
	if c.VMDispatchStarted != c.VMDispatchCompleted || c.HostDispatchStarted != c.HostDispatchCompleted || sum(c.RefreshResults[0][:]) != uint32(c.VMDispatchStarted) || sum(c.RefreshResults[1][:]) != uint32(c.HostDispatchStarted) {
		return false
	}
	for r := 0; r < 3; r++ {
		if sum(c.VMOutcomes[r][:]) != uint32(c.VMIdentified[r]) || sum(c.VMLeaseState[r][:]) != uint32(c.VMIdentified[r]) || uint32(c.VMWriteAttempts[r]) != sum(c.VMOutcomes[r][3:]) {
			return false
		}
		for col := 0; col < 2; col++ {
			if sum(c.VMTargetPredicates[r][col][:]) != uint32(c.VMIdentified[r]) {
				return false
			}
		}
	}
	for r := 0; r < 6; r++ {
		if sum(c.HostClassOutcomes[r][:]) != uint32(c.HostReplyClass[r]) || uint32(c.HostWriteAttempts[r]) != sum(c.HostClassOutcomes[r][2:]) {
			return false
		}
	}
	return true
}
func (r Summary) Matches(a Arm, armed Armed) bool {
	if r.Loss || r.Overflow {
		return false
	}
	for _, flag := range r.InvalidFlags {
		if flag {
			return false
		}
	}
	if !echo(a, r.Version, r.Kind, r.Generation, r.Nonce, r.OperationID, r.Candidate, r.Control, r.Gateway, r.DurationMS, r.ControlProvenance, "SUMMARY") || r.ArmedOffsetNS != armed.ArmedOffsetNS || r.EndOffsetNS < r.ArmedOffsetNS || r.CoverageScope != "identified_pair_headers" || r.ZeroCountAttribution || !r.PacketCountUnobserved || !r.Counters.Arithmetic() {
		return false
	}
	if r.Complete {
		// Frozen refresh_error and api_length_mismatch buckets contradict Complete,
		// even when the counters balance and the peer omits the loss flags.
		for _, direction := range r.Counters.RefreshResults {
			if direction[2] != 0 {
				return false
			}
		}
		for _, outcome := range r.Counters.VMOutcomes {
			if outcome[2] != 0 || outcome[4] != 0 {
				return false
			}
		}
		for _, outcome := range r.Counters.HostClassOutcomes {
			if outcome[1] != 0 || outcome[3] != 0 {
				return false
			}
		}
		if !r.CandidateLeaseValid || r.Overflow || r.Loss || r.EndOffsetNS-r.ArmedOffsetNS < uint64(r.DurationMS)*1000000 || r.EndOffsetNS-r.ArmedOffsetNS > uint64(r.DurationMS)*1000000+100000000 {
			return false
		}
		for _, v := range r.InvalidFlags {
			if v {
				return false
			}
		}
		for _, n := range r.Counters.RecognizedUnsupportedPair {
			if n != 0 {
				return false
			}
		}
		for _, n := range r.Counters.RecognizedTruncatedPair {
			if n != 0 {
				return false
			}
		}
	}
	return true
}
func (r Summary) ValidStandalone() bool {
	a := Arm{r.Version, "ARM", r.Generation, r.Nonce, r.OperationID, r.Candidate, r.Control, r.Gateway, r.DurationMS, r.ControlProvenance}
	armed := Armed{r.Version, "ARMED", r.Generation, r.Nonce, r.OperationID, r.Candidate, r.Control, r.Gateway, r.DurationMS, r.ControlProvenance, r.ArmedOffsetNS, true}
	return a.Valid(a.Generation, a.Nonce, a.Candidate.MAC, a.Gateway) && r.Matches(a, armed)
}

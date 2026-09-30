//go:build n1diagnostic && !n1candidate

package networkdiag

import (
	"math"
	"reflect"
	"strings"
	"testing"
)

func TestDiagnosticJointRustSerializedBounds(t *testing.T) {
	a := armFixture()
	a.Candidate.BackendObject = strings.Repeat("x", 128)
	a.Control.BackendObject = strings.Repeat("y", 128)
	a.DurationMS = 30000
	a.Candidate.Address = [4]uint8{192, 168, 255, 254}
	a.Control.Address = [4]uint8{192, 168, 255, 253}
	a.Gateway = [4]uint8{192, 168, 255, 252}
	a.Candidate.MAC = [6]uint8{254, 255, 255, 255, 255, 254}
	a.Control.MAC = [6]uint8{254, 255, 255, 255, 255, 253}
	if !a.Valid(a.Generation, a.Nonce, a.Candidate.MAC, a.Gateway) {
		t.Fatal("maximum-width binding invalid")
	}
	s := summaryFixture(a)
	s.ArmedOffsetNS = math.MaxUint64
	s.EndOffsetNS = math.MaxUint64
	s.CandidateLeaseValid = false
	s.Complete = false
	var fill func(reflect.Value)
	fill = func(v reflect.Value) {
		switch v.Kind() {
		case reflect.Struct:
			for i := 0; i < v.NumField(); i++ {
				fill(v.Field(i))
			}
		case reflect.Array:
			for i := 0; i < v.Len(); i++ {
				fill(v.Index(i))
			}
		case reflect.Uint16:
			v.SetUint(4096)
		case reflect.Uint64:
			v.SetUint(math.MaxUint64)
		}
	}
	fill(reflect.ValueOf(&s.Counters).Elem())
	hello, _ := Frame(helloFixture(a))
	armed := armedFixture(a)
	armed.ArmedOffsetNS = math.MaxUint64
	ready, _ := Frame(armed)
	raw, err := Frame(s)
	if err != nil || len(raw)-4 != 3166 || len(hello)+len(ready)+len(raw) != 4469 {
		t.Fatalf("Go/Rust corrected bounds: summary=%d framedchild=%d err=%v", len(raw)-4, len(hello)+len(ready)+len(raw), err)
	}
	var decoded Summary
	if Decode(raw[4:], &decoded) != nil || decoded != s {
		t.Fatal("joint exact numeric counter catalogue drift")
	}
	if s.Matches(a, armed) {
		t.Fatal("conservative width overapproximation claimed jointly reachable arithmetic")
	}
	t.Log("Go agrees with retained Rust R1 maximum-width fixture: SUMMARY3166; HELLO+ARMED+SUMMARY prefixes included4469; independent maxima overapproximate reachable counters")
}
func TestDiagnosticFrozenCounterArithmetic(t *testing.T) {
	c := Counters{VMDispatchStarted: 1, VMDispatchCompleted: 1, HostDispatchStarted: 1, HostDispatchCompleted: 1}
	c.RefreshResults[0][0] = 1
	c.RefreshResults[1][0] = 1
	c.VMIdentified[0] = 1
	c.VMOutcomes[0][3] = 1
	c.VMWriteAttempts[0] = 1
	c.VMLeaseState[0][0] = 1
	c.VMTargetPredicates[0][0][0] = 1
	c.VMTargetPredicates[0][1][1] = 1
	c.HostReplyClass[0] = 1
	c.HostClassOutcomes[0][2] = 1
	c.HostWriteAttempts[0] = 1
	if !c.Arithmetic() {
		t.Fatal("balanced representative dispatch refused")
	}
	tests := map[string]func(*Counters){"vm completion": func(c *Counters) { c.VMDispatchCompleted = 0 }, "host completion": func(c *Counters) { c.HostDispatchCompleted = 0 }, "vm refresh": func(c *Counters) { c.RefreshResults[0][0] = 0 }, "host refresh": func(c *Counters) { c.RefreshResults[1][0] = 0 }, "vm outcome": func(c *Counters) { c.VMOutcomes[0][3] = 0 }, "vm lease": func(c *Counters) { c.VMLeaseState[0][0] = 0 }, "vm target candidate": func(c *Counters) { c.VMTargetPredicates[0][0][0] = 0 }, "vm target control": func(c *Counters) { c.VMTargetPredicates[0][1][1] = 0 }, "vm writes": func(c *Counters) { c.VMWriteAttempts[0] = 0 }, "host outcomes": func(c *Counters) { c.HostClassOutcomes[0][2] = 0 }, "host writes": func(c *Counters) { c.HostWriteAttempts[0] = 0 }}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			bad := c
			mutate(&bad)
			if bad.Arithmetic() {
				t.Fatal("unbalanced frozen arithmetic admitted")
			}
		})
	}
	a := armFixture()
	for _, duration := range []uint32{999, 30001} {
		a.DurationMS = duration
		if a.Valid(a.Generation, a.Nonce, a.Candidate.MAC, a.Gateway) {
			t.Fatal("duration outside original bounds admitted")
		}
	}
}
func TestDiagnosticLossNeverPromotedByIncompleteSummary(t *testing.T) {
	a := armFixture()
	armed := armedFixture(a)
	for _, kind := range []string{"loss", "overflow", "invalid"} {
		s := summaryFixture(a)
		s.Complete = false
		switch kind {
		case "loss":
			s.Loss = true
		case "overflow":
			s.Overflow = true
		case "invalid":
			s.InvalidFlags[13] = true
		}
		if s.Matches(a, armed) {
			t.Fatalf("incomplete %s admitted as valid watch", kind)
		}
	}
}

// Original independent review five negatives and two positives, retained unchanged.
func TestDiagnosticCompleteCounterReviewCases(t *testing.T) {
	a := armFixture()
	ready := armedFixture(a)
	for _, mode := range []string{"vm_refresh_failure", "host_refresh_failure", "unidentified_refresh_failure", "vm_length_mismatch", "host_length_mismatch", "valid_zero", "valid_vm_api_full"} {
		t.Run(mode, func(t *testing.T) {
			s := summaryFixture(a)
			c := &s.Counters
			switch mode {
			case "unidentified_refresh_failure":
				c.VMDispatchStarted = 1
				c.VMDispatchCompleted = 1
				c.RefreshResults[0][2] = 1
			case "vm_refresh_failure", "vm_length_mismatch", "valid_vm_api_full":
				c.VMDispatchStarted = 1
				c.VMDispatchCompleted = 1
				c.VMIdentified[0] = 1
				c.VMLeaseState[0][0] = 1
				c.VMTargetPredicates[0][0][0] = 1
				c.VMTargetPredicates[0][1][0] = 1
				if mode == "vm_refresh_failure" {
					c.RefreshResults[0][2] = 1
					c.VMOutcomes[0][2] = 1
				} else {
					c.RefreshResults[0][1] = 1
					c.VMWriteAttempts[0] = 1
					c.VMWriteBytes[0][0] = 42
					if mode == "vm_length_mismatch" {
						c.VMOutcomes[0][4] = 1
						c.VMWriteBytes[0][1] = 41
					} else {
						c.VMOutcomes[0][3] = 1
						c.VMWriteBytes[0][1] = 42
					}
				}
			case "host_refresh_failure", "host_length_mismatch":
				c.HostDispatchStarted = 1
				c.HostDispatchCompleted = 1
				c.HostReplyClass[0] = 1
				if mode == "host_refresh_failure" {
					c.RefreshResults[1][2] = 1
					c.HostClassOutcomes[0][1] = 1
				} else {
					c.RefreshResults[1][1] = 1
					c.HostClassOutcomes[0][3] = 1
					c.HostWriteAttempts[0] = 1
					c.HostWriteBytes[0][0] = 42
					c.HostWriteBytes[0][1] = 41
				}
			}
			raw, e := Encode(s)
			if e != nil {
				t.Fatal(e)
			}
			var got Summary
			if Decode(raw, &got) != nil || got != s || !c.Arithmetic() {
				t.Fatal("fixture is not strict-decoded balanced metadata")
			}
			want := mode == "valid_zero" || mode == "valid_vm_api_full"
			matched, standalone := got.Matches(a, ready), got.ValidStandalone()
			t.Logf("balanced=%v complete=%v loss=%v matches=%v standalone=%v bytes=%d", c.Arithmetic(), got.Complete, got.Loss, matched, standalone, len(raw))
			if matched != want || standalone != want {
				t.Errorf("inconsistent Complete counter semantics accepted: matches=%v standalone=%v want=%v", matched, standalone, want)
			}
		})
	}
}

// Balanced fixtures isolate terminal-column implications from refresh totals:
// a row refresh_error still precludes Complete when only success is declared.
func completeCounterReceipt(a Arm, mode string, row int) Summary {
	s := summaryFixture(a)
	c := &s.Counters
	switch mode {
	case "vm_refresh_failure", "unidentified_refresh_failure":
		c.VMDispatchStarted = 1
		c.VMDispatchCompleted = 1
		c.RefreshResults[0][2] = 1
	case "host_refresh_failure", "unidentified_host_refresh_failure":
		c.HostDispatchStarted = 1
		c.HostDispatchCompleted = 1
		c.RefreshResults[1][2] = 1
	}
	switch mode {
	case "vm_refresh_failure", "vm_row_refresh_failure", "vm_length_mismatch", "valid_vm_api_full", "valid_vm_denial":
		c.VMDispatchStarted = 1
		c.VMDispatchCompleted = 1
		c.VMIdentified[row] = 1
		c.VMLeaseState[row][0] = 1
		c.VMTargetPredicates[row][0][0] = 1
		c.VMTargetPredicates[row][1][0] = 1
		if mode != "vm_refresh_failure" {
			c.RefreshResults[0][1] = 1
		}
		switch mode {
		case "vm_refresh_failure", "vm_row_refresh_failure":
			c.VMOutcomes[row][2] = 1
		case "valid_vm_denial":
			c.VMOutcomes[row][0] = 1
		default:
			c.VMWriteAttempts[row] = 1
			c.VMWriteBytes[row][0] = 42
			c.VMWriteBytes[row][1] = 42
			c.VMOutcomes[row][3] = 1
			if mode == "vm_length_mismatch" {
				c.VMOutcomes[row][3] = 0
				c.VMOutcomes[row][4] = 1
				c.VMWriteBytes[row][1] = 41
			}
		}
	case "host_refresh_failure", "host_row_refresh_failure", "host_length_mismatch", "valid_host_api_full", "valid_host_denial":
		c.HostDispatchStarted = 1
		c.HostDispatchCompleted = 1
		c.HostReplyClass[row] = 1
		if mode != "host_refresh_failure" {
			c.RefreshResults[1][1] = 1
		}
		switch mode {
		case "host_refresh_failure", "host_row_refresh_failure":
			c.HostClassOutcomes[row][1] = 1
		case "valid_host_denial":
			c.HostClassOutcomes[row][0] = 1
		default:
			c.HostWriteAttempts[row] = 1
			c.HostWriteBytes[row][0] = 42
			c.HostWriteBytes[row][1] = 42
			c.HostClassOutcomes[row][2] = 1
			if mode == "host_length_mismatch" {
				c.HostClassOutcomes[row][2] = 0
				c.HostClassOutcomes[row][3] = 1
				c.HostWriteBytes[row][1] = 41
			}
		}
	}
	return s
}
func TestDiagnosticCompleteCounterEveryRowAndLegitimateOutcomes(t *testing.T) {
	a := armFixture()
	armed := armedFixture(a)
	for _, direction := range []string{"vm", "host"} {
		rows := 3
		if direction == "host" {
			rows = 6
		}
		for row := 0; row < rows; row++ {
			for _, suffix := range []string{"row_refresh_failure", "length_mismatch", "denial", "api_full"} {
				mode := direction + "_" + suffix
				valid := suffix == "denial" || suffix == "api_full"
				if valid {
					mode = "valid_" + mode
				}
				t.Run(mode+"/"+string(rune('0'+row)), func(t *testing.T) {
					s := completeCounterReceipt(a, mode, row)
					raw, err := Encode(s)
					var decoded Summary
					if err != nil || Decode(raw, &decoded) != nil || decoded != s || !decoded.Counters.Arithmetic() {
						t.Fatal("fixture is not strict-decoded balanced metadata")
					}
					if decoded.Matches(a, armed) != valid || decoded.ValidStandalone() != valid {
						t.Fatal("Complete terminal-column implication or valid outcome changed")
					}
					if !valid {
						decoded.Complete = false
						if !decoded.Matches(a, armed) || !decoded.ValidStandalone() {
							t.Fatal("counter-only incompleteness was widened into a new admission rule")
						}
					}
				})
			}
		}
	}
	s := completeCounterReceipt(a, "unidentified_host_refresh_failure", 0)
	if !s.Counters.Arithmetic() || s.Matches(a, armed) || s.ValidStandalone() {
		t.Fatal("unidentified host refresh failure admitted as Complete")
	}
}

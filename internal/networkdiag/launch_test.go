//go:build n1diagnostic && !n1candidate

package networkdiag

import (
	"strings"
	"testing"
	"time"
)

func TestLaunchObservationAndArmReceiptStrictBounds(t *testing.T) {
	a := armFixture()
	a.Candidate.BackendObject = strings.Repeat("x", 128)
	a.Control.BackendObject = strings.Repeat("y", 128)
	a.DurationMS = 30000
	r := ClockReading{WallNS: 1000000000000000000, ContinuousNS: 1000000000}
	d, _ := deadlineReading(r, PrearmCap)
	o := LaunchObservation{Version: 1, Inspection: Inspection{Binding: a.Candidate, PinFingerprint: "SHA256:" + strings.Repeat("A", 43), ConfigSHA256: strings.Repeat("a", 64), ObservedUnixNS: r.WallNS}, Watch: WatchObservation{Hello: helloFixture(a), Phase: "available", Anchor: r, Deadline: d, Observed: r}, Owner: ProcessCorrelation{PID: 1<<32 - 1, BirthUS: ^uint64(0), UniqueID: ^uint64(0)}, Child: ProcessCorrelation{PID: 1<<32 - 2, BirthUS: ^uint64(0), UniqueID: ^uint64(0)}}
	if !o.Valid() {
		t.Fatal("maximum width observation")
	}
	raw, e := Encode(o)
	var got LaunchObservation
	if e != nil || len(raw) > MaxFrame || Decode(raw, &got) != nil || got != o {
		t.Fatal("bounded observation", e)
	}
	for _, bad := range []string{strings.Replace(string(raw), `"version":1`, `"version":1,"version":1`, 1), strings.Replace(string(raw), `"version":1`, `"version":1,"extra":1`, 1), strings.Replace(string(raw), `"pid":4294967295`, `"pid":4294967296`, 1), string(raw) + "{}"} {
		if Decode([]byte(bad), &got) == nil {
			t.Fatal("non-strict observation")
		}
	}
	for _, mode := range []string{"phase", "cap", "generation", "mac", "birth", "owner-child", "expired"} {
		bad := o
		switch mode {
		case "phase":
			bad.Watch.Phase = "arming"
		case "cap":
			bad.Watch.Deadline.WallNS++
		case "generation":
			bad.Watch.Hello.Generation = a.Control.Generation
		case "mac":
			bad.Watch.Hello.CandidateMAC = a.Control.MAC
		case "birth":
			bad.Child.BirthUS = 0
		case "owner-child":
			bad.Child.PID = bad.Owner.PID
		case "expired":
			bad.Watch.Observed = bad.Watch.Deadline
		}
		if bad.Valid() {
			t.Fatal("bad launch binding accepted", mode)
		}
	}
	armDeadline, _ := deadlineReading(r, time.Duration(a.DurationMS)*time.Millisecond+100*time.Millisecond)
	receipt := ArmReceipt{Version: 1, Armed: armedFixture(a), Sent: r, Deadline: armDeadline}
	receipt.Armed.ArmedOffsetNS = ^uint64(0)
	if !receipt.Matches(a) {
		t.Fatal("maximum receipt")
	}
	raw, e = Encode(receipt)
	var received ArmReceipt
	if e != nil || len(raw) > MaxFrame || Decode(raw, &received) != nil || received != receipt {
		t.Fatal("bounded strict receipt", e)
	}
	receipt.Deadline.WallNS++
	if receipt.Matches(a) {
		t.Fatal("receipt extended actual interval")
	}
}

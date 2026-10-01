package adjudicate

import "testing"

func TestConnectedSurvivesLaterFailure(t *testing.T) {
	r := Network(NetworkEvidence{ConnectValid: true, Connected: true, TransportOK: false})
	if r.Verdict != "FAIL" {
		t.Fatal(r)
	}
}
func TestHistorical113AndTimeoutPremises(t *testing.T) {
	if r := Network(NetworkEvidence{ConnectValid: true, Errno: 113}); r.Verdict != "UNQUALIFIED" {
		t.Fatal(r)
	}
	e := NetworkEvidence{ConnectValid: true, Timeout: true, TransportOK: true, TimingOK: true, PositiveBefore: true, PositiveAfter: true, ControlsBefore: true, ControlsAfter: true, Provenance: true, ObserversComplete: true, WatchComplete: true, CaptureQualified: true}
	if r := Network(e); r.Verdict != "PASS" {
		t.Fatal(r)
	}
	e.CaptureQualified = false
	if r := Network(e); r.Verdict == "PASS" || r.Attribution == "pre_emission" {
		t.Fatal(r)
	}
	e.Connected = true
	e.TransportOK = false
	if r := Network(e); r.Verdict != "FAIL" {
		t.Fatal(r)
	}
}
func TestWriteReturnNeverDelivery(t *testing.T) {
	e := NetworkEvidence{VMWriteReturned: true}
	r := Network(e)
	if r.Enqueue || r.Delivery {
		t.Fatal(r)
	}
}

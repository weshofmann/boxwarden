// Package adjudicate contains verdicts only. It has no dispatch authority.
package adjudicate

type NetworkEvidence struct {
	ConnectValid, Connected, Timeout, TransportOK, TimingOK                   bool
	Errno                                                                     int
	PositiveBefore, PositiveAfter, ControlsBefore, ControlsAfter, Provenance  bool
	ObserversComplete, WatchComplete, CaptureQualified                        bool
	ExactARPPolicyDenied, HostWriteAttempted, VMWriteReturned, OutgoingSilent bool
}
type NetworkResult struct {
	Version     int    `json:"version"`
	Verdict     string `json:"verdict"`
	Attribution string `json:"attribution"`
	Enqueue     bool   `json:"enqueue"`
	Delivery    bool   `json:"delivery"`
}

func Network(e NetworkEvidence) NetworkResult {
	r := NetworkResult{Version: 1, Verdict: "INCOMPLETE", Attribution: "incomplete"}
	if e.ConnectValid && e.Connected {
		r.Verdict = "FAIL"
	} else if e.ConnectValid && e.Errno == 113 {
		r.Verdict = "UNQUALIFIED"
	} else if e.ConnectValid && e.Timeout && e.TransportOK && e.TimingOK && e.PositiveBefore && e.PositiveAfter && e.ControlsBefore && e.ControlsAfter && e.Provenance && e.ObserversComplete && e.WatchComplete && e.CaptureQualified {
		r.Verdict = "PASS"
	}
	complete := e.Provenance && e.ObserversComplete && e.WatchComplete && e.PositiveBefore && e.PositiveAfter && e.ControlsBefore && e.ControlsAfter
	if complete && e.ExactARPPolicyDenied && !e.HostWriteAttempted {
		r.Attribution = "policy_denied_before_write"
	} else if complete && e.OutgoingSilent && e.CaptureQualified {
		r.Attribution = "pre_emission"
	} else if complete && e.OutgoingSilent {
		r.Attribution = "conditional_capture_absence"
	} else if e.VMWriteReturned {
		r.Attribution = "write_api_return_only"
	}
	return r
}

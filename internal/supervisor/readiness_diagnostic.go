package supervisor

import "strings"

// ReadinessFailureDiagnostic describes only fixed readiness predicates and known internal reasons.
// A snapshot diagnostic from an alternate reader must never become public text.
func ReadinessFailureDiagnostic(snapshot Snapshot) string {
	var unproven []string
	for _, check := range []struct {
		name   string
		proved bool
	}{
		{"backend", snapshot.BackendRunning},
		{"serial", snapshot.SerialHealthy},
		{"host-key pin", snapshot.PinPresent},
		{"certificate", snapshot.CertificateCurrent},
		{"ssh probe", snapshot.ProbeOK},
		{"guest time zone", snapshot.ZoneMatches},
	} {
		if !check.proved {
			unproven = append(unproven, check.name)
		}
	}
	diagnostic := "exact live supervisor readiness unproven checks: " + strings.Join(unproven, ", ")
	switch snapshot.Diagnostic {
	case "snapshot observation expired",
		"exact backend observation failed",
		"exact retained Tart child lifetime is unavailable",
		"exact host-key pin verification failed",
		"management certificate requires renewal",
		"management connection binding changed",
		"strict management SSH probe failed",
		"trusted host time zone detection failed",
		"guest time zone verification failed",
		"exact runtime changed during observation":
		return diagnostic + "; " + snapshot.Diagnostic
	default:
		return diagnostic
	}
}

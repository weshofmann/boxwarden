package supervisor

// StopRequest records which bounded shutdown request reached its caller.
// Guest acceptance is only an acknowledgment, never proof of a clean volume.
type StopRequest string

const (
	StopRequestUnrequested   StopRequest = "unrequested"
	StopRequestGuestAccepted StopRequest = "guest_accepted"
	StopRequestTartFallback  StopRequest = "tart_fallback"
	StopRequestTartOnly      StopRequest = "tart_only"
)

type StopOutcome struct {
	Request StopRequest `json:"request"`
	Forced  bool        `json:"forced"`
}

func (o StopOutcome) Valid() bool {
	switch o.Request {
	case StopRequestUnrequested, StopRequestGuestAccepted, StopRequestTartFallback, StopRequestTartOnly:
		return true
	default:
		return false
	}
}

//go:build n1diagnostic && !n1candidate

package networkdiag

// ProcessCorrelation is observational metadata, never authority to adopt,
// reconstruct, stop or reap a process.
type ProcessCorrelation struct {
	PID      uint32 `json:"pid"`
	BirthUS  uint64 `json:"birth_us"`
	UniqueID uint64 `json:"unique_id"`
}

func (p ProcessCorrelation) Valid() bool { return p.PID > 0 && p.BirthUS > 0 && p.UniqueID > 0 }

type WatchObservation struct {
	Hello    Hello        `json:"hello"`
	Phase    string       `json:"phase"`
	Anchor   ClockReading `json:"anchor"`
	Deadline ClockReading `json:"deadline"`
	Observed ClockReading `json:"observed"`
}

func (w WatchObservation) Valid() bool {
	d, e := deadlineReading(w.Anchor, PrearmCap)
	h := w.Hello
	return e == nil && w.Phase == "available" && w.Deadline == d && beforeReading(w.Observed, d) && w.Observed.WallNS >= w.Anchor.WallNS && w.Observed.ContinuousNS >= w.Anchor.ContinuousNS && h.Version == 1 && h.Kind == "HELLO" && UUID(h.Generation) && UUID(h.Nonce) && MAC(h.CandidateMAC) && Private(h.Gateway)
}

type LaunchObservation struct {
	Version    uint8              `json:"version"`
	Inspection Inspection         `json:"inspection"`
	Watch      WatchObservation   `json:"watch"`
	Owner      ProcessCorrelation `json:"owner"`
	Child      ProcessCorrelation `json:"child"`
}

func (l LaunchObservation) Valid() bool {
	return l.Version == 1 && l.Inspection.Valid() && l.Watch.Valid() && l.Owner.Valid() && l.Child.Valid() && l.Owner.PID != l.Child.PID && l.Watch.Hello.Generation == l.Inspection.Binding.Generation && l.Watch.Hello.CandidateMAC == l.Inspection.Binding.MAC && l.Watch.Observed.WallNS >= l.Inspection.ObservedUnixNS
}

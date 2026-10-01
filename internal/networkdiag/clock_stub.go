//go:build (n1diagnostic || n1clipboarddiagnostic) && !n1candidate && ((!darwin && !linux) || (darwin && !cgo) || !n1diagnostic)

package networkdiag

func nativeClock() (ClockReading, error) { return ClockReading{}, ErrMetadata }

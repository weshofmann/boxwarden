//go:build !n1diagnostic || n1candidate

package serialx

type diagnosticCloseState struct{}

func (*Runtime) captureDiagnosticStreamClose(error)          {}
func (*Runtime) diagnosticStreamCloseError() error           { return nil }
func (*Runtime) finishDiagnosticStreamClose(err error) error { return err }

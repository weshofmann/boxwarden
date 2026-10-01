//go:build n1diagnostic && !n1candidate

package tart

import "github.com/weshofmann/boxwarden/internal/networkdiag"

type diagnosticProcessObserver interface {
	RetainedDiagnosticProcess() (networkdiag.ProcessCorrelation, error)
}

func (h *osProcessHandle) RetainedDiagnosticProcess() (networkdiag.ProcessCorrelation, error) {
	return correlateDiagnosticProcess(h, readDiagnosticProcess)
}
func correlateDiagnosticProcess(h *osProcessHandle, read func(uint32) (networkdiag.ProcessCorrelation, error)) (networkdiag.ProcessCorrelation, error) {
	if h == nil || h.process == nil || h.process.Pid <= 0 || h.process.Pid > 1<<31-1 || read == nil {
		return networkdiag.ProcessCorrelation{}, networkdiag.ErrMetadata
	}
	h.stopMu.Lock()
	defer h.stopMu.Unlock()
	if h.reaped || h.authorityLost || h.done == nil {
		return networkdiag.ProcessCorrelation{}, networkdiag.ErrMetadata
	}
	select {
	case <-h.done:
		return networkdiag.ProcessCorrelation{}, networkdiag.ErrMetadata
	default:
	}
	p, e := read(uint32(h.process.Pid))
	if e != nil || !p.Valid() || p.PID != uint32(h.process.Pid) {
		return networkdiag.ProcessCorrelation{}, networkdiag.ErrMetadata
	}
	return p, nil
}
func forwardedDiagnosticProcess(h any) (networkdiag.ProcessCorrelation, error) {
	p, ok := h.(diagnosticProcessObserver)
	if !ok {
		return networkdiag.ProcessCorrelation{}, networkdiag.ErrMetadata
	}
	return p.RetainedDiagnosticProcess()
}
func (h *scratchHandle) RetainedDiagnosticProcess() (networkdiag.ProcessCorrelation, error) {
	return forwardedDiagnosticProcess(h.Handle)
}
func (h *managedDiskHandle) RetainedDiagnosticProcess() (networkdiag.ProcessCorrelation, error) {
	return forwardedDiagnosticProcess(h.Handle)
}

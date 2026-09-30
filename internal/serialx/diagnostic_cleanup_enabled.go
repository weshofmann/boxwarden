//go:build n1diagnostic && !n1candidate

package serialx

import (
	"errors"
	"sync"
)

type diagnosticCloseState struct {
	mu  sync.Mutex
	err error
}

func (r *Runtime) captureDiagnosticStreamClose(err error) {
	r.diagnosticClose.mu.Lock()
	r.diagnosticClose.err = err
	r.diagnosticClose.mu.Unlock()
}
func (r *Runtime) diagnosticStreamCloseError() error {
	r.diagnosticClose.mu.Lock()
	defer r.diagnosticClose.mu.Unlock()
	return r.diagnosticClose.err
}
func (r *Runtime) finishDiagnosticStreamClose(err error) error {
	return errors.Join(err, r.diagnosticStreamCloseError())
}

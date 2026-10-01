//go:build n1diagnostic && !n1candidate

package serialx

import (
	"errors"
	"net"
	"testing"
)

type diagnosticCloseStream struct {
	net.Conn
	cause error
}

func (s diagnosticCloseStream) Close() error { return errors.Join(s.Conn.Close(), s.cause) }
func TestDiagnosticActualMasterCloseErrorRetained(t *testing.T) {
	host, peer := net.Pipe()
	defer peer.Close()
	cause := errors.New("synthetic master close uncertainty")
	r := newRuntime(diagnosticCloseStream{host, cause}, requestGeneration)
	if err := r.Close(); !errors.Is(err, cause) {
		t.Fatalf("actual master close was discharged: %v", err)
	}
}

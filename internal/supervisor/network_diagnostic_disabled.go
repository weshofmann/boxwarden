//go:build (!n1diagnostic && !n1clipboarddiagnostic) || n1candidate

package supervisor

import (
	"context"
	"net"
	"time"
)

func handleNetworkDiagnosticControl(context.Context, net.Conn, Binding, RuntimeOwner, []byte, time.Time) bool {
	return false
}

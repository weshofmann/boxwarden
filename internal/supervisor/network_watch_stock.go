//go:build n1clipboarddiagnostic && !n1diagnostic && !n1candidate

package supervisor

import (
	"context"
	"net"
	"time"
)

func handleNetworkWatchControl(context.Context, net.Conn, Binding, RuntimeOwner, []byte, time.Time, string) bool {
	return true
}

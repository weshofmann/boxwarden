//go:build darwin && cgo && n1diagnostic && n1cleanup && !n1candidate

package n1

import "testing"

// Calls the actual C count/error boundary with injected scalars only; never
// invokes proc_listallpids, proc_pidinfo, proc_pidpath or current-host census.
func TestNativeCountContractInjectedOnly(t *testing.T) {
	for _, x := range []struct{ count, code, want int }{{1, 0, 1}, {8192, 0, 8192}, {0, 0, -1}, {-1, 0, -1}, {8193, 0, -1}, {1, 13, -1}} {
		if got := checkedNativeCount(x.count, x.code); got != x.want {
			t.Fatal(x, got)
		}
	}
}

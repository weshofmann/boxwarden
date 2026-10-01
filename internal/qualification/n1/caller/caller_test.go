package caller

import (
	"bytes"

	"errors"

	"os"

	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/weshofmann/boxwarden/internal/qualification/n1/contract"
)

func TestRetainedCoordinatorRequiresActualPlannedReturn(t *testing.T) {
	for _, x := range []struct {
		code string
		good bool
	}{{"exit 20", true}, {"exit 0", false}, {"printf forged; exit 20", false}, {"printf leaked >&2; exit 20", false}, {"sleep 2 & exit 20", false}, {"printf '%020000d' 1; exit 20", false}} {
		t.Run(x.code, func(t *testing.T) {
			r, e := waitCoordinator("/bin/sh", []string{"-c", x.code}, []string{"PATH=/usr/bin:/bin"}, bytes.NewReader(nil), time.Now().Add(150*time.Millisecond), func(f *os.File) error { return f.Close() })
			if (e == nil) != x.good {
				t.Fatalf("exit=%d closed=%v error=%v", r.Exit, r.Closed, e)
			}
		})
	}
}
func TestRetainedCoordinatorActualCloseErrorRefuses(t *testing.T) {
	_, e := waitCoordinator("/bin/sh", []string{"-c", "exit 20"}, nil, bytes.NewReader(nil), time.Now().Add(time.Second), func(f *os.File) error { e := f.Close(); return errors.Join(e, errors.New("after-close")) })
	if e == nil {
		t.Fatal("accepted failed close")
	}
}
func TestCallerOriginChargesPreflightWithoutReset(t *testing.T) {
	w := originFixture()
	if preflightRemaining(w, w.StartedUnixNS+599*1e9, w.ContinuousStartNS+599*1e9) != nil {
		t.Fatal("valid remaining budget")
	}
	for _, n := range []uint64{600, 1200, 1800} {
		if preflightRemaining(w, w.StartedUnixNS+n*1e9, w.ContinuousStartNS+n*1e9) == nil {
			t.Fatal("pre-L attendance exhaustion", n)
		}
	}
}
func originFixture() contract.Window {
	return contract.Window{LockSHA: strings.Repeat("a", 64), ID: "11111111-1111-4111-8111-111111111111", StartedUnixNS: 1000000000, ExpiresUnixNS: 1000000000 + contract.WindowNS, ContinuousStartNS: 1000000000, ContinuousLimitNS: 1000000000 + contract.WindowNS}
}
func TestCallerEarlyReplacementRefusalClosesOwnedInput(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("native Darwin replacement input")
	}
	f, e := os.CreateTemp(t.TempDir(), "ordinary-file")
	if e != nil {
		t.Fatal(e)
	}
	if replaceImage("/no-such-reviewed-image", []string{"unused"}, nil, f) == nil {
		t.Fatal("non-pipe accepted")
	}
	if _, e = f.Stat(); e == nil {
		f.Close()
		t.Fatal("early replacement refusal leaked owned descriptor")
	}
}

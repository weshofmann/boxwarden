package diskreserve

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestReservePreservesFloorWithSamplingMargin(t *testing.T) {
	const gib = uint64(1) << 30
	for _, tc := range []struct {
		name     string
		capacity uint64
		free     uint64
		wantFail bool
	}{
		{"twenty-gib-floor-breached", 100 * gib, 20*gib + gib/2, true},
		{"twenty-gib-floor-with-margin", 100 * gib, 22 * gib, false},
		{"ten-percent-floor-breached", 300 * gib, 30*gib + gib/2, true},
		{"ten-percent-floor-with-margin", 300 * gib, 32 * gib, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := checkValues(tc.capacity, tc.free)
			if (err != nil) != tc.wantFail {
				t.Fatalf("reserve(capacity=%d, free=%d) = %v", tc.capacity, tc.free, err)
			}
		})
	}
}

func TestRunRejectsUnsafePathBeforeOperation(t *testing.T) {
	root := t.TempDir()
	regular := filepath.Join(root, "not-a-directory")
	if err := os.WriteFile(regular, []byte("test"), 0600); err != nil {
		t.Fatal(err)
	}
	called := false
	err := Run(context.Background(), []string{regular}, func(context.Context) error {
		called = true
		return nil
	})
	if err == nil || called {
		t.Fatalf("unsafe path admitted: err=%v, called=%t", err, called)
	}
}

func TestRunCancelsOperationAndReturnsReserveFailure(t *testing.T) {
	var checks atomic.Int32
	err := runWithChecker(context.Background(), time.Millisecond, func() error {
		if checks.Add(1) == 1 {
			return nil
		}
		return errors.New("simulated low space")
	}, func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	})
	if err == nil || !strings.Contains(err.Error(), "simulated low space") || !errors.Is(err, context.Canceled) {
		t.Fatalf("reserve stop lost cause or cancellation: %v", err)
	}
}

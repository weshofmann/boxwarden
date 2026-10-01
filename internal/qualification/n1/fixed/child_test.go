package fixed

import (
	"errors"
	"os"
	"os/exec"
	"sync/atomic"
	"testing"
	"time"
)

func TestOwnedChildActualExitAndEOF(t *testing.T) {
	if os.Getenv("N1_TEST_CHILD") == "1" {
		switch os.Getenv("N1_TEST_MODE") {
		case "ok":
			os.Stdout.Write([]byte("witness\n"))
			os.Exit(0)
		case "nonzero":
			os.Stdout.Write([]byte("witness\n"))
			os.Exit(7)
		case "hold":
			time.Sleep(time.Second)
			os.Exit(0)
		case "descendant":
			c := exec.Command(os.Args[0], "-test.run=^TestOwnedChildActualExitAndEOF$")
			c.Env = []string{"GORACE=atexit_sleep_ms=0", "N1_TEST_CHILD=1", "N1_TEST_MODE=hold"}
			c.Stdout = os.Stdout
			c.Stderr = os.Stderr
			if c.Start() != nil {
				os.Exit(9)
			}
			os.Exit(0)
		case "overflow":
			os.Stdout.Write(make([]byte, 514))
			os.Exit(0)
		}
		os.Exit(9)
	}
	for _, mode := range []string{"ok", "nonzero", "overflow"} {
		t.Run(mode, func(t *testing.T) {
			r, e := WaitChild(os.Args[0], []string{"-test.run=^TestOwnedChildActualExitAndEOF$"}, []string{"GORACE=atexit_sleep_ms=0", "N1_TEST_CHILD=1", "N1_TEST_MODE=" + mode}, nil, time.Now().Add(time.Second*5))
			if mode == "ok" && (e != nil || r.Exit != 0 || !r.Closed || string(r.Raw) != "witness\n") {
				t.Fatal("actual successful child not admitted", r, e)
			}
			if mode == "nonzero" && (r.Exit != 7 || e == nil) {
				t.Fatal("nonzero hidden", r, e)
			}
			if mode == "overflow" && e == nil {
				t.Fatal("overflow accepted")
			}
		})
	}
}

func TestOwnedChildExitDoesNotCertifyDescendantEOF(t *testing.T) {
	start := time.Now()
	r, e := WaitChild(os.Args[0], []string{"-test.run=^TestOwnedChildActualExitAndEOF$"}, []string{"GORACE=atexit_sleep_ms=0", "N1_TEST_CHILD=1", "N1_TEST_MODE=descendant"}, nil, start.Add(150*time.Millisecond))
	if e == nil || r.Closed {
		t.Fatal("held descendant EOF admitted", r, e)
	}
	if time.Since(start) > 700*time.Millisecond {
		t.Fatal("drain deadline disabled after direct exit")
	}
}

func TestActualChildCheckedCloseReturnFailureRefuses(t *testing.T) {
	for fail := int32(1); fail <= 4; fail++ {
		var calls atomic.Int32
		_, e := waitChild(os.Args[0], []string{"-test.run=^TestOwnedChildActualExitAndEOF$"}, []string{"GORACE=atexit_sleep_ms=0", "N1_TEST_CHILD=1", "N1_TEST_MODE=ok"}, nil, time.Now().Add(time.Second), func(f *os.File) error {
			e := f.Close()
			if calls.Add(1) == fail && e == nil {
				return errors.New("after actual close")
			}
			return e
		})
		if e == nil {
			t.Fatal("checked close failure hidden", fail)
		}
	}
}

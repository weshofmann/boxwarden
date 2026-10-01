package fixed

import (
	"errors"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/contract"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

func closeoutFixtureWitness() contract.Witness {
	return contract.Witness{Version: 1, Phase: 3, LockSHA: strings.Repeat("a", 64), WindowID: "11111111-1111-1111-1111-111111111111", HandoffSHA: strings.Repeat("b", 64), CompletionSHA: strings.Repeat("c", 64), Exit: 0}
}
func TestCloseoutInheritedActualChild(t *testing.T) {
	if mode := os.Getenv("N1_CLOSEOUT_TEST_MODE"); mode != "" {
		switch mode {
		case "read":
			w, e := ReadCloseoutTransition(time.Now().Add(time.Second))
			if e != nil || w != closeoutFixtureWitness() {
				os.Exit(41)
			}
		case "output":
			os.Stdout.Write([]byte("unexpected"))
		case "stderr":
			os.Stderr.Write([]byte("unexpected"))
		case "exit":
			os.Exit(7)
		case "held":
			time.Sleep(120 * time.Millisecond)
		}
		os.Exit(0)
	}
	raw, e := contract.EncodeWitness(closeoutFixtureWitness())
	if e != nil {
		t.Fatal(e)
	}
	for _, mode := range []string{"read", "output", "stderr", "exit", "held", "close", "write"} {
		t.Run(mode, func(t *testing.T) {
			childmode := mode
			if mode == "close" || mode == "write" {
				childmode = "read"
			}
			closeFile := func(f *os.File) error {
				e := f.Close()
				if mode == "close" {
					return errors.Join(e, errors.New("injected checked close"))
				}
				return e
			}
			write := func(w io.Writer, b []byte) (int, error) {
				if mode == "write" {
					return 0, errors.New("injected write")
				}
				return w.Write(b)
			}
			// A race-instrumented self-test child also performs its exit delay.
			// The held-child negative below retains its independent short deadline.
			deadline := time.Now().Add(5 * time.Second)
			if mode == "held" {
				deadline = time.Now().Add(50 * time.Millisecond)
			}
			r, e := waitCompletionChild(os.Args[0], []string{"-test.run=^TestCloseoutInheritedActualChild$"}, []string{"N1_CLOSEOUT_TEST_MODE=" + childmode}, raw, deadline, closeFile, write)
			if mode == "read" {
				if e != nil || r.Exit != 0 || !r.Closed || len(r.Raw) != 0 {
					t.Fatal("actual retained child/pipe failed", r, e)
				}
			} else if e == nil {
				t.Fatal("uncertain child admitted", mode)
			}
		})
	}
}
func TestCloseoutPipeRejectsForgedPhaseAndMissingEOF(t *testing.T) {
	for _, phase := range []uint8{1, 2, 3} {
		r, w, e := os.Pipe()
		if e != nil {
			t.Fatal(e)
		}
		v := closeoutFixtureWitness()
		v.Phase = phase
		if phase == 1 {
			v.CompletionSHA = ""
			v.Exit = contract.PlannedExit
		}
		raw, e := contract.EncodeWitness(v)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = w.Write(raw); e != nil {
			t.Fatal(e)
		}
		if e = w.Close(); e != nil {
			t.Fatal(e)
		}
		got, e := readTransitionPhase(r, time.Now().Add(time.Second), func(f *os.File) error { return f.Close() }, 3)
		if phase == 3 {
			if e != nil || got != v {
				t.Fatal("phase3 refused", e)
			}
		} else if e == nil {
			t.Fatal("noncompletion phase admitted")
		}
	}
	r, w, e := os.Pipe()
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := contract.EncodeWitness(closeoutFixtureWitness())
	if _, e = w.Write(raw); e != nil {
		t.Fatal(e)
	}
	_, e = readTransitionPhase(r, time.Now().Add(25*time.Millisecond), func(f *os.File) error { return f.Close() }, 3)
	if e == nil {
		t.Fatal("unclosed writer admitted")
	}
	if e = w.Close(); e != nil {
		t.Fatal(e)
	}
}

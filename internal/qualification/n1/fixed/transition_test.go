package fixed

import (
	"errors"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/contract"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func transitionWitness() []byte {
	raw, _ := contract.EncodeWitness(contract.Witness{Version: 1, Phase: 1, LockSHA: strings.Repeat("a", 64), WindowID: "11111111-1111-1111-1111-111111111111", HandoffSHA: strings.Repeat("b", 64), Exit: 20})
	return raw
}
func TestInheritedTransitionActualExecReplacement(t *testing.T) {
	if os.Getenv("N1_EXEC_FIXTURE") == "before" {
		env := []string{"N1_EXEC_FIXTURE=after", "N1_PRE_EXEC_PID=" + strconv.Itoa(os.Getpid())}
		if syscall.Exec(os.Args[0], []string{os.Args[0], "-test.run=^TestInheritedTransitionActualExecReplacement$"}, env) != nil {
			os.Exit(8)
		}
		os.Exit(9)
	}
	if os.Getenv("N1_EXEC_FIXTURE") == "after" {
		if os.Getenv("N1_PRE_EXEC_PID") != strconv.Itoa(os.Getpid()) {
			os.Exit(7)
		}
		w, e := ReadTransition(time.Now().Add(time.Second))
		if e != nil || w.Phase != 1 {
			os.Exit(6)
		}
		os.Stdout.WriteString("exec-and-fd3-EOF-checked\n")
		os.Exit(0)
	}
	r, w, e := os.Pipe()
	if e != nil {
		t.Fatal(e)
	}
	if _, e = w.Write(transitionWitness()); e != nil {
		t.Fatal(e)
	}
	if w.Close() != nil {
		t.Fatal("writer-close")
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestInheritedTransitionActualExecReplacement$")
	cmd.Env = []string{"N1_EXEC_FIXTURE=before"}
	cmd.ExtraFiles = []*os.File{r}
	raw, e := cmd.Output()
	ce := r.Close()
	if e != nil || ce != nil || string(raw) != "exec-and-fd3-EOF-checked\n" {
		t.Fatal("actual image/transition control", string(raw), e, ce)
	}
}
func TestTransitionLossOverflowDeadlineAndAfterCloseFailure(t *testing.T) {
	for _, kind := range []string{"valid", "overflow", "closed", "held", "close-error"} {
		t.Run(kind, func(t *testing.T) {
			r, w, e := os.Pipe()
			if e != nil {
				t.Fatal(e)
			}
			if kind == "closed" {
				r.Close()
			}
			if kind != "held" {
				raw := transitionWitness()
				if kind == "overflow" {
					raw = make([]byte, 514)
				}
				if kind != "closed" {
					w.Write(raw)
				}
				w.Close()
			} else {
				defer w.Close()
			}
			closer := func(f *os.File) error {
				e := f.Close()
				if kind == "close-error" && e == nil {
					return errors.New("after-close-unknown")
				}
				return e
			}
			_, e = readTransition(r, time.Now().Add(100*time.Millisecond), closer)
			if kind == "valid" && e != nil {
				t.Fatal(e)
			}
			if kind != "valid" && e == nil {
				t.Fatal("lost transition admitted")
			}
		})
	}
}

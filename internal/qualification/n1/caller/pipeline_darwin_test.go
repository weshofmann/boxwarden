//go:build darwin && cgo

package caller

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/clock"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/contract"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/fixed"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func pipelineFixture(t *testing.T, childExit int) (callerOps, []string, []byte) {
	t.Helper()
	root, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	now, e := clock.Now()
	if e != nil {
		t.Fatal(e)
	}
	origin := originFixture()
	origin.StartedUnixNS = now.Wall - 1e9
	origin.ExpiresUnixNS = origin.StartedUnixNS + contract.WindowNS
	origin.ContinuousStartNS = now.Continuous - 1e9
	origin.ContinuousLimitNS = origin.ContinuousStartNS + contract.WindowNS
	raw, e := json.Marshal(origin)
	if e != nil {
		t.Fatal(e)
	}
	raw = append(raw, '\n')
	child := root + "/coordinator"
	attendee := root + "/attendee"
	marker := root + "/returned"
	childSource := "#!/bin/sh\n[ \"$#\" -eq 3 ] && [ \"$1\" = --run ] && [ \"$2\" = --approved-lock-sha ] && [ \"$3\" = " + origin.LockSHA + " ] || exit 9\nIFS= read -r original\n[ \"$original\" = '" + strings.TrimSuffix(string(raw), "\n") + "' ] || exit 8\n: > '" + marker + "'\nexit " + fmt.Sprint(childExit) + "\n"
	for p, v := range map[string]string{child: childSource, attendee: "#!/bin/sh\n/bin/cat /dev/fd/3\n"} {
		if e := os.WriteFile(p, []byte(v), 0700); e != nil {
			t.Fatal(e)
		}
		if e := os.Chmod(p, 0700); e != nil {
			t.Fatal(e)
		}
	}
	inputs := fixed.Inputs{LockSHA: origin.LockSHA, HandoffSHA: strings.Repeat("b", 64), Handoff: contract.Handoff{Window: origin, Budget: contract.Budget{ClosedUnixNS: now.Wall, ClosedContinuousNS: now.Continuous, ActiveSpentNS: 1e9, AttendanceSpentNS: 1e9}}}
	ops := callerOps{authorize: func() error { return nil }, static: func() (fixed.StaticInputs, error) { return fixed.StaticInputs{LockSHA: origin.LockSHA}, nil }, inputs: func() (fixed.Inputs, error) {
		if _, e := os.Stat(marker); e != nil {
			t.Fatal("handoff consumed before actual C", e)
		}
		return inputs, nil
	}, now: clock.Now, artifact: func(int, contract.StaticLock) error { return nil }, coordinator: child, attendee: attendee}
	return ops, []string{"L", "--run", "--approved-lock-sha", origin.LockSHA}, raw
}
func TestCallerComposedActualChildThenPipeExec(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("native Darwin image replacement")
	}
	if os.Getenv("N1_PIPELINE_HELPER") == "1" {
		ops, args, raw := pipelineFixture(t, 20)
		if run(args, bytes.NewReader(raw), ops) != nil {
			os.Exit(10)
		}
		os.Exit(11)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestCallerComposedActualChildThenPipeExec$")
	cmd.Env = append(os.Environ(), "N1_PIPELINE_HELPER=1", "GORACE=atexit_sleep_ms=0")
	out, e := cmd.Output()
	if e != nil {
		t.Fatal("composed retained return/exec", e, string(out))
	}
	witness, e := contract.ParseWitness(out)
	if e != nil || witness.Phase != 1 || witness.Exit != 20 || witness.LockSHA != strings.Repeat("a", 64) || witness.HandoffSHA != strings.Repeat("b", 64) {
		t.Fatal("actual phase1 fd3 bytes", string(out), e)
	}
}
func TestCallerForgedHandoffDoesNotAuthorizeReturnZero(t *testing.T) {
	ops, args, raw := pipelineFixture(t, 0)
	read := false
	ops.inputs = func() (fixed.Inputs, error) { read = true; return fixed.Inputs{}, nil }
	if run(args, bytes.NewReader(raw), ops) == nil || read {
		t.Fatal("C exit-zero or supplemental handoff authorized phase1")
	}
}
func TestCallerPostReturnClockAndWindowRefuse(t *testing.T) {
	for _, kind := range []string{"wall-regression", "continuous-regression", "expired", "different-origin", "changed-lock", "approval"} {
		t.Run(kind, func(t *testing.T) {
			ops, args, raw := pipelineFixture(t, 20)
			now, e := clock.Now()
			if e != nil {
				t.Fatal(e)
			}
			calls := 0
			ops.now = func() (clock.Reading, error) {
				calls++
				n := now
				if calls >= 3 {
					switch kind {
					case "wall-regression":
						n.Wall--
					case "continuous-regression":
						n.Continuous--
					case "expired":
						n.Wall += contract.WindowNS
						n.Continuous += contract.WindowNS
					}
				}
				return n, nil
			}
			original := ops.inputs
			ops.inputs = func() (fixed.Inputs, error) {
				v, e := original()
				switch kind {
				case "different-origin":
					v.Handoff.Window.ID = "22222222-2222-4222-8222-222222222222"
				case "changed-lock":
					v.LockSHA = strings.Repeat("c", 64)
				}
				return v, e
			}
			if kind == "approval" {
				args[3] = strings.Repeat("c", 64)
			}
			if run(args, bytes.NewReader(raw), ops) == nil {
				t.Fatal("post-return drift accepted")
			}
		})
	}
}

package n1

import (
	"context"
	"encoding/json"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/clock"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/contract"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/fixed"
	"strings"
	"testing"
)

func TestOriginalStampAndReadOnlyPreflightChargeAuthentication(t *testing.T) {
	l, c := censusFixture()
	sha := strings.Repeat("a", 64)
	w, e := stampWindow(clock.Reading{Wall: 100, Continuous: 200}, "11111111-1111-4111-8111-111111111111", sha)
	if e != nil {
		t.Fatal(e)
	}
	b, _ := json.Marshal(w)
	b = append(b, '\n')
	if _, e = contract.ParseOrigin(b); e != nil {
		t.Fatal(e)
	}
	for _, mode := range []string{"success", "late-auth", "spent-then-late", "wall-regression", "continuous-regression", "clock-error", "static", "doctor", "inventory", "census", "wrong-lock"} {
		t.Run(mode, func(t *testing.T) {
			now := clock.Reading{Wall: w.StartedUnixNS + 9*60*1000000000, Continuous: w.ContinuousStartNS + 9*60*1000000000}
			reads, observations := 0, 0
			d := preflightDependencies{now: func() (clock.Reading, error) {
				reads++
				if mode == "clock-error" {
					return clock.Reading{}, ErrRefused
				}
				if mode == "late-auth" {
					return clock.Reading{Wall: w.StartedUnixNS + contract.AttendanceNS, Continuous: now.Continuous}, nil
				}
				if reads == 3 {
					switch mode {
					case "spent-then-late":
						now.Wall += 2 * 60 * 1000000000
						now.Continuous += 2 * 60 * 1000000000
					case "wall-regression":
						now.Wall--
					case "continuous-regression":
						now.Continuous--
					}
				}
				return now, nil
			}, static: func() (fixed.StaticInputs, error) {
				if mode == "static" {
					return fixed.StaticInputs{}, ErrRefused
				}
				s := fixed.StaticInputs{Lock: l, Catalogue: c, Protected: protectedFixture(), Build: buildFixture(), LockSHA: sha}
				if mode == "wrong-lock" {
					s.LockSHA = strings.Repeat("b", 64)
				}
				return s, nil
			}}
			observe := func(which string) func(context.Context, fixed.StaticInputs) error {
				return func(context.Context, fixed.StaticInputs) error {
					observations++
					if mode == which {
						return ErrRefused
					}
					return nil
				}
			}
			d.doctor = observe("doctor")
			d.inventory = observe("inventory")
			d.census = observe("census")
			e := executePreflight(t.Context(), w, d)
			if mode == "success" {
				if e != nil || observations != 3 {
					t.Fatal(e, observations)
				}
			} else if e == nil {
				t.Fatal("failure recovered/reset origin")
			}
			if mode == "late-auth" && observations != 0 {
				t.Fatal("observation after original cap")
			}
		})
	}
	for _, r := range []clock.Reading{{Wall: 0, Continuous: 1}, {Wall: 1, Continuous: 0}, {Wall: 9223372036854775807, Continuous: 1}, {Wall: 1, Continuous: ^uint64(0)}} {
		if _, e := stampWindow(r, w.ID, sha); e == nil {
			t.Fatal("overflow/zero admitted")
		}
	}
	for _, bad := range [][]byte{b[:len(b)-1], append([]byte(" "), b...), append(b, 'x'), []byte(`{"lock_sha":null}`)} {
		if _, e := contract.ParseOrigin(bad); e == nil {
			t.Fatal("noncanonical origin admitted")
		}
	}
}

func buildFixture() contract.BuildInputs {
	b := contract.BuildInputs{Version: 1, NativeInputSHA: contract.NativeInputSHA, SDKPath: contract.SDKPath, SDKCanonicalName: contract.SDKCanonicalName, PythonAlias: contract.PythonAlias}
	for i := range b.Inputs {
		b.Inputs[i] = contract.ToolInputPolicy(i)
		b.Inputs[i].SHA = strings.Repeat("a", 64)
	}
	return b
}

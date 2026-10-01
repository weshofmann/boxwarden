package closeout

import (
	"context"
	"errors"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/clock"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/contract"
	"strings"
	"testing"
	"time"
)

func flowHandoff() contract.Handoff {
	w := contract.Window{LockSHA: strings.Repeat("a", 64), ID: "12345678-1234-1234-1234-123456789abc", StartedUnixNS: 100, ExpiresUnixNS: 100 + contract.WindowNS, ContinuousStartNS: 200, ContinuousLimitNS: 200 + contract.WindowNS}
	return contract.Handoff{Version: 1, Window: w, Budget: contract.Budget{ClosedUnixNS: 101, ClosedContinuousNS: 201, ActiveSpentNS: 2}, PublicRecords: [2]contract.PublicRecord{{Name: contract.PublicRecordNames[0], SHA: strings.Repeat("d", 64), WindowID: w.ID}, {Name: contract.PublicRecordNames[1], SHA: strings.Repeat("e", 64), WindowID: w.ID}}, Configs: [2]string{contract.StockConfigSHA, contract.CandidateConfigSHA}, Pair: [2]contract.Peer{{HostPinSHA: strings.Repeat("7", 64), Role: "control", Session: "11111111-1111-1111-1111-111111111111", Generation: "22222222-2222-2222-2222-222222222222", Backend: "test-control", Reaped: true, Deleted: true}, {HostPinSHA: strings.Repeat("8", 64), Role: "candidate", Session: "33333333-3333-3333-3333-333333333333", Generation: "44444444-4444-4444-4444-444444444444", Backend: "test-candidate", Reaped: true, Deleted: true}}, Attempts: []contract.Attempt{{ID: "55555555-5555-5555-5555-555555555555", Command: "archive", Closed: true}}, ArchiveSHA: strings.Repeat("b", 64), ArchiveBytes: 1024, ArchiveFiles: 2, DispatchClosed: true, RuntimeClean: true}
}

type stateFixture struct {
	events *[]string
	fail   string
	closed int
}

func (s *stateFixture) Revalidate(context.Context) error {
	*s.events = append(*s.events, "revalidate")
	if s.fail == "revalidate" {
		return errors.New("unproved state")
	}
	return nil
}
func (s *stateFixture) Retire(context.Context) error {
	*s.events = append(*s.events, "retire-state")
	if s.fail == "retire-state" {
		return errors.New("partial retirement")
	}
	return nil
}
func (s *stateFixture) Close() error {
	s.closed++
	*s.events = append(*s.events, "close-state")
	if s.fail == "close-state" {
		return errors.New("checkedclose")
	}
	return nil
}
func TestCloseoutConsumesCompletionAndNeverResumesUncertainBoundary(t *testing.T) {
	for _, fail := range []string{"", "input", "static", "phase", "completion", "archive", "intent", "absence", "doctor", "protected", "inventory", "revalidate", "retire-state", "close-state", "retire-configs", "finish", "expired", "clock-error", "late-wall-regression", "late-continuous-regression", "suspend", "after-final-clock"} {
		t.Run(fail, func(t *testing.T) {
			h := flowHandoff()
			events := []string{}
			state := &stateFixture{events: &events, fail: fail}
			witness := contract.Witness{Version: 1, Phase: 3, LockSHA: h.Window.LockSHA, WindowID: h.Window.ID, HandoffSHA: strings.Repeat("c", 64), CompletionSHA: strings.Repeat("d", 64), Exit: 0}
			c := contract.Completion{Version: 1, Window: h.Window, HandoffSHA: witness.HandoffSHA, ArchiveSHA: h.ArchiveSHA, SoftnetSHA: contract.SoftnetSHA, Removed: 3, DirectoryRemoved: true, ParentSynced: true, HandlesClosed: true}
			step := func(n string) error {
				events = append(events, n)
				if fail == n {
					return errors.New("injected " + n)
				}
				return nil
			}
			d := flowDependencies{
				load: func() (flowInput, error) {
					e := step("input")
					return flowInput{Handoff: h, LockSHA: h.Window.LockSHA, HandoffSHA: witness.HandoffSHA}, e
				},
				transition: func(time.Time) (contract.Witness, error) {
					events = append(events, "phase")
					if fail == "phase" {
						witness.Phase = 2
					}
					return witness, nil
				},
				completion: func() (contract.Completion, string, error) {
					events = append(events, "completion")
					if fail == "completion" {
						c.ArchiveSHA = strings.Repeat("e", 64)
					}
					return c, witness.CompletionSHA, nil
				},
				now: func() (clock.Reading, error) {
					if len(events) > 0 && events[len(events)-1] == "revalidate" {
						switch fail {
						case "late-wall-regression":
							return clock.Reading{Wall: 101, Continuous: 202}, nil
						case "late-continuous-regression":
							return clock.Reading{Wall: 102, Continuous: 201}, nil
						case "suspend":
							return clock.Reading{Wall: 102, Continuous: h.Window.ContinuousLimitNS}, nil
						}
					}
					if fail == "after-final-clock" && len(events) > 0 && events[len(events)-1] == "finish" {
						return clock.Reading{Wall: h.Window.ExpiresUnixNS, Continuous: 202}, nil
					}
					if fail == "clock-error" {
						return clock.Reading{}, errors.New("nativeclock")
					}
					if fail == "expired" {
						return clock.Reading{Wall: h.Window.ExpiresUnixNS, Continuous: 202}, nil
					}
					return clock.Reading{Wall: 102, Continuous: 202}, nil
				},
				static:    func() error { return step("static") },
				archive:   func(context.Context, contract.Handoff) error { return step("archive") },
				reserve:   func(context.Context, contract.Witness, contract.Handoff) error { return step("intent") },
				absence:   func(context.Context) error { return step("absence") },
				doctor:    func(context.Context) error { return step("doctor") },
				protected: func(context.Context) error { return step("protected") },
				inventory: func(context.Context, contract.Handoff, func() error) (retirable, error) {
					e := step("inventory")
					return state, e
				},
				configs: func(context.Context, func() error) error { return step("retire-configs") },
				finish:  func(context.Context, contract.Witness, contract.Handoff) error { return step("finish") },
			}
			e := runFlow(t.Context(), d)
			if fail == "" {
				if e != nil || state.closed != 1 {
					t.Fatal(e, state.closed)
				}
			} else if e == nil {
				t.Fatal("unknown/expired boundary succeeded", fail)
			}
			if fail != "" {
				for _, s := range events {
					if s == "finish" && fail != "finish" && fail != "after-final-clock" {
						t.Fatal("false completion after failure", fail, events)
					}
				}
			}
			if fail == "phase" || fail == "completion" || fail == "expired" || fail == "clock-error" {
				for _, s := range events {
					if s == "intent" || s == "retire-state" {
						t.Fatal("effect without actual bound completion", fail)
					}
				}
			}
			if fail == "intent" || fail == "archive" || fail == "absence" || fail == "doctor" || fail == "protected" || fail == "revalidate" {
				for _, s := range events {
					if s == "retire-state" || s == "retire-configs" {
						t.Fatal("uncertain premise retired", fail, events)
					}
				}
			}
		})
	}
}

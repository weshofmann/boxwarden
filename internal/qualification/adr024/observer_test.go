package adr024

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestObserverAcceptsConsecutivePostDropSamplesWithoutRootSample(t *testing.T) {
	expected := Expected{
		TartPID:           410,
		TartExecutable:    "/qualified/tart",
		SoftnetExecutable: "/qualified/softnet",
		OperatorUID:       501,
		OperatorGID:       20,
	}
	tart := ProcessInfo{
		PID: 410, PPID: 100, PGID: 410,
		UniqueID:            90_410,
		ParentUniqueID:      90_100,
		StartTimeUnixMicros: 1_700_000_000_000_001,
		Executable:          "/qualified/tart",
		Credentials:         Credentials{EffectiveUID: 501, EffectiveGID: 20, RealUID: 501, RealGID: 20, SavedUID: 501, SavedGID: 20},
	}
	softnet := ProcessInfo{
		PID: 411, PPID: 410, PGID: 410,
		UniqueID:            90_411,
		ParentUniqueID:      90_410,
		StartTimeUnixMicros: 1_700_000_000_000_002,
		Executable:          "/qualified/softnet",
		Credentials:         Credentials{EffectiveUID: 501, EffectiveGID: 20, RealUID: 501, RealGID: 20, SavedUID: 501, SavedGID: 20},
	}
	sampler := &sequenceSampler{samples: []Sample{
		{Tart: tart, Children: []ProcessInfo{softnet}},
		{Tart: tart, Children: []ProcessInfo{softnet}},
		{Tart: tart, Children: []ProcessInfo{softnet}},
	}}

	report, err := (Observer{Sampler: sampler, MaxSamples: 3, RequiredSteadySamples: 3}).Observe(context.Background(), expected)
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	if report.Status != StatusPass {
		t.Fatalf("status = %q, want %q", report.Status, StatusPass)
	}
	if report.Softnet.RootEffectiveObserved {
		t.Fatal("root-effective credentials reported despite no matching sample")
	}
	if report.Softnet.ConsecutiveSteadySamples != 3 {
		t.Fatalf("steady samples = %d, want 3", report.Softnet.ConsecutiveSteadySamples)
	}
	if report.Tart.PID != 410 || report.Tart.StartTimeUnixMicros != 1_700_000_000_000_001 || !report.Tart.ExecutableMatch {
		t.Fatalf("Tart evidence = %#v", report.Tart)
	}
	if report.Softnet.PID != 411 || report.Softnet.PPID != 410 || report.Softnet.StartTimeUnixMicros != 1_700_000_000_000_002 || !report.Softnet.ExecutableMatch {
		t.Fatalf("Softnet evidence = %#v", report.Softnet)
	}
	if len(report.Softnet.CredentialObservations) != 1 || report.Softnet.CredentialObservations[0].Samples != 3 || report.Softnet.CredentialObservations[0].Credentials != softnet.Credentials {
		t.Fatalf("credential observations = %#v", report.Softnet.CredentialObservations)
	}
	if !report.Limitations.SamplingNotLossless || report.Limitations.TransientRootPhaseDirectlyProven {
		t.Fatalf("limitations = %#v", report.Limitations)
	}
}

func TestObserverRecordsOptionalRootSampleBeforePostDropEvidence(t *testing.T) {
	expected := testExpected()
	tart := testTart()
	rootSoftnet := testSoftnet()
	rootSoftnet.StartTimeUnixMicros = 0
	rootSoftnet.Credentials = Credentials{
		EffectiveUID: 0, EffectiveGID: 20,
		RealUID: 501, RealGID: 20,
		SavedUID: 0, SavedGID: 20,
	}
	steadySoftnet := testSoftnet()
	sampler := &sequenceSampler{samples: []Sample{
		{Tart: tart, Children: []ProcessInfo{rootSoftnet}},
		{Tart: tart, Children: []ProcessInfo{steadySoftnet}},
		{Tart: tart, Children: []ProcessInfo{steadySoftnet}},
	}}

	report, err := (Observer{Sampler: sampler, MaxSamples: 3, RequiredSteadySamples: 2}).Observe(context.Background(), expected)
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	if report.Status != StatusPass || !report.Softnet.RootEffectiveObserved || report.Softnet.ConsecutiveSteadySamples != 2 {
		t.Fatalf("report = %#v", report)
	}
	if report.Limitations.TransientRootPhaseDirectlyProven {
		t.Fatal("one sampled root-effective tuple was reported as direct proof of the complete transient phase")
	}
}

func TestObserverReportsAndAppliesTheBoundedSamplingInterval(t *testing.T) {
	sampler := &sequenceSampler{samples: []Sample{
		{Tart: testTart()},
		{Tart: testTart(), Children: []ProcessInfo{testSoftnet()}},
	}}
	var sleeps []time.Duration
	report, err := (Observer{
		Sampler: sampler, MaxSamples: 2, RequiredSteadySamples: 1, Interval: 7 * time.Millisecond,
		Sleep: func(_ context.Context, interval time.Duration) error {
			sleeps = append(sleeps, interval)
			return nil
		},
	}).Observe(context.Background(), testExpected())
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	if len(sleeps) != 1 || sleeps[0] != 7*time.Millisecond {
		t.Fatalf("sleeps = %v", sleeps)
	}
	if report.Sampling.ConfiguredIntervalMicros != 7_000 || report.Sampling.MaximumAttempts != 2 || report.Sampling.RequiredConsecutiveSteady != 1 {
		t.Fatalf("sampling report = %#v", report.Sampling)
	}
	if !report.Limitations.UnobservedBetweenSamples || !report.Limitations.QueryAndSchedulingLatencyExcluded {
		t.Fatalf("race limitations = %#v", report.Limitations)
	}
}

func TestObserverFailsClosedOnAmbiguousOrChangingProcessEvidence(t *testing.T) {
	queryFailure := errors.New("proc_pidinfo failed")
	cases := []struct {
		name     string
		samples  []Sample
		errors   map[int]error
		max      int
		required int
		wantCode FailureCode
	}{
		{
			name: "root sample process unique ID changes before drop",
			samples: []Sample{
				{Tart: testTart(), Children: []ProcessInfo{func() ProcessInfo {
					child := testSoftnet()
					child.StartTimeUnixMicros = 0
					child.Credentials = Credentials{EffectiveUID: 0, EffectiveGID: 20, RealUID: 501, RealGID: 20, SavedUID: 0, SavedGID: 20}
					return child
				}()}},
				{Tart: testTart(), Children: []ProcessInfo{func() ProcessInfo {
					child := testSoftnet()
					child.UniqueID++
					return child
				}()}},
			},
			max: 2, required: 1, wantCode: FailureSoftnetIdentity,
		},
		{
			name: "root sample start time changes before drop",
			samples: []Sample{
				{Tart: testTart(), Children: []ProcessInfo{func() ProcessInfo {
					child := testSoftnet()
					child.Credentials = Credentials{EffectiveUID: 0, EffectiveGID: 20, RealUID: 501, RealGID: 20, SavedUID: 0, SavedGID: 20}
					return child
				}()}},
				{Tart: testTart(), Children: []ProcessInfo{func() ProcessInfo {
					child := testSoftnet()
					child.StartTimeUnixMicros++
					return child
				}()}},
			},
			max: 2, required: 1, wantCode: FailureSoftnetIdentity,
		},
		{
			name: "privileged sample has wrong executable",
			samples: []Sample{{Tart: testTart(), Children: []ProcessInfo{func() ProcessInfo {
				child := testSoftnet()
				child.StartTimeUnixMicros = 0
				child.Executable = "/unqualified/softnet"
				child.Credentials = Credentials{EffectiveUID: 0, EffectiveGID: 20, RealUID: 501, RealGID: 20, SavedUID: 0, SavedGID: 20}
				return child
			}()}}},
			max: 1, required: 1, wantCode: FailureSoftnetIdentity,
		},
		{
			name: "softnet parent unique ID does not bind Tart",
			samples: []Sample{{Tart: testTart(), Children: []ProcessInfo{func() ProcessInfo {
				child := testSoftnet()
				child.ParentUniqueID++
				return child
			}()}}},
			max: 1, required: 1, wantCode: FailureSoftnetAncestry,
		},
		{
			name: "wrong ancestry",
			samples: []Sample{{Tart: testTart(), Children: []ProcessInfo{func() ProcessInfo {
				child := testSoftnet()
				child.PPID = 999
				return child
			}()}}},
			max: 1, required: 1, wantCode: FailureSoftnetAncestry,
		},
		{
			name: "multiple direct children",
			samples: []Sample{{Tart: testTart(), Children: []ProcessInfo{testSoftnet(), func() ProcessInfo {
				child := testSoftnet()
				child.PID = 412
				return child
			}()}}},
			max: 1, required: 1, wantCode: FailureChildAmbiguous,
		},
		{
			name: "wrong executable",
			samples: []Sample{{Tart: testTart(), Children: []ProcessInfo{func() ProcessInfo {
				child := testSoftnet()
				child.Executable = "/unqualified/softnet"
				return child
			}()}}},
			max: 1, required: 1, wantCode: FailureSoftnetIdentity,
		},
		{
			name: "softnet PID reuse",
			samples: []Sample{
				{Tart: testTart(), Children: []ProcessInfo{testSoftnet()}},
				{Tart: testTart(), Children: []ProcessInfo{func() ProcessInfo { child := testSoftnet(); child.PID = 412; return child }()}},
			},
			max: 2, required: 2, wantCode: FailureSoftnetIdentity,
		},
		{
			name: "softnet start-time mismatch",
			samples: []Sample{
				{Tart: testTart(), Children: []ProcessInfo{testSoftnet()}},
				{Tart: testTart(), Children: []ProcessInfo{func() ProcessInfo { child := testSoftnet(); child.StartTimeUnixMicros++; return child }()}},
			},
			max: 2, required: 2, wantCode: FailureSoftnetIdentity,
		},
		{
			name: "tart start-time mismatch",
			samples: []Sample{
				{Tart: testTart(), Children: []ProcessInfo{testSoftnet()}},
				{Tart: func() ProcessInfo { process := testTart(); process.StartTimeUnixMicros++; return process }(), Children: []ProcessInfo{testSoftnet()}},
			},
			max: 2, required: 2, wantCode: FailureTartIdentity,
		},
		{
			name: "tart process unique ID mismatch",
			samples: []Sample{
				{Tart: testTart(), Children: []ProcessInfo{testSoftnet()}},
				{Tart: func() ProcessInfo { process := testTart(); process.UniqueID++; return process }(), Children: []ProcessInfo{testSoftnet()}},
			},
			max: 2, required: 2, wantCode: FailureTartIdentity,
		},
		{
			name: "unexpected credentials",
			samples: []Sample{{Tart: testTart(), Children: []ProcessInfo{func() ProcessInfo {
				child := testSoftnet()
				child.Credentials.EffectiveUID = 502
				return child
			}()}}},
			max: 1, required: 1, wantCode: FailureCredentials,
		},
		{
			name: "disappears before adequate evidence",
			samples: []Sample{
				{Tart: testTart(), Children: []ProcessInfo{testSoftnet()}},
				{Tart: testTart()},
			},
			max: 2, required: 2, wantCode: FailureChildDisappeared,
		},
		{
			name: "query error",
			samples: []Sample{
				{Tart: testTart(), Children: []ProcessInfo{testSoftnet()}},
				{},
			},
			errors: map[int]error{1: queryFailure},
			max:    2, required: 2, wantCode: FailureSampleQuery,
		},
		{
			name:    "sampling deadline",
			samples: []Sample{{}},
			errors:  map[int]error{0: context.DeadlineExceeded},
			max:     1, required: 1, wantCode: FailureTimeout,
		},
		{
			name: "timeout before child appears",
			samples: []Sample{
				{Tart: testTart()},
				{Tart: testTart()},
			},
			max: 2, required: 1, wantCode: FailureTimeout,
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			sampler := &sequenceSampler{samples: test.samples, errors: test.errors}
			report, err := (Observer{Sampler: sampler, MaxSamples: test.max, RequiredSteadySamples: test.required}).Observe(context.Background(), testExpected())
			if err == nil {
				t.Fatal("Observe() error = nil")
			}
			if report.Status != StatusFail || report.Failure != test.wantCode {
				t.Fatalf("report = %#v, want failure %q", report, test.wantCode)
			}
		})
	}
}

func testExpected() Expected {
	return Expected{TartPID: 410, TartExecutable: "/qualified/tart", SoftnetExecutable: "/qualified/softnet", OperatorUID: 501, OperatorGID: 20}
}

func testTart() ProcessInfo {
	return ProcessInfo{
		PID: 410, PPID: 100, PGID: 410,
		UniqueID:            90_410,
		ParentUniqueID:      90_100,
		StartTimeUnixMicros: 1_700_000_000_000_001,
		Executable:          "/qualified/tart",
		Credentials:         Credentials{EffectiveUID: 501, EffectiveGID: 20, RealUID: 501, RealGID: 20, SavedUID: 501, SavedGID: 20},
	}
}

func testSoftnet() ProcessInfo {
	return ProcessInfo{
		PID: 411, PPID: 410, PGID: 410,
		UniqueID:            90_411,
		ParentUniqueID:      90_410,
		StartTimeUnixMicros: 1_700_000_000_000_002,
		Executable:          "/qualified/softnet",
		Credentials:         Credentials{EffectiveUID: 501, EffectiveGID: 20, RealUID: 501, RealGID: 20, SavedUID: 501, SavedGID: 20},
	}
}

type sequenceSampler struct {
	samples []Sample
	errors  map[int]error
	next    int
}

func (s *sequenceSampler) Sample(_ context.Context, _ int) (Sample, error) {
	if err := s.errors[s.next]; err != nil {
		s.next++
		return Sample{}, err
	}
	if s.next >= len(s.samples) {
		return Sample{}, context.DeadlineExceeded
	}
	result := s.samples[s.next]
	s.next++
	return result, nil
}

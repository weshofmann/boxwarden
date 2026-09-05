// Package adr024 implements the unprivileged, qualification-only process
// observer for the ADR 024 Tart/Softnet attended gate. Production Boxwarden
// code must not import this package.
package adr024

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type Status string

const (
	StatusPass Status = "pass"
	StatusFail Status = "fail"
)

type FailureCode string

const (
	FailureInvalidConfiguration FailureCode = "invalid_configuration"
	FailureSampleQuery          FailureCode = "sample_query_failed"
	FailureTartIdentity         FailureCode = "tart_identity_mismatch"
	FailureChildAmbiguous       FailureCode = "child_ambiguous"
	FailureChildDisappeared     FailureCode = "child_disappeared"
	FailureSoftnetAncestry      FailureCode = "softnet_ancestry_mismatch"
	FailureSoftnetIdentity      FailureCode = "softnet_identity_mismatch"
	FailureCredentials          FailureCode = "credential_mismatch"
	FailureTimeout              FailureCode = "sampling_timeout"
	FailureUnsupported          FailureCode = "unsupported_platform"
	FailureHostState            FailureCode = "fixed_host_state_invalid"
	FailureHostStateChanged     FailureCode = "fixed_host_state_changed"
	FailureInvalidTartPID       FailureCode = "invalid_tart_pid"
	FailureOutputBound          FailureCode = "output_bound_exhausted"
)

type Credentials struct {
	EffectiveUID int `json:"effective_uid"`
	EffectiveGID int `json:"effective_gid"`
	RealUID      int `json:"real_uid"`
	RealGID      int `json:"real_gid"`
	SavedUID     int `json:"saved_uid"`
	SavedGID     int `json:"saved_gid"`
}

type ProcessInfo struct {
	PID                 int
	PPID                int
	PGID                int
	UniqueID            uint64
	ParentUniqueID      uint64
	StartTimeUnixMicros int64
	Executable          string
	Credentials         Credentials
}

type Sample struct {
	Tart     ProcessInfo
	Children []ProcessInfo
}

type Sampler interface {
	Sample(context.Context, int) (Sample, error)
}

type Expected struct {
	TartPID           int
	TartExecutable    string
	SoftnetExecutable string
	OperatorUID       int
	OperatorGID       int
}

type Limitations struct {
	SamplingNotLossless               bool `json:"sampling_not_lossless"`
	TransientRootPhaseDirectlyProven  bool `json:"transient_root_phase_directly_proven"`
	UnobservedBetweenSamples          bool `json:"unobserved_between_samples"`
	QueryAndSchedulingLatencyExcluded bool `json:"query_and_scheduling_latency_excluded"`
}

type SamplingEvidence struct {
	ConfiguredIntervalMicros  int64 `json:"configured_interval_micros"`
	MaximumAttempts           int   `json:"maximum_attempts"`
	RequiredConsecutiveSteady int   `json:"required_consecutive_steady"`
}

type ProcessEvidence struct {
	PID                 int         `json:"pid"`
	PPID                int         `json:"ppid"`
	PGID                int         `json:"pgid"`
	UniqueID            uint64      `json:"unique_id"`
	ParentUniqueID      uint64      `json:"parent_unique_id"`
	StartTimeUnixMicros int64       `json:"start_time_unix_micros"`
	Executable          string      `json:"executable"`
	ExecutableMatch     bool        `json:"executable_match"`
	Credentials         Credentials `json:"credentials"`
}

type CredentialObservation struct {
	Credentials Credentials `json:"credentials"`
	Samples     int         `json:"samples"`
}

type SoftnetEvidence struct {
	PID                      int                     `json:"pid"`
	PPID                     int                     `json:"ppid"`
	PGID                     int                     `json:"pgid"`
	UniqueID                 uint64                  `json:"unique_id"`
	ParentUniqueID           uint64                  `json:"parent_unique_id"`
	StartTimeUnixMicros      int64                   `json:"start_time_unix_micros"`
	Executable               string                  `json:"executable"`
	ExecutableMatch          bool                    `json:"executable_match"`
	RootEffectiveObserved    bool                    `json:"root_effective_observed"`
	ConsecutiveSteadySamples int                     `json:"consecutive_steady_samples"`
	CredentialObservations   []CredentialObservation `json:"credential_observations"`
}

type Report struct {
	SchemaVersion    int              `json:"schema_version"`
	Status           Status           `json:"status"`
	Failure          FailureCode      `json:"failure,omitempty"`
	SamplesAttempted int              `json:"samples_attempted"`
	Sampling         SamplingEvidence `json:"sampling"`
	FixedHostState   FixedState       `json:"fixed_host_state"`
	FixedStateStable bool             `json:"fixed_host_state_stable"`
	Tart             ProcessEvidence  `json:"tart"`
	Softnet          SoftnetEvidence  `json:"softnet"`
	Limitations      Limitations      `json:"limitations"`
}

type Observer struct {
	Sampler               Sampler
	MaxSamples            int
	RequiredSteadySamples int
	Interval              time.Duration
	Sleep                 func(context.Context, time.Duration) error
}

func (o Observer) Observe(ctx context.Context, expected Expected) (Report, error) {
	report := Report{
		SchemaVersion: 1,
		Status:        StatusFail,
		Limitations: Limitations{
			SamplingNotLossless:               true,
			UnobservedBetweenSamples:          true,
			QueryAndSchedulingLatencyExcluded: true,
		},
		Sampling: SamplingEvidence{
			ConfiguredIntervalMicros:  o.Interval.Microseconds(),
			MaximumAttempts:           o.MaxSamples,
			RequiredConsecutiveSteady: o.RequiredSteadySamples,
		},
	}
	if o.Sampler == nil || expected.TartPID <= 0 || o.MaxSamples <= 0 || o.RequiredSteadySamples <= 0 || o.RequiredSteadySamples > o.MaxSamples || o.Interval < 0 {
		return fail(report, FailureInvalidConfiguration, errors.New("invalid observer configuration"))
	}
	var tartIdentity ProcessInfo
	var softnetIdentity ProcessInfo
	var childObserved bool
	var postDropObserved bool
	for sampleNumber := 0; sampleNumber < o.MaxSamples; sampleNumber++ {
		report.SamplesAttempted++
		sample, err := o.Sampler.Sample(ctx, expected.TartPID)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
				return fail(report, FailureTimeout, fmt.Errorf("bounded sampling ended: %w", err))
			}
			return fail(report, FailureSampleQuery, fmt.Errorf("sample process state: %w", err))
		}
		if !validTart(sample.Tart, expected) {
			return fail(report, FailureTartIdentity, errors.New("Tart process does not match fixed qualified identity"))
		}
		if tartIdentity.PID == 0 {
			tartIdentity = sample.Tart
			report.Tart = evidenceFor(sample.Tart, expected.TartExecutable)
		} else if !sameProcessIdentity(tartIdentity, sample.Tart) {
			return fail(report, FailureTartIdentity, errors.New("Tart process identity changed while sampling"))
		}
		switch len(sample.Children) {
		case 0:
			if childObserved {
				return fail(report, FailureChildDisappeared, errors.New("Softnet child disappeared before adequate evidence"))
			}
			if err := o.pause(ctx); err != nil {
				return fail(report, FailureTimeout, err)
			}
			continue
		case 1:
		default:
			return fail(report, FailureChildAmbiguous, errors.New("Tart has multiple direct child processes"))
		}
		softnet := sample.Children[0]
		if softnet.PID <= 0 || softnet.PPID != expected.TartPID || softnet.ParentUniqueID == 0 || softnet.ParentUniqueID != sample.Tart.UniqueID {
			return fail(report, FailureSoftnetAncestry, errors.New("Softnet child does not have the exact Tart parent"))
		}
		if softnet.UniqueID == 0 || softnet.Executable != expected.SoftnetExecutable {
			return fail(report, FailureSoftnetIdentity, errors.New("Softnet process does not match the fixed qualified identity"))
		}
		if !childObserved {
			softnetIdentity = ProcessInfo{
				PID: softnet.PID, PPID: softnet.PPID, PGID: softnet.PGID,
				UniqueID: softnet.UniqueID, ParentUniqueID: softnet.ParentUniqueID,
				StartTimeUnixMicros: softnet.StartTimeUnixMicros, Executable: softnet.Executable,
			}
			childObserved = true
			report.Softnet.PID = softnet.PID
			report.Softnet.PPID = softnet.PPID
			report.Softnet.PGID = softnet.PGID
			report.Softnet.UniqueID = softnet.UniqueID
			report.Softnet.ParentUniqueID = softnet.ParentUniqueID
			report.Softnet.StartTimeUnixMicros = softnet.StartTimeUnixMicros
			report.Softnet.Executable = softnet.Executable
			report.Softnet.ExecutableMatch = true
		} else if !sameSoftnetIdentity(softnetIdentity, softnet) {
			return fail(report, FailureSoftnetIdentity, errors.New("Softnet process identity changed while sampling"))
		}
		if softnetIdentity.StartTimeUnixMicros == 0 && softnet.StartTimeUnixMicros > 0 {
			softnetIdentity.StartTimeUnixMicros = softnet.StartTimeUnixMicros
			report.Softnet.StartTimeUnixMicros = softnet.StartTimeUnixMicros
		}
		if privilegedCredentials(softnet.Credentials, expected.OperatorUID, expected.OperatorGID) {
			recordCredentials(&report.Softnet, softnet.Credentials)
			if postDropObserved {
				return fail(report, FailureCredentials, errors.New("Softnet returned to root-effective credentials after post-drop observation"))
			}
			report.Softnet.RootEffectiveObserved = true
			if err := o.pause(ctx); err != nil {
				return fail(report, FailureTimeout, err)
			}
			continue
		}
		if !steadyCredentials(softnet.Credentials, expected.OperatorUID, expected.OperatorGID) {
			return fail(report, FailureCredentials, errors.New("Softnet credentials are neither accepted root-effective nor post-drop state"))
		}
		recordCredentials(&report.Softnet, softnet.Credentials)
		if softnet.StartTimeUnixMicros <= 0 {
			return fail(report, FailureSoftnetIdentity, errors.New("Softnet executable or start time does not match fixed qualified identity"))
		}
		if !postDropObserved {
			report.Softnet.StartTimeUnixMicros = softnet.StartTimeUnixMicros
			postDropObserved = true
		}
		report.Softnet.ConsecutiveSteadySamples++
		if report.Softnet.ConsecutiveSteadySamples == o.RequiredSteadySamples {
			report.Status = StatusPass
			return report, nil
		}
		if err := o.pause(ctx); err != nil {
			return fail(report, FailureTimeout, err)
		}
	}
	return fail(report, FailureTimeout, errors.New("insufficient steady-state samples before bounded timeout"))
}

func (o Observer) pause(ctx context.Context) error {
	if o.Interval < 0 {
		return errors.New("sampling interval must not be negative")
	}
	if o.Interval == 0 {
		return nil
	}
	if o.Sleep != nil {
		return o.Sleep(ctx, o.Interval)
	}
	timer := time.NewTimer(o.Interval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func evidenceFor(process ProcessInfo, expectedExecutable string) ProcessEvidence {
	return ProcessEvidence{
		PID: process.PID, PPID: process.PPID, PGID: process.PGID,
		UniqueID:            process.UniqueID,
		ParentUniqueID:      process.ParentUniqueID,
		StartTimeUnixMicros: process.StartTimeUnixMicros,
		Executable:          process.Executable,
		ExecutableMatch:     process.Executable == expectedExecutable,
		Credentials:         process.Credentials,
	}
}

func recordCredentials(evidence *SoftnetEvidence, credentials Credentials) {
	for index := range evidence.CredentialObservations {
		if evidence.CredentialObservations[index].Credentials == credentials {
			evidence.CredentialObservations[index].Samples++
			return
		}
	}
	evidence.CredentialObservations = append(evidence.CredentialObservations, CredentialObservation{Credentials: credentials, Samples: 1})
}

func validTart(process ProcessInfo, expected Expected) bool {
	return process.PID == expected.TartPID && process.PPID > 0 && process.PGID > 0 && process.UniqueID > 0 && process.ParentUniqueID > 0 && process.StartTimeUnixMicros > 0 &&
		process.Executable == expected.TartExecutable && steadyCredentials(process.Credentials, expected.OperatorUID, expected.OperatorGID)
}

func sameProcessIdentity(left, right ProcessInfo) bool {
	return left.PID == right.PID && left.PPID == right.PPID && left.PGID == right.PGID &&
		left.UniqueID == right.UniqueID && left.ParentUniqueID == right.ParentUniqueID &&
		left.StartTimeUnixMicros == right.StartTimeUnixMicros && left.Executable == right.Executable
}

func sameSoftnetIdentity(left, right ProcessInfo) bool {
	if left.PID != right.PID || left.PPID != right.PPID || left.PGID != right.PGID ||
		left.UniqueID != right.UniqueID || left.ParentUniqueID != right.ParentUniqueID || left.Executable != right.Executable {
		return false
	}
	return left.StartTimeUnixMicros == 0 || right.StartTimeUnixMicros == 0 || left.StartTimeUnixMicros == right.StartTimeUnixMicros
}

func fail(report Report, code FailureCode, err error) (Report, error) {
	report.Status = StatusFail
	report.Failure = code
	return report, err
}

func steadyCredentials(credentials Credentials, uid, gid int) bool {
	return credentials.EffectiveUID == uid && credentials.RealUID == uid && credentials.SavedUID == uid &&
		credentials.EffectiveGID == gid && credentials.RealGID == gid && credentials.SavedGID == gid
}

func privilegedCredentials(credentials Credentials, uid, gid int) bool {
	return credentials.EffectiveUID == 0 && credentials.RealUID == uid && credentials.SavedUID == 0 &&
		credentials.EffectiveGID == gid && credentials.RealGID == gid && credentials.SavedGID == gid
}

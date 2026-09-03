package adr024

import (
	"context"
	"errors"
	"reflect"
	"time"
)

const (
	productionSampleInterval = 5 * time.Millisecond
	productionMaxSamples     = 6_000
	productionSteadySamples  = 100
	productionTimeout        = 45 * time.Second
)

// ObserveFixed runs only the unprivileged sampling observer against the exact
// supplied Tart PID. It neither launches nor signals a process and revalidates
// the fixed host artifacts after sampling before it can return a passing report.
func ObserveFixed(ctx context.Context, tartPID int) (Report, error) {
	sampler, err := newSystemSampler()
	if err != nil {
		return externalFailure(FailureUnsupported), err
	}
	before, err := LoadFixedState(tartPID)
	if err != nil {
		return externalFailure(FailureHostState), err
	}
	bounded, cancel := context.WithTimeout(ctx, productionTimeout)
	defer cancel()
	report, observeErr := (Observer{
		Sampler: sampler, MaxSamples: productionMaxSamples, RequiredSteadySamples: productionSteadySamples, Interval: productionSampleInterval,
	}).Observe(bounded, before.Expected)
	report.FixedHostState = before
	if observeErr != nil {
		return report, observeErr
	}
	after, err := LoadFixedState(tartPID)
	if err != nil {
		report.Status = StatusFail
		report.Failure = FailureHostStateChanged
		return report, err
	}
	if !reflect.DeepEqual(before, after) {
		report.Status = StatusFail
		report.Failure = FailureHostStateChanged
		return report, errors.New("fixed host state changed while observing")
	}
	report.FixedStateStable = true
	return report, nil
}

func externalFailure(code FailureCode) Report {
	return Report{
		SchemaVersion: 1, Status: StatusFail, Failure: code,
		Limitations: Limitations{
			SamplingNotLossless:               true,
			UnobservedBetweenSamples:          true,
			QueryAndSchedulingLatencyExcluded: true,
		},
	}
}

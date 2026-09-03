package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/weshofmann/boxwarden/internal/qualification/adr024"
)

func TestParseAcceptsOnlyOneExactTartPID(t *testing.T) {
	pid, err := parse([]string{"--tart-pid", "410"})
	if err != nil || pid != 410 {
		t.Fatalf("parse() = %d, %v", pid, err)
	}
	for _, arguments := range [][]string{
		{},
		{"--tart-pid", "0"},
		{"--tart-pid", "not-a-pid"},
		{"--process-name", "tart"},
		{"--tart-pid", "410", "--softnet-path", "/tmp/softnet"},
		{"--tart-pid=410"},
	} {
		if _, err := parse(arguments); err == nil {
			t.Fatalf("parse(%q) error = nil", arguments)
		}
	}
}

func TestExecuteWritesOneBoundedJSONReport(t *testing.T) {
	var output bytes.Buffer
	exitCode := execute(context.Background(), []string{"--tart-pid", "410"}, &output, func(context.Context, int) (adr024.Report, error) {
		return adr024.Report{SchemaVersion: 1, Status: adr024.StatusPass}, nil
	})
	if exitCode != 0 {
		t.Fatalf("execute() = %d", exitCode)
	}
	if output.Len() > maxReportBytes || bytes.Count(output.Bytes(), []byte("\n")) != 1 || !bytes.HasSuffix(output.Bytes(), []byte("\n")) {
		t.Fatalf("output length/newlines = %d/%d", output.Len(), bytes.Count(output.Bytes(), []byte("\n")))
	}
	var report adr024.Report
	if err := json.Unmarshal(output.Bytes(), &report); err != nil || report.Status != adr024.StatusPass {
		t.Fatalf("report = %#v, %v", report, err)
	}
}

func TestExecuteFailsClosedWithBoundedJSONOnObservationAndOutputErrors(t *testing.T) {
	t.Run("observation failure", func(t *testing.T) {
		var output bytes.Buffer
		exitCode := execute(context.Background(), []string{"--tart-pid", "410"}, &output, func(context.Context, int) (adr024.Report, error) {
			return adr024.Report{SchemaVersion: 1, Status: adr024.StatusFail, Failure: adr024.FailureSampleQuery}, errors.New("private raw error")
		})
		if exitCode != 1 || strings.Contains(output.String(), "private raw error") {
			t.Fatalf("exit/output = %d/%q", exitCode, output.String())
		}
	})

	t.Run("oversized report", func(t *testing.T) {
		var output bytes.Buffer
		exitCode := execute(context.Background(), []string{"--tart-pid", "410"}, &output, func(context.Context, int) (adr024.Report, error) {
			return adr024.Report{
				SchemaVersion: 1, Status: adr024.StatusPass,
				Softnet: adr024.SoftnetEvidence{Executable: strings.Repeat("x", maxReportBytes*2)},
			}, nil
		})
		var report adr024.Report
		if exitCode != 1 || output.Len() > maxReportBytes || json.Unmarshal(output.Bytes(), &report) != nil || report.Failure != adr024.FailureOutputBound {
			t.Fatalf("exit/len/report = %d/%d/%#v", exitCode, output.Len(), report)
		}
	})
}

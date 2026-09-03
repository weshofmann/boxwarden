// Command observe is the unprivileged, qualification-only ADR 024 process
// observer. It is intentionally outside the normal boxwarden command tree.
package main

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"os"
	"strconv"

	"github.com/weshofmann/boxwarden/internal/qualification/adr024"
)

const maxReportBytes = 16 << 10

type observeFunc func(context.Context, int) (adr024.Report, error)

func main() {
	os.Exit(execute(context.Background(), os.Args[1:], os.Stdout, adr024.ObserveFixed))
}

func execute(ctx context.Context, arguments []string, output io.Writer, observe observeFunc) int {
	pid, err := parse(arguments)
	if err != nil {
		report := adr024.Report{SchemaVersion: 1, Status: adr024.StatusFail, Failure: adr024.FailureInvalidTartPID}
		writeBoundedReport(output, report)
		return 1
	}
	report, observeErr := observe(ctx, pid)
	writtenExactly := writeBoundedReport(output, report)
	if observeErr != nil || report.Status != adr024.StatusPass || !writtenExactly {
		return 1
	}
	return 0
}

func parse(arguments []string) (int, error) {
	if len(arguments) != 2 || arguments[0] != "--tart-pid" {
		return 0, strconv.ErrSyntax
	}
	pid, err := strconv.ParseInt(arguments[1], 10, 32)
	if err != nil || pid <= 0 || pid > math.MaxInt32 {
		return 0, strconv.ErrSyntax
	}
	return int(pid), nil
}

func writeBoundedReport(output io.Writer, report adr024.Report) bool {
	raw, err := json.Marshal(report)
	exact := err == nil && len(raw)+1 <= maxReportBytes
	if !exact {
		report = adr024.Report{SchemaVersion: 1, Status: adr024.StatusFail, Failure: adr024.FailureOutputBound}
		raw, _ = json.Marshal(report)
	}
	raw = append(raw, '\n')
	if _, err := output.Write(raw); err != nil {
		return false
	}
	return exact
}

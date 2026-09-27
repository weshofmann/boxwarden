package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/weshofmann/boxwarden/internal/workspaceformat"
)

type guestReport struct {
	Version     int                             `json:"version"`
	Transaction string                          `json:"transaction"`
	Success     bool                            `json:"success"`
	Evidence    *workspaceformat.FormatEvidence `json:"evidence,omitempty"`
	Error       string                          `json:"error,omitempty"`
}

func writeSerialReport(output io.Writer, report []byte) error {
	if len(report) == 0 || len(report) > 4095 || bytes.ContainsAny(report, "\r\n") {
		return fmt.Errorf("invalid formatter serial report")
	}
	frame := append(append([]byte(nil), report...), '\n')
	for len(frame) > 0 {
		count, err := output.Write(frame)
		if err != nil {
			return err
		}
		if count <= 0 || count > len(frame) {
			return io.ErrShortWrite
		}
		frame = frame[count:]
	}
	return nil
}

func runFormatter(ctx context.Context, commandLine string, format func(context.Context, workspaceformat.GuestFormatRequest) (workspaceformat.FormatEvidence, error)) ([]byte, error) {
	request, err := parseBootRequest(commandLine)
	if err != nil {
		return nil, err
	}
	if format == nil {
		return nil, fmt.Errorf("formatter function missing")
	}
	report := guestReport{Version: 1, Transaction: request.transaction}
	evidence, err := format(ctx, request.format)
	if err != nil {
		report.Error = err.Error()
		if len(report.Error) > 256 {
			report.Error = report.Error[:256]
		}
	} else if evidence.ObservedUUID != request.format.FilesystemUUID || !evidence.WholeDevice || !evidence.FilesystemClean {
		report.Error = "formatter returned invalid evidence"
	} else {
		report.Success = true
		report.Evidence = &evidence
	}
	return json.Marshal(report)
}

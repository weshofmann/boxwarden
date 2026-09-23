package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/weshofmann/boxwarden/internal/workspaceformat"
)

func TestGuestReportBindsTransactionAndVerifiedResult(t *testing.T) {
	raw, err := runFormatter(context.Background(), goodBootLine, func(_ context.Context, request workspaceformat.GuestFormatRequest) (workspaceformat.FormatEvidence, error) {
		if request.SizeBytes != 67108864 || request.Marker != strings.Repeat("ab", 32) {
			t.Fatalf("wrong request: %+v", request)
		}
		return workspaceformat.FormatEvidence{ObservedUUID: request.FilesystemUUID, WholeDevice: true, FilesystemClean: true}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var report guestReport
	if err := json.Unmarshal(raw, &report); err != nil {
		t.Fatal(err)
	}
	if report.Version != 1 || report.Transaction != "0123456789abcdef0123456789abcdef" || !report.Success || report.Evidence == nil || !report.Evidence.WholeDevice || !report.Evidence.FilesystemClean || report.Evidence.ObservedUUID != "12345678-1234-1234-1234-123456789abc" || report.Error != "" {
		t.Fatalf("invalid report: %+v", report)
	}
}

func TestGuestReportCarriesBoundedFailureWithoutEvidence(t *testing.T) {
	raw, err := runFormatter(context.Background(), goodBootLine, func(context.Context, workspaceformat.GuestFormatRequest) (workspaceformat.FormatEvidence, error) {
		return workspaceformat.FormatEvidence{}, errors.New(strings.Repeat("x", 1000))
	})
	if err != nil {
		t.Fatal(err)
	}
	var report guestReport
	if err := json.Unmarshal(raw, &report); err != nil {
		t.Fatal(err)
	}
	if report.Success || report.Evidence != nil || len(report.Error) != 256 || report.Transaction != "0123456789abcdef0123456789abcdef" {
		t.Fatalf("invalid failure report: %+v", report)
	}
}

func TestInvalidGuestBootDoesNotFormat(t *testing.T) {
	called := false
	_, err := runFormatter(context.Background(), goodBootLine+" alpha_tx=0123456789abcdef0123456789abcdef", func(context.Context, workspaceformat.GuestFormatRequest) (workspaceformat.FormatEvidence, error) {
		called = true
		return workspaceformat.FormatEvidence{}, nil
	})
	if err == nil || called {
		t.Fatalf("invalid boot request reached formatter: err=%v called=%t", err, called)
	}
}

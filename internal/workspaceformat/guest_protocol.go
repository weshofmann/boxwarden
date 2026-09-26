package workspaceformat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
)

const maxGuestRequestBytes = 1024

// RunGuestHelper is the bounded stdin/stdout protocol for a private binary
// inside the fresh formatter VM. Its successful response is still subject to
// the host adapter's exact stop/reap proof and host superblock check.
func RunGuestHelper(ctx context.Context, input io.Reader, output io.Writer) error {
	return runGuestHelper(ctx, input, output, FormatInLinuxGuest)
}

func runGuestHelper(ctx context.Context, input io.Reader, output io.Writer, format func(context.Context, GuestFormatRequest) (FormatEvidence, error)) error {
	if input == nil || output == nil || format == nil {
		return fmt.Errorf("guest formatter protocol is incomplete")
	}
	raw, err := io.ReadAll(io.LimitReader(input, maxGuestRequestBytes+1))
	if err != nil {
		return fmt.Errorf("read guest formatter request: %w", err)
	}
	if len(raw) > maxGuestRequestBytes {
		return fmt.Errorf("guest formatter request is oversized")
	}
	if err := rejectDuplicateJSON(raw); err != nil {
		return fmt.Errorf("guest formatter request is ambiguous: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var request GuestFormatRequest
	if err := decoder.Decode(&request); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("guest formatter request has trailing content")
	}
	evidence, err := format(ctx, request)
	if err != nil {
		return err
	}
	if err := json.NewEncoder(output).Encode(evidence); err != nil {
		return err
	}
	return nil
}

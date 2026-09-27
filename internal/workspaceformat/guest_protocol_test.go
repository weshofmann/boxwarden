package workspaceformat

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestGuestHelperProtocolPreservesExactRequestAndEvidence(t *testing.T) {
	input := `{"filesystem_uuid":"` + testFSUUID + `","size_bytes":16777216,"marker":"` + testMarker + `"}`
	var output bytes.Buffer
	called := false
	err := runGuestHelper(t.Context(), strings.NewReader(input), &output, func(_ context.Context, request GuestFormatRequest) (FormatEvidence, error) {
		called = true
		if request != testGuestRequest() {
			t.Fatalf("guest request = %+v", request)
		}
		return FormatEvidence{ObservedUUID: request.FilesystemUUID, WholeDevice: true, FilesystemClean: true}, nil
	})
	if err != nil || !called || output.String() != `{"observed_uuid":"`+testFSUUID+`","whole_device":true,"filesystem_clean":true}`+"\n" {
		t.Fatalf("guest protocol = %q, called=%v, err=%v", output.String(), called, err)
	}
}

func TestGuestHelperProtocolRejectsAmbiguousOrOversizedRequestsBeforeFormatting(t *testing.T) {
	for _, input := range []string{
		`{"marker":"a","marker":"b"}`,
		`{"filesystem_uuid":"x","unknown":true}`,
		`{"marker":"x"} {"marker":"y"}`,
		strings.Repeat("x", maxGuestRequestBytes+1),
	} {
		called := false
		var output bytes.Buffer
		err := runGuestHelper(t.Context(), strings.NewReader(input), &output, func(context.Context, GuestFormatRequest) (FormatEvidence, error) {
			called = true
			return FormatEvidence{}, nil
		})
		if err == nil || called || output.Len() != 0 {
			t.Fatalf("invalid request reached formatter: err=%v called=%v output=%q", err, called, output.String())
		}
	}
}

package main

import (
	"strings"
	"testing"
)

var goodBootLine = "console=hvc0 rdinit=/alpha-formatter alpha_tx=0123456789abcdef0123456789abcdef alpha_uuid=12345678-1234-1234-1234-123456789abc alpha_size=67108864 alpha_marker=" + strings.Repeat("ab", 32)

func TestParseBootRequestExactFields(t *testing.T) {
	request, err := parseBootRequest(goodBootLine)
	if err != nil {
		t.Fatal(err)
	}
	if request.transaction != "0123456789abcdef0123456789abcdef" || request.format.FilesystemUUID != "12345678-1234-1234-1234-123456789abc" || request.format.SizeBytes != 67108864 || request.format.Marker != strings.Repeat("ab", 32) {
		t.Fatalf("wrong formatter boot request: %+v", request)
	}
}

func TestParseBootRequestRejectsAmbiguityAndMalformedValues(t *testing.T) {
	for _, line := range []string{
		goodBootLine + " alpha_size=67108864",
		strings.Replace(goodBootLine, "alpha_size=67108864", "alpha_size=-1", 1),
		strings.Replace(goodBootLine, "alpha_uuid=12345678-1234-1234-1234-123456789abc", "alpha_uuid=wrong", 1),
		strings.Replace(goodBootLine, "alpha_tx=0123456789abcdef0123456789abcdef", "alpha_tx=0", 1),
		strings.Replace(goodBootLine, "alpha_tx=0123456789abcdef0123456789abcdef", "alpha_tx="+strings.Repeat("0", 32), 1),
		strings.Replace(goodBootLine, "alpha_marker="+strings.Repeat("ab", 32), "alpha_marker=abc", 1),
		goodBootLine + " alpha_extra=surprise",
		strings.Replace(goodBootLine, " alpha_size=67108864", "", 1),
	} {
		if _, err := parseBootRequest(line); err == nil {
			t.Fatalf("accepted malformed boot line %q", line)
		}
	}
}

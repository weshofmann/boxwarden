package workspaceformat

import (
	"context"
	"testing"
)

func TestPrebuiltSupportRejectsUncleanOrAbsentResources(t *testing.T) {
	for _, p := range []string{"relative", "/private/../tmp", "/not-present/prebuilt"} {
		if digest, err := CheckPrebuiltSupport(context.Background(), "/not-present/source", p); err == nil || digest != "" {
			t.Fatalf("unadmitted resources returned fingerprint: %q %v", digest, err)
		}
	}
}

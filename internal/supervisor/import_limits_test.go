package supervisor

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/weshofmann/boxwarden/internal/importx"
)

func TestImportControlLargerMeasuredReceipt(t *testing.T) {
	for _, tc := range []struct {
		name  string
		files int
		bytes int64
		want  bool
	}{
		{"larger project", 1003, 48383040, true},
		{"source boundaries", importx.MaxFiles, importx.MaxTotalBytes, true},
		{"extra file", importx.MaxFiles + 1, 0, false},
		{"extra byte", 4, importx.MaxTotalBytes + 1, false},
		{"empty file selection", 0, 0, false},
		{"negative bytes", 1, -1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := minimalRequest(t)
			path, _, err := publishOrAdmitRequest(request)
			if err != nil {
				t.Fatal(err)
			}
			spec := ImportTransfer{TransactionID: "039179af-8411-4790-9587-890922080236", SourceDigest: strings.Repeat("a", 64),
				VolumeID: "a7d43c10-344e-4617-812d-c8a1c7253631", FilesystemUUID: "691ec498-707b-47f0-a0f2-1869259068ed", MountPath: "/home/boxwarden/workspaces/project"}
			owner := &importRuntimeFixture{runtimeFixture: runtimeFixture{done: make(chan struct{})}, result: ImportResult{Digest: spec.SourceDigest, FileCount: tc.files, TotalBytes: tc.bytes, RemotePath: spec.MountPath + "/boxwarden-import-" + spec.TransactionID}}
			ctx, cancel := context.WithCancel(t.Context())
			done := make(chan error, 1)
			go func() { done <- Run(ctx, path, owner) }()
			t.Cleanup(func() {
				cancel()
				if err := <-done; err != nil && !errors.Is(err, context.Canceled) {
					t.Error(err)
				}
			})
			client := &Client{RuntimeDirectory: request.RuntimeDirectory, MaxSnapshotAge: time.Minute}
			if _, err := awaitSnapshot(ctx, request.Binding, startupPolicy{timeout: time.Second, interval: time.Millisecond}, client.Snapshot); err != nil {
				t.Fatal(err)
			}
			result, err := client.TransferImport(ctx, request.Binding, spec)
			if (err == nil) != tc.want || owner.imports.Load() != 1 {
				t.Fatalf("result=%#v calls=%d err=%v", result, owner.imports.Load(), err)
			}
			if tc.want && result != owner.result {
				t.Fatalf("changed measured receipt: %#v", result)
			}
		})
	}
}

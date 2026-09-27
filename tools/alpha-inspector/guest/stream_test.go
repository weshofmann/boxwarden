package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/weshofmann/boxwarden/internal/exportx"
)

func TestWriteReportProducesReceivableBoundedStream(t *testing.T) {
	transaction := [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	const body = `{"disk_prefix_sha256":"abcd","network_interfaces":["lo"],"read_only":true}`
	var stream bytes.Buffer
	if err := writeReport(&stream, transaction, []byte(body)); err != nil {
		t.Fatal(err)
	}
	parent := filepath.Join(t.TempDir(), "exports")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	path, err := exportx.Receive(context.Background(), io.NopCloser(bytes.NewReader(stream.Bytes())), exportx.Options{
		Parent: parent, TransactionID: transaction, MaxChunkBytes: 4096,
		MaxFileBytes: 4096, MaxTotalBytes: 4096, MaxFiles: 1,
		MaxDirectories: 1, MinFreeBytes: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(parent, hex.EncodeToString(transaction[:])) {
		t.Fatalf("unexpected destination %q", path)
	}
	data, err := os.ReadFile(filepath.Join(path, "report.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != body {
		t.Fatalf("received report = %q", data)
	}
	if got := sha256.Sum256(data); got != sha256.Sum256([]byte(body)) {
		t.Fatal("received report digest changed")
	}
}

func TestParseTransactionRejectsMissingDuplicateAndInvalid(t *testing.T) {
	for _, commandLine := range []string{
		"console=hvc0",
		"alpha_tx=deadbeef",
		"alpha_tx=00000000000000000000000000000000",
		"alpha_tx=0102030405060708090a0b0c0d0e0f10 alpha_tx=0102030405060708090a0b0c0d0e0f10",
	} {
		if _, err := parseTransaction(commandLine); err == nil {
			t.Fatalf("accepted invalid command line %q", commandLine)
		}
	}
	want := [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	got, err := parseTransaction("console=hvc0 rdinit=/alpha-probe alpha_tx=0102030405060708090a0b0c0d0e0f10")
	if err != nil || got != want {
		t.Fatalf("transaction = %x, %v", got, err)
	}
}

package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestDecodeExportRequestBindsTransactionAndSelection(t *testing.T) {
	tx := [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	raw := []byte(`{"version":1,"transaction":"0102030405060708090a0b0c0d0e0f10","filesystem_uuid":"2f1c6b88-9849-4c5d-9d20-f3bc30bd77a1","disk_bytes":67108864,"selected":["project/report.txt"]}`)
	request, err := decodeExportRequest(raw, tx)
	if err != nil || request.DiskBytes != 64<<20 || len(request.Selected) != 1 || request.Selected[0] != "project/report.txt" {
		t.Fatalf("request = %#v, %v", request, err)
	}
	for _, bad := range [][]byte{
		bytes.Replace(raw, []byte(`"version":1`), []byte(`"version":1,"version":1`), 1),
		bytes.Replace(raw, []byte(`"transaction":"0102030405060708090a0b0c0d0e0f10"`), []byte(`"transaction":"00000000000000000000000000000000"`), 1),
		bytes.Replace(raw, []byte(`"disk_bytes":67108864`), []byte(`"disk_bytes":1099511627776`), 1),
		bytes.Replace(raw, []byte(`"selected":["project/report.txt"]`), []byte(`"selected":["../escape"]`), 1),
		append(append([]byte(nil), raw...), []byte(`{}`)...),
		[]byte(strings.Repeat("x", maxExportRequestBytes+1)),
	} {
		if _, err := decodeExportRequest(bad, tx); err == nil {
			t.Fatalf("unsafe request accepted: %q", bad[:min(len(bad), 180)])
		}
	}
}

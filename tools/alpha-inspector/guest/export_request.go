package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
)

// The durable host journal is capped at 64 KiB. The request adds its own
// envelope around the same selection and therefore allows 128 KiB.
const maxExportRequestBytes = 128 << 10

var exportUUIDPattern = regexp.MustCompile(`^[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}$`)

type guestExportRequest struct {
	FilesystemUUID string
	DiskBytes      int64
	Selected       []string
}

// decodeExportRequest admits transaction-specific data placed by the trusted
// host in the inspector initramfs. The kernel command line supplies the same
// transaction independently; a mismatched or ambiguous request fails closed.
func decodeExportRequest(raw []byte, transaction [16]byte) (guestExportRequest, error) {
	if transaction == [16]byte{} || len(raw) == 0 || len(raw) > maxExportRequestBytes {
		return guestExportRequest{}, fmt.Errorf("invalid export request size or transaction")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return guestExportRequest{}, fmt.Errorf("invalid export request object: %v", err)
	}
	fields := make(map[string]json.RawMessage, 5)
	for decoder.More() {
		nameToken, err := decoder.Token()
		name, ok := nameToken.(string)
		if err != nil || !ok {
			return guestExportRequest{}, fmt.Errorf("invalid export request field: %v", err)
		}
		if _, exists := fields[name]; exists {
			return guestExportRequest{}, fmt.Errorf("duplicate export request field")
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return guestExportRequest{}, err
		}
		fields[name] = value
	}
	if token, err = decoder.Token(); err != nil || token != json.Delim('}') {
		return guestExportRequest{}, fmt.Errorf("invalid export request ending: %v", err)
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return guestExportRequest{}, fmt.Errorf("trailing export request data")
	}
	if len(fields) != 5 {
		return guestExportRequest{}, fmt.Errorf("incomplete or unknown export request fields")
	}
	var version int
	var requestTransaction string
	var result guestExportRequest
	for name, target := range map[string]any{
		"version": &version, "transaction": &requestTransaction,
		"filesystem_uuid": &result.FilesystemUUID, "disk_bytes": &result.DiskBytes,
		"selected": &result.Selected,
	} {
		value, ok := fields[name]
		if !ok || bytes.Equal(value, []byte("null")) || json.Unmarshal(value, target) != nil {
			return guestExportRequest{}, fmt.Errorf("invalid export request field %s", name)
		}
	}
	if version != 1 || requestTransaction != hex.EncodeToString(transaction[:]) ||
		!exportUUIDPattern.MatchString(result.FilesystemUUID) ||
		result.DiskBytes < 4096 || result.DiskBytes > 1<<30 || result.DiskBytes%512 != 0 ||
		len(result.Selected) == 0 || len(result.Selected) > 1024 {
		return guestExportRequest{}, fmt.Errorf("export request is outside alpha policy")
	}
	seen := make(map[string]bool, len(result.Selected))
	for _, name := range result.Selected {
		if !validGuestExportPath(name) || seen[strings.ToLower(name)] {
			return guestExportRequest{}, fmt.Errorf("invalid or duplicate export selection")
		}
		seen[strings.ToLower(name)] = true
	}
	return result, nil
}

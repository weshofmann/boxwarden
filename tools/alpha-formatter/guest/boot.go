package main

import (
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	"github.com/weshofmann/boxwarden/internal/workspaceformat"
)

type bootRequest struct {
	transaction string
	format      workspaceformat.GuestFormatRequest
}

func parseBootRequest(commandLine string) (bootRequest, error) {
	values := make(map[string]string, 5)
	for _, field := range strings.Fields(commandLine) {
		key, value, ok := strings.Cut(field, "=")
		if !ok {
			if strings.HasPrefix(field, "alpha_") {
				return bootRequest{}, fmt.Errorf("unrecognized formatter boot field")
			}
			continue
		}
		if key != "console" && key != "rdinit" && key != "alpha_tx" && key != "alpha_uuid" && key != "alpha_size" && key != "alpha_marker" {
			if strings.HasPrefix(key, "alpha_") {
				return bootRequest{}, fmt.Errorf("unrecognized formatter boot field")
			}
			continue
		}
		if _, duplicate := values[key]; duplicate {
			return bootRequest{}, fmt.Errorf("duplicate formatter boot field %q", key)
		}
		values[key] = value
	}
	if len(values) != 6 || values["console"] != "hvc0" || values["rdinit"] != "/alpha-formatter" || !lowerHex(values["alpha_tx"], 32) || values["alpha_tx"] == strings.Repeat("0", 32) || !canonicalUUID(values["alpha_uuid"]) || !lowerHex(values["alpha_marker"], 64) {
		return bootRequest{}, fmt.Errorf("incomplete or invalid formatter boot request")
	}
	size, err := strconv.ParseInt(values["alpha_size"], 10, 64)
	if err != nil || size < 4096 || size > 1<<43 || size%512 != 0 {
		return bootRequest{}, fmt.Errorf("invalid formatter disk size")
	}
	return bootRequest{
		transaction: values["alpha_tx"],
		format: workspaceformat.GuestFormatRequest{
			FilesystemUUID: values["alpha_uuid"],
			SizeBytes:      size,
			Marker:         values["alpha_marker"],
		},
	}, nil
}

func lowerHex(value string, length int) bool {
	if len(value) != length {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && hex.EncodeToString(decoded) == value
}

func canonicalUUID(value string) bool {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return false
	}
	return lowerHex(strings.ReplaceAll(value, "-", ""), 32)
}

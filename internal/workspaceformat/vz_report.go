package workspaceformat

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// parseVZRunEvidence admits only the exact stopped, isolated host observation
// paired with the guest's transaction-bound ext4 result. The child process
// must have exited and been reaped before its caller invokes this parser.
func parseVZRunEvidence(raw []byte, transaction, filesystemUUID string) (FormatEvidence, error) {
	if len(raw) == 0 || len(raw) > 4096 || raw[len(raw)-1] != '\n' || bytes.Count(raw, []byte{'\n'}) != 1 {
		return FormatEvidence{}, fmt.Errorf("formatter host result is not one bounded line")
	}
	if err := rejectDuplicateJSON(raw); err != nil {
		return FormatEvidence{}, fmt.Errorf("ambiguous formatter host result: %w", err)
	}
	var result struct {
		Version                *int    `json:"version"`
		Transaction            *string `json:"transaction"`
		VMStopped              *bool   `json:"vm_stopped"`
		RuntimeNetworkDevices  *int    `json:"runtime_network_devices"`
		StorageDevices         *int    `json:"storage_devices"`
		StorageReadOnly        *bool   `json:"storage_read_only"`
		SerialPorts            *int    `json:"serial_ports"`
		SocketDevices          *int    `json:"socket_devices"`
		SharedDirectoryDevices *int    `json:"shared_directory_devices"`
		ObservedUUID           *string `json:"observed_uuid"`
		WholeDevice            *bool   `json:"whole_device"`
		FilesystemClean        *bool   `json:"filesystem_clean"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return FormatEvidence{}, fmt.Errorf("decode formatter host result: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return FormatEvidence{}, fmt.Errorf("formatter host result has trailing content")
	}
	if result.Version == nil || *result.Version != 1 ||
		result.Transaction == nil || *result.Transaction != transaction ||
		result.VMStopped == nil || !*result.VMStopped ||
		result.RuntimeNetworkDevices == nil || *result.RuntimeNetworkDevices != 0 ||
		result.StorageDevices == nil || *result.StorageDevices != 1 ||
		result.StorageReadOnly == nil || *result.StorageReadOnly ||
		result.SerialPorts == nil || *result.SerialPorts != 2 ||
		result.SocketDevices == nil || *result.SocketDevices != 0 ||
		result.SharedDirectoryDevices == nil || *result.SharedDirectoryDevices != 0 ||
		result.ObservedUUID == nil || *result.ObservedUUID != filesystemUUID ||
		result.WholeDevice == nil || !*result.WholeDevice ||
		result.FilesystemClean == nil || !*result.FilesystemClean {
		return FormatEvidence{}, fmt.Errorf("formatter host did not verify exact stopped isolated ext4 result")
	}
	return FormatEvidence{ObservedUUID: *result.ObservedUUID, WholeDevice: true, FilesystemClean: true}, nil
}

package config

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/weshofmann/boxwarden/internal/hostidentity"
)

// NewEnrolledAlphaConfig renders a new declaration, without reading or adopting
// another configuration. Filesystem admission remains the publisher's job.
func NewEnrolledAlphaConfig(host Host, root string, storage WorkspaceStorage) ([]byte, error) {
	if err := (hostidentity.StorageExpectation{ConfigPath: "/config", StateRoot: root, MountPoint: storage.MountPoint, VolumeUUID: storage.VolumeUUID}).Validate(); err != nil {
		return nil, err
	}
	for _, path := range []string{host.TartExecutable, host.TartHome, host.SoftnetSource} {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path {
			return nil, fmt.Errorf("host path must be clean and absolute")
		}
	}
	paths := []string{root, host.TartExecutable, host.TartHome, host.SoftnetSource}
	for i, parent := range paths {
		for j, child := range paths {
			if i == j {
				continue
			}
			relative, _ := filepath.Rel(parent, child)
			if relative == "." || (relative != ".." && !strings.HasPrefix(relative, "../")) {
				return nil, fmt.Errorf("first-run host and domain paths overlap")
			}
		}
	}
	type savedStorage struct {
		MountPoint string `json:"mount_point"`
		VolumeUUID string `json:"apfs_volume_uuid"`
	}
	type savedDomain struct {
		StateRoot string       `json:"state_root"`
		Storage   savedStorage `json:"workspace_storage"`
	}
	type savedHost struct {
		TartExecutable string `json:"tart_executable"`
		TartHome       string `json:"tart_home"`
		SoftnetSource  string `json:"softnet_source"`
	}
	value := struct {
		Version int                    `json:"version"`
		Host    savedHost              `json:"host"`
		Domains map[string]savedDomain `json:"domains"`
	}{2, savedHost{host.TartExecutable, host.TartHome, host.SoftnetSource}, map[string]savedDomain{"alpha": {root, savedStorage{storage.MountPoint, storage.VolumeUUID}}}}
	raw, err := json.MarshalIndent(value, "", "  ")
	return append(raw, '\n'), err
}

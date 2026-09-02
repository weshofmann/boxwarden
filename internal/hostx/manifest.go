package hostx

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"
	"unicode"
)

const maxManifestBytes = 64 << 10

var ErrUnsupportedManifestVersion = errors.New("unsupported manifest version")

type Group struct {
	ID      int    `json:"id"`
	Name    string `json:"name"`
	Members []int  `json:"members"`
}

type Operator struct {
	UID  int    `json:"uid"`
	Name string `json:"name"`
	Home string `json:"home"`
}

// Manifest is the root-published, versioned authorization record for the
// digest tree. It deliberately has no mutable "current" pointer.
type Manifest struct {
	Version     int          `json:"version"`
	Platform    string       `json:"platform"`
	MacOS       string       `json:"macos"`
	MacOSBuild  string       `json:"macos_build"`
	Tart        ToolIdentity `json:"tart"`
	Softnet     ToolIdentity `json:"softnet"`
	RootUID     int          `json:"root_uid"`
	Group       Group        `json:"group"`
	Operator    Operator     `json:"operator"`
	TartHome    string       `json:"tart_home"`
	SoftnetMode uint32       `json:"softnet_mode"`
	InstalledAt time.Time    `json:"installed_at"`
}

func ParseManifest(data []byte) (Manifest, error) {
	if len(data) == 0 || len(data) > maxManifestBytes {
		return Manifest{}, fmt.Errorf("manifest must be between 1 and %d bytes", maxManifestBytes)
	}
	if err := rejectDuplicateFields(data); err != nil {
		return Manifest{}, fmt.Errorf("decode manifest: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var manifest Manifest
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode manifest: %w", err)
	}
	if decoder.More() {
		return Manifest{}, fmt.Errorf("manifest has trailing JSON")
	}
	var extra any
	if err := decoder.Decode(&extra); err == nil {
		return Manifest{}, fmt.Errorf("manifest has trailing content")
	}
	if err := manifest.Validate(); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func rejectDuplicateFields(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := consumeJSONValue(decoder); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		if err == nil {
			return fmt.Errorf("trailing JSON value")
		}
		return err
	}
	return nil
}

func consumeJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, isDelimiter := token.(json.Delim)
	if !isDelimiter {
		return nil
	}
	switch delimiter {
	case '{':
		seen := map[string]bool{}
		for decoder.More() {
			nameToken, err := decoder.Token()
			if err != nil {
				return err
			}
			name, ok := nameToken.(string)
			if !ok {
				return fmt.Errorf("object field is not a string")
			}
			if seen[name] {
				return fmt.Errorf("duplicate field %q", name)
			}
			seen[name] = true
			if err := consumeJSONValue(decoder); err != nil {
				return err
			}
		}
		_, err := decoder.Token()
		return err
	case '[':
		for decoder.More() {
			if err := consumeJSONValue(decoder); err != nil {
				return err
			}
		}
		_, err := decoder.Token()
		return err
	default:
		return fmt.Errorf("unexpected JSON delimiter %q", delimiter)
	}
}

func (m Manifest) Validate() error {
	if m.Version != ManifestVersion {
		return fmt.Errorf("%w %d", ErrUnsupportedManifestVersion, m.Version)
	}
	if m.Platform != QualifiedPlatform || m.MacOS != QualifiedMacOS || m.MacOSBuild != QualifiedMacOSBuild {
		return fmt.Errorf("manifest has unqualified platform %q macOS %q build %q", m.Platform, m.MacOS, m.MacOSBuild)
	}
	if !qualifiedTart(m.Tart) || !canonicalAbsolute(m.Tart.Path) {
		return fmt.Errorf("manifest has unqualified Tart identity")
	}
	if !qualifiedSoftnet(m.Softnet) {
		return fmt.Errorf("manifest has unqualified Softnet identity")
	}
	if m.RootUID != 0 || m.SoftnetMode != SoftnetMode {
		return fmt.Errorf("manifest has invalid root ownership or Softnet mode")
	}
	if m.Group.ID < 0 || m.Group.Name != OperatorGroupName || len(m.Group.Members) != 1 || m.Group.Members[0] < 0 {
		return fmt.Errorf("manifest has invalid operator group")
	}
	if m.Operator.UID != m.Group.Members[0] || strings.TrimSpace(m.Operator.Name) == "" || !canonicalAbsolute(m.Operator.Home) {
		return fmt.Errorf("manifest has invalid trusted operator")
	}
	if !canonicalAbsolute(m.TartHome) || m.InstalledAt.IsZero() {
		return fmt.Errorf("manifest has invalid trusted paths or timestamp")
	}
	return nil
}

func canonicalAbsolute(path string) bool {
	return filepath.IsAbs(path) && filepath.Clean(path) == path && path != "/" && strings.IndexFunc(path, unicode.IsControl) < 0
}

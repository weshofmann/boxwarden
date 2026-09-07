package timezonex

import (
	"io/fs"
	"testing"
	"time"
)

// Production break: accepting a host localtime file outside the trusted
// zoneinfo tree would permit an unvalidated or ambient zone source.
func TestHostDetectorResolvesOnlyTrustedIANAZoneinfo(t *testing.T) {
	resolver := &filesystemFake{resolved: map[string]string{
		"/var/db/timezone/zoneinfo": "/var/db/timezone/zoneinfo",
		"/etc/localtime":            "/var/db/timezone/zoneinfo/America/Denver",
	}, info: map[string]fs.FileInfo{
		"/var/db/timezone/zoneinfo":                fileInfo{directory: true},
		"/var/db/timezone/zoneinfo/America/Denver": fileInfo{},
	}}
	zone, err := (HostDetector{Resolver: resolver}).Detect()
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if zone != "America/Denver" {
		t.Fatalf("Detect() = %q, want America/Denver", zone)
	}
}

type filesystemFake struct {
	resolved map[string]string
	info     map[string]fs.FileInfo
}

func (f *filesystemFake) EvalSymlinks(path string) (string, error) { return f.resolved[path], nil }
func (f *filesystemFake) Stat(path string) (fs.FileInfo, error)    { return f.info[path], nil }

type fileInfo struct{ directory bool }

func (f fileInfo) Name() string { return "fixture" }
func (f fileInfo) Size() int64  { return 0 }
func (f fileInfo) Mode() fs.FileMode {
	if f.directory {
		return fs.ModeDir | 0o755
	}
	return 0o644
}
func (f fileInfo) ModTime() time.Time { return time.Time{} }
func (f fileInfo) IsDir() bool        { return f.directory }
func (f fileInfo) Sys() any           { return nil }

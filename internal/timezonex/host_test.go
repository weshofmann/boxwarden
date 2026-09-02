package timezonex

import (
	"io/fs"
	"testing"
	"time"
)

func TestHostDetectorResolvesLocaltimeInsideTrustedZoneinfo(t *testing.T) {
	resolver := &filesystemFake{resolved: map[string]string{
		"/var/db/timezone/zoneinfo": "/var/db/timezone/zoneinfo",
		"/etc/localtime":            "/var/db/timezone/zoneinfo/America/Chihuahua",
	}, info: map[string]fs.FileInfo{
		"/var/db/timezone/zoneinfo":                   fileInfo{directory: true},
		"/var/db/timezone/zoneinfo/America/Chihuahua": fileInfo{},
	}}
	detector := HostDetector{Resolver: resolver}
	zone, err := detector.Detect()
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if zone != "America/Chihuahua" {
		t.Fatalf("Detect() = %q, want America/Chihuahua", zone)
	}
}

func TestHostDetectorRejectsUntrustedOrMalformedLocaltime(t *testing.T) {
	for name, target := range map[string]string{
		"outside root":      "/private/untrusted/UTC",
		"traversal name":    "/var/db/timezone/zoneinfo/America/../UTC",
		"invalid character": "/var/db/timezone/zoneinfo/America/Chihuahua space",
	} {
		t.Run(name, func(t *testing.T) {
			resolver := &filesystemFake{resolved: map[string]string{
				"/var/db/timezone/zoneinfo": "/var/db/timezone/zoneinfo",
				"/etc/localtime":            target,
			}, info: map[string]fs.FileInfo{
				"/var/db/timezone/zoneinfo": fileInfo{directory: true}, target: fileInfo{},
			}}
			if _, err := (HostDetector{Resolver: resolver}).Detect(); err == nil {
				t.Fatal("Detect() error = nil, want no-fallback rejection")
			}
		})
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

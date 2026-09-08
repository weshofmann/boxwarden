// Package timezonex validates and converges the trusted host's IANA time zone.
package timezonex

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const (
	defaultLocaltimePath = "/etc/localtime"
	defaultZoneinfoRoot  = "/var/db/timezone/zoneinfo"
)

type Resolver interface {
	EvalSymlinks(string) (string, error)
	Stat(string) (fs.FileInfo, error)
}

type osResolver struct{}

func (osResolver) EvalSymlinks(path string) (string, error) { return filepath.EvalSymlinks(path) }
func (osResolver) Stat(path string) (fs.FileInfo, error)    { return os.Stat(path) }

type HostDetector struct {
	Resolver      Resolver
	LocaltimePath string
	ZoneinfoRoot  string
}

func (d HostDetector) Detect() (string, error) {
	resolver := d.Resolver
	if resolver == nil {
		resolver = osResolver{}
	}
	localtime, zoneinfoRoot := d.LocaltimePath, d.ZoneinfoRoot
	if localtime == "" {
		localtime = defaultLocaltimePath
	}
	if zoneinfoRoot == "" {
		zoneinfoRoot = defaultZoneinfoRoot
	}
	if !canonicalAbsolutePath(localtime) || !canonicalAbsolutePath(zoneinfoRoot) {
		return "", fmt.Errorf("time-zone paths must be canonical and absolute")
	}
	root, err := resolver.EvalSymlinks(zoneinfoRoot)
	if err != nil || !canonicalAbsolutePath(root) {
		return "", fmt.Errorf("resolve trusted zoneinfo root")
	}
	rootInfo, err := resolver.Stat(root)
	if err != nil || !rootInfo.IsDir() {
		return "", fmt.Errorf("trusted zoneinfo root is unavailable")
	}
	localtimeTarget, err := resolver.EvalSymlinks(localtime)
	if err != nil || !canonicalAbsolutePath(localtimeTarget) {
		return "", fmt.Errorf("resolve host localtime")
	}
	zoneInfo, err := resolver.Stat(localtimeTarget)
	if err != nil || !zoneInfo.Mode().IsRegular() {
		return "", fmt.Errorf("host localtime is not a zoneinfo file")
	}
	zone, err := filepath.Rel(root, localtimeTarget)
	if err != nil || zone == "." || zone == ".." || strings.HasPrefix(zone, ".."+string(filepath.Separator)) || !Valid(zone) {
		return "", fmt.Errorf("host localtime is outside trusted zoneinfo")
	}
	return zone, nil
}

func DetectHost() (string, error) { return HostDetector{}.Detect() }

func Valid(zone string) bool {
	if len(zone) == 0 || len(zone) > 127 || filepath.IsAbs(zone) || filepath.Clean(zone) != zone || strings.Contains(zone, "..") {
		return false
	}
	for _, segment := range strings.Split(zone, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
		for _, value := range segment {
			if !((value >= 'a' && value <= 'z') || (value >= 'A' && value <= 'Z') || (value >= '0' && value <= '9') || value == '.' || value == '_' || value == '-' || value == '+') {
				return false
			}
		}
	}
	return true
}

func canonicalAbsolutePath(path string) bool {
	return path != "/" && filepath.IsAbs(path) && filepath.Clean(path) == path && !strings.ContainsAny(path, "\x00\r\n")
}

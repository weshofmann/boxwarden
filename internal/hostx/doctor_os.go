//go:build darwin || linux

package hostx

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"os/user"
	pathpkg "path"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/weshofmann/boxwarden/internal/execx"
)

type osDoctorInspector struct {
	acl     ACLInspector
	runner  execx.Runner
	timeout time.Duration
}

func NewOSDoctorInspector() DoctorInspector {
	return &osDoctorInspector{acl: OSACLInspector{}, runner: execx.OSRunner{MaxOutputBytes: 16 << 10}, timeout: 5 * time.Second}
}

func (i *osDoctorInspector) commandContext() (context.Context, context.CancelFunc) {
	timeout := i.timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return context.WithTimeout(context.Background(), timeout)
}

func (i *osDoctorInspector) Platform() PlatformFact {
	fact := PlatformFact{OS: runtime.GOOS, Arch: runtime.GOARCH}
	if runtime.GOOS != "darwin" {
		return fact
	}
	ctx, cancel := i.commandContext()
	defer cancel()
	result, err := i.runner.Run(ctx, execx.Command{Path: "/usr/bin/sw_vers", Args: []string{"-productVersion"}, Env: []string{"LC_ALL=C", "LANG=C"}})
	if err == nil && !result.Truncated {
		fact.Release = strings.TrimSpace(result.Stdout)
	}
	return fact
}

func (i *osDoctorInspector) InspectPath(path string) (PathFact, error) {
	if !canonicalAbsolute(path) {
		return PathFact{}, fmt.Errorf("path is not canonical and absolute")
	}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return PathFact{}, nil
	}
	if err != nil {
		return PathFact{}, err
	}
	if _, err := snapshotPath(path); err != nil {
		return PathFact{}, err
	}
	uid, gid, ok := ownership(info)
	if !ok {
		return PathFact{}, fmt.Errorf("unsupported filesystem metadata")
	}
	fact := PathFact{Exists: true, Regular: info.Mode().IsRegular(), Directory: info.IsDir(), Mode: unixMode(info), UID: uid, GID: gid, Links: links(info)}
	acl, err := i.acl.HasExtendedACL(path)
	if err != nil {
		return PathFact{}, err
	}
	fact.ExtendedACL = acl
	if !fact.Regular {
		return fact, nil
	}
	file, err := openVerifiedRegular(path, "", false)
	if err != nil {
		return PathFact{}, err
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		file.Close()
		return PathFact{}, err
	}
	fact.SHA256 = fmt.Sprintf("%x", hash.Sum(nil))
	if filepath.Base(path) == "manifest.json" {
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			file.Close()
			return PathFact{}, err
		}
		fact.Data, err = io.ReadAll(io.LimitReader(file, maxManifestBytes+1))
		if err != nil || len(fact.Data) > maxManifestBytes {
			file.Close()
			return PathFact{}, fmt.Errorf("manifest exceeds bounded input")
		}
	}
	if err := file.Close(); err != nil {
		return PathFact{}, err
	}
	return fact, nil
}

func (*osDoctorInspector) LookupOperator(uid int) (Operator, error) {
	entry, err := user.LookupId(strconv.Itoa(uid))
	if err != nil {
		return Operator{}, err
	}
	resolvedUID, err := strconv.Atoi(entry.Uid)
	if err != nil || resolvedUID != uid || entry.Username == "" || !canonicalAbsolute(entry.HomeDir) {
		return Operator{}, fmt.Errorf("invalid operator directory record")
	}
	return Operator{UID: resolvedUID, Name: entry.Username, Home: entry.HomeDir}, nil
}

func (i *osDoctorInspector) LookupGroup(name string) (Group, error) {
	if name != OperatorGroupName {
		return Group{}, fmt.Errorf("unexpected group name")
	}
	entry, err := user.LookupGroup(name)
	if err != nil {
		return Group{}, err
	}
	gid, err := strconv.Atoi(entry.Gid)
	if err != nil || gid < 0 {
		return Group{}, fmt.Errorf("invalid group id")
	}
	members, err := lookupDirectGroupMembers(i.runner, name)
	if err != nil {
		return Group{}, err
	}
	return Group{ID: gid, Name: name, Members: members}, nil
}

func (*osDoctorInspector) IsMember(operator Operator, group Group) (bool, error) {
	entry, err := user.LookupId(strconv.Itoa(operator.UID))
	if err != nil {
		return false, err
	}
	groups, err := entry.GroupIds()
	if err != nil {
		return false, err
	}
	for _, raw := range groups {
		gid, err := strconv.Atoi(raw)
		if err == nil && gid == group.ID {
			return containsInt(group.Members, operator.UID), nil
		}
	}
	return false, nil
}

func (*osDoctorInspector) EffectiveGroups() ([]int, error) { return os.Getgroups() }

func (i *osDoctorInspector) CommandOutput(path string, args ...string) (string, error) {
	if !canonicalAbsolute(path) {
		return "", fmt.Errorf("command path is not canonical and absolute")
	}
	ctx, cancel := i.commandContext()
	defer cancel()
	result, err := i.runner.Run(ctx, execx.Command{Path: path, Args: append([]string(nil), args...), Env: []string{"LC_ALL=C", "LANG=C"}})
	if err != nil || result.Truncated {
		return "", fmt.Errorf("bounded command inspection failed")
	}
	output := result.Stdout
	if strings.TrimSpace(output) == "" {
		output = result.Stderr
	}
	return strings.TrimSpace(output), nil
}

func (i *osDoctorInspector) HomebrewSoftnet() ([]HomebrewSoftnet, error) {
	patterns := []string{
		"/opt/homebrew/bin/softnet", "/usr/local/bin/softnet",
		"/opt/homebrew/Cellar/softnet/*/bin/softnet", "/usr/local/Cellar/softnet/*/bin/softnet",
	}
	seen := map[string]bool{}
	policyTargets := map[string]bool{"/opt/homebrew/bin/softnet": true, "/usr/local/bin/softnet": true}
	var findings []HomebrewSoftnet
	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return nil, err
		}
		for _, path := range matches {
			resolved, err := filepath.EvalSymlinks(path)
			if err != nil {
				return nil, err
			}
			resolved = filepath.Clean(resolved)
			if seen[resolved] {
				continue
			}
			seen[resolved] = true
			policyTargets[resolved] = true
			info, err := os.Lstat(resolved)
			if err != nil {
				return nil, err
			}
			privilege := ""
			if info.Mode()&os.ModeSetuid != 0 {
				privilege = "setuid"
			}
			if info.Mode()&os.ModeSetgid != 0 {
				if privilege != "" {
					privilege += "+setgid"
				} else {
					privilege = "setgid"
				}
			}
			findings = append(findings, HomebrewSoftnet{Path: resolved, Privilege: privilege})
		}
	}
	for target := range policyTargets {
		passwordless, err := i.passwordlessRoot(target)
		if err != nil {
			return nil, err
		}
		if passwordless {
			matched := false
			for index := range findings {
				if findings[index].Path == target {
					if findings[index].Privilege != "" {
						findings[index].Privilege += "+passwordless-root"
					} else {
						findings[index].Privilege = "passwordless-root"
					}
					matched = true
				}
			}
			if !matched {
				findings = append(findings, HomebrewSoftnet{Path: target, Privilege: "passwordless-root"})
			}
		}
	}
	sort.Slice(findings, func(a, b int) bool { return findings[a].Path < findings[b].Path })
	return findings, nil
}

func (i *osDoctorInspector) passwordlessRoot(target string) (bool, error) {
	ctx, cancel := i.commandContext()
	defer cancel()
	result, err := i.runner.Run(ctx, execx.Command{
		Path: "/usr/bin/sudo", Args: []string{"-n", "-l", "--", target},
		Env: []string{"LC_ALL=C", "LANG=C", "PATH=/usr/bin:/bin:/usr/sbin:/sbin"},
	})
	combined := result.Stdout + result.Stderr
	if result.Truncated {
		return false, fmt.Errorf("bounded sudo policy inspection was truncated")
	}
	if err != nil {
		lower := strings.ToLower(combined)
		if strings.Contains(lower, "not allowed to run sudo") || strings.Contains(lower, "not in the sudoers file") || strings.Contains(lower, "may not run sudo") {
			return false, nil
		}
		// A password-required response does not prove that some argument-specific
		// NOPASSWD rule for the same mutable executable is absent. Doctor and init
		// therefore fail closed rather than depending on sudo timestamp state.
		return false, fmt.Errorf("sudo policy inspection failed")
	}
	return detectPasswordlessRule(combined, target), nil
}

func detectPasswordlessRule(output, target string) bool {
	for _, line := range strings.Split(output, "\n") {
		if !strings.Contains(line, "NOPASSWD:") {
			continue
		}
		fields := strings.Fields(strings.TrimSpace(strings.SplitN(line, "NOPASSWD:", 2)[1]))
		if len(fields) == 0 {
			continue
		}
		command := strings.TrimSuffix(fields[0], ",")
		matched, _ := pathpkg.Match(command, target)
		if command == "ALL" || command == target || (strings.HasPrefix(command, "/opt/homebrew/") || strings.HasPrefix(command, "/usr/local/")) && matched {
			return true
		}
	}
	return false
}

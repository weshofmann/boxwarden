//go:build darwin || linux

package hostx

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
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
	result, err = i.runner.Run(ctx, execx.Command{Path: "/usr/bin/sw_vers", Args: []string{"-buildVersion"}, Env: []string{"LC_ALL=C", "LANG=C"}})
	if err == nil && !result.Truncated {
		fact.Build = strings.TrimSpace(result.Stdout)
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
	uid, gid, ok := ownership(info)
	if !ok {
		return PathFact{Exists: true}, fmt.Errorf("unsupported filesystem metadata")
	}
	fact := PathFact{Exists: true, Regular: info.Mode().IsRegular(), Directory: info.IsDir(), Mode: unixMode(info), UID: uid, GID: gid, Links: links(info)}
	if _, err := snapshotPath(path); err != nil {
		return fact, err
	}
	acl, err := i.acl.HasExtendedACL(path)
	if err != nil {
		return fact, err
	}
	fact.ExtendedACL = acl
	if !fact.Regular {
		return fact, nil
	}
	file, err := openVerifiedRegular(path, "", false)
	if err != nil {
		return fact, err
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		file.Close()
		return fact, err
	}
	fact.SHA256 = fmt.Sprintf("%x", hash.Sum(nil))
	if filepath.Base(path) == "manifest.json" {
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			file.Close()
			return fact, err
		}
		fact.Data, err = io.ReadAll(io.LimitReader(file, maxManifestBytes+1))
		if err != nil || len(fact.Data) > maxManifestBytes {
			file.Close()
			return fact, fmt.Errorf("manifest exceeds bounded input")
		}
	}
	if err := file.Close(); err != nil {
		return fact, err
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

func (i *osDoctorInspector) ExactOperatorGroup(operator Operator, name string) (Group, error) {
	return inspectExactLocalOperatorGroup(i.runner, operator, name, false)
}

func (*osDoctorInspector) EffectiveGroups() ([]int, error) { return os.Getgroups() }

func (i *osDoctorInspector) CommandOutput(path string, args ...string) (string, error) {
	if !canonicalAbsolute(path) {
		return "", fmt.Errorf("command path is not canonical and absolute")
	}
	ctx, cancel := i.commandContext()
	defer cancel()
	result, err := i.runner.Run(ctx, execx.Command{Path: path, Args: append([]string(nil), args...), Env: []string{"LC_ALL=C", "LANG=C"}})
	if result.Truncated {
		return "", fmt.Errorf("bounded command inspection failed")
	}
	if err != nil && !knownScreenVersionStatusOne(path, args, result, err) {
		return "", fmt.Errorf("bounded command inspection failed")
	}
	output := result.Stdout
	if strings.TrimSpace(output) == "" {
		output = result.Stderr
	}
	output = strings.TrimSpace(output)
	return output, nil
}

// macOS's system Screen prints its stable version string but exits 1 for this
// exact probe. This exception is intentionally limited to that qualified
// binary, argument vector, output, and status; other failed probes stay errors.
func knownScreenVersionStatusOne(path string, args []string, result execx.Result, err error) bool {
	if path != ScreenPath || len(args) != 1 || args[0] != "--version" || strings.TrimSpace(result.Stdout) != ScreenVersionOutput || strings.TrimSpace(result.Stderr) != "" {
		return false
	}
	var exitError *exec.ExitError
	return errors.As(err, &exitError) && exitError.ExitCode() == 1
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
		Path: "/usr/bin/sudo", Args: []string{"-n", "-ll"},
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
	passwordless, parseErr := parseVerboseSudoPasswordlessRule(combined, target)
	if parseErr != nil {
		return false, fmt.Errorf("sudo policy inspection could not be parsed")
	}
	return passwordless, nil
}

func detectPasswordlessRule(output, target string) bool {
	matched, err := parseVerboseSudoPasswordlessRule(output, target)
	return err == nil && matched
}

type sudoVerboseStanza struct {
	noAuthenticate bool
	commands       []string
}

// parseVerboseSudoPasswordlessRule accepts only the stanza structure emitted by
// `sudo -ll`. A target-filtered `sudo -l` omits the authentication option, so
// it cannot prove whether a matching mutable Homebrew command is passwordless.
func parseVerboseSudoPasswordlessRule(output, target string) (bool, error) {
	inheritedNoAuthenticate, err := validateVerboseSudoDefaults(output)
	if err != nil {
		return false, err
	}
	var stanzas []sudoVerboseStanza
	var current *sudoVerboseStanza
	commandsStarted := false

	finish := func() error {
		if current == nil {
			return nil
		}
		if !commandsStarted || len(current.commands) == 0 {
			return fmt.Errorf("incomplete sudoers entry")
		}
		stanzas = append(stanzas, *current)
		return nil
	}

	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" {
			continue
		}
		if verboseSudoersEntryHeader(line) {
			if err := finish(); err != nil {
				return false, err
			}
			current = &sudoVerboseStanza{noAuthenticate: inheritedNoAuthenticate}
			commandsStarted = false
			continue
		}
		if current == nil {
			continue
		}

		switch {
		case strings.HasPrefix(line, "Options:"):
			if commandsStarted {
				return false, fmt.Errorf("sudoers options after commands")
			}
			noAuthenticate, authenticationSet, optionErr := sudoOptionsAuthentication(strings.TrimSpace(strings.TrimPrefix(line, "Options:")))
			if optionErr != nil {
				return false, optionErr
			}
			if authenticationSet {
				current.noAuthenticate = noAuthenticate
			}
		case line == "Commands:":
			if commandsStarted {
				return false, fmt.Errorf("multiple sudoers command blocks")
			}
			commandsStarted = true
		case commandsStarted:
			if strings.HasSuffix(line, ",") || strings.Contains(line, ",") {
				return false, fmt.Errorf("ambiguous sudoers command list")
			}
			current.commands = append(current.commands, line)
		case strings.Contains(line, ":"):
			// Other verbose stanza attributes (for example RunAsUsers) do not
			// change command authentication and are deliberately ignored.
		default:
			return false, fmt.Errorf("unrecognized sudoers entry content")
		}
	}
	if err := finish(); err != nil {
		return false, err
	}
	if len(stanzas) == 0 {
		return false, fmt.Errorf("verbose sudoers entries absent")
	}
	for _, stanza := range stanzas {
		if !stanza.noAuthenticate {
			continue
		}
		for _, command := range stanza.commands {
			matched, err := sudoCommandMatchesTarget(command, target)
			if err != nil {
				return false, err
			}
			if matched {
				return true, nil
			}
		}
	}
	return false, nil
}

// validateVerboseSudoDefaults intentionally recognizes only the two Defaults
// settings that make a noninteractive list unable to establish authentication
// for this operator. It does not attempt to interpret the sudoers language.
func validateVerboseSudoDefaults(output string) (bool, error) {
	inDefaults := false
	commandSpecificDefaults := false
	inheritedNoAuthenticate := false
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if verboseSudoersEntryHeader(line) {
			break
		}
		if strings.HasPrefix(line, "Matching Defaults entries for ") {
			inDefaults = true
			commandSpecificDefaults = false
			continue
		}
		if strings.HasPrefix(line, "Runas and Command-specific defaults for ") {
			inDefaults = true
			commandSpecificDefaults = true
			continue
		}
		if !inDefaults || line == "" {
			continue
		}
		if strings.HasPrefix(line, "User ") {
			inDefaults = false
			continue
		}
		noAuthenticate, authenticationSet, optionErr := sudoOptionsAuthentication(line)
		if optionErr != nil {
			return false, optionErr
		}
		if authenticationSet {
			if commandSpecificDefaults {
				return false, fmt.Errorf("sudo command-specific Defaults affect authentication")
			}
			inheritedNoAuthenticate = noAuthenticate
		}
		for _, option := range strings.Split(line, ",") {
			option = strings.TrimSpace(option)
			if strings.Contains(option, "exempt_group") && option != "!exempt_group" {
				return false, fmt.Errorf("sudo Defaults exempt group is enabled or ambiguous")
			}
		}
	}
	return inheritedNoAuthenticate, nil
}

func verboseSudoersEntryHeader(line string) bool {
	const prefix = "Sudoers entry:"
	if line == prefix {
		return true
	}
	suffix, found := strings.CutPrefix(line, prefix)
	return found && len(suffix) > 0 && (suffix[0] == ' ' || suffix[0] == '\t') && strings.TrimSpace(suffix) != ""
}

func sudoOptionsAuthentication(options string) (bool, bool, error) {
	set := false
	noAuthenticate := false
	tokens, err := sudoOptionTokens(options)
	if err != nil {
		return false, false, err
	}
	for _, option := range tokens {
		if option == "!authenticate" {
			set, noAuthenticate = true, true
		}
		if option == "authenticate" {
			set, noAuthenticate = true, false
		}
	}
	return noAuthenticate, set, nil
}

func sudoOptionTokens(options string) ([]string, error) {
	var tokens []string
	var token strings.Builder
	var quote rune
	escaped := false
	for _, r := range options {
		if escaped {
			token.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
			} else {
				token.WriteRune(r)
			}
			continue
		}
		if r == '\'' || r == '"' {
			quote = r
			continue
		}
		if r == ',' || r == ' ' || r == '\t' {
			if token.Len() > 0 {
				tokens = append(tokens, token.String())
				token.Reset()
			}
			continue
		}
		token.WriteRune(r)
	}
	if escaped || quote != 0 {
		return nil, fmt.Errorf("malformed sudoers options")
	}
	if token.Len() > 0 {
		tokens = append(tokens, token.String())
	}
	return tokens, nil
}

func sudoCommandMatchesTarget(specification, target string) (bool, error) {
	fields := strings.Fields(specification)
	if len(fields) == 0 {
		return false, fmt.Errorf("empty sudoers command")
	}
	command := fields[0]
	for _, argument := range fields[1:] {
		if strings.ContainsAny(argument, "'\\\"") {
			return false, fmt.Errorf("unsupported sudoers command arguments")
		}
	}
	if command == "ALL" {
		if len(fields) != 1 {
			return false, fmt.Errorf("ambiguous ALL sudoers command")
		}
		return true, nil
	}
	if !strings.HasPrefix(command, "/") {
		return false, fmt.Errorf("unrecognized sudoers command")
	}
	if strings.ContainsAny(command, "*?[]!^$\\") {
		return false, fmt.Errorf("unsupported sudoers command pattern")
	}
	if command == target {
		return true, nil
	}
	if strings.HasSuffix(command, "/") {
		if len(fields) != 1 {
			return false, fmt.Errorf("sudoers command directory has arguments")
		}
		return pathpkg.Dir(target) == strings.TrimSuffix(command, "/"), nil
	}
	return false, nil
}

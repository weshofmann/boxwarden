package architecture

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

var (
	libprocSourcePattern = regexp.MustCompile(`(?i)(\blibproc\b|<libproc\.h>|-lproc\b)`)
	ofdSourcePattern     = regexp.MustCompile(`(?i)(\bofd\b|f_ofd_[a-z0-9_]*|\bofd_[a-z0-9_]*)`)
	hmacSourcePattern    = regexp.MustCompile(`(?i)\bhmac\b`)
)

func TestSliceBPolicyRejectsDiscardedAndDeferredMechanisms(t *testing.T) {
	tests := []struct {
		name, path, source, want string
	}{
		{"moved deferred import", "internal/lifecycle/start.go", `package lifecycle; import _ "github.com/weshofmann/boxwarden/internal/timezonex"`, "deferred Slice C/D import"},
		{"moved deferred call", "internal/backend/start.go", `package backend; func f(r interface{ Bootstrap() }) { r.Bootstrap() }`, "deferred Slice C/D call"},
		{"composite readiness publication", "internal/app/start.go", `package app; type Snapshot struct{ PinPresent bool }; var _ = Snapshot{PinPresent: true}`, "deferred readiness publication"},
		{"assigned readiness publication", "internal/lifecycle/start.go", `package lifecycle; type Snapshot struct{ ZoneMatches bool }; func f(s *Snapshot) { s.ZoneMatches = true }`, "deferred readiness publication"},
		{"cgo libproc header", "internal/backend/proc.go", "package backend\n/* #include <libproc.h> */\nimport \"C\"", "discarded libproc"},
		{"cgo libproc linker", "internal/backend/proc.go", "package backend\n/* #cgo LDFLAGS: -lproc */\nimport \"C\"", "discarded libproc"},
		{"cgo OFD token", "internal/lifecycle/lock.go", "package lifecycle\n// F_OFD_SETLK would reconstruct authority.\n", "discarded OFD"},
		{"direct second PTY", "internal/serialx/runtime.go", `package serialx; func second() { allocatePTY() }`, "direct PTY allocation"},
		{"alternate process field", "internal/backend/state.go", `package backend; type State struct { ProcessID int }`, "persisted process authority"},
		{"alternate process json", "internal/backend/state.go", "package backend; type State struct { Value int `json:\"process_id\"` }", "persisted process authority"},
		{"pid map key", "internal/lifecycle/state.go", `package lifecycle; var state = map[string]any{"pid": 42}`, "persisted process authority"},
		{"process reconstruction", "internal/backend/start.go", `package backend; import "os"; func f() { os.FindProcess(42) }`, "process reconstruction"},
		{"ownership record", "internal/lifecycle/state.go", `package lifecycle; type OwnershipRecord struct{ Token string }`, "ownership record"},
		{"ownership file token", "internal/supervisor/state.go", `package supervisor; const ownershipFile = "runtime-owner.json"`, "ownership file"},
		{"screen command", "cmd/boxwarden/main.go", `package main; const command = "/usr/bin/screen"`, "discarded Screen/socat"},
		{"HMAC import", "internal/backend/start.go", `package backend; import _ "crypto/hmac"`, "discarded HMAC"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			issues := strings.Join(inspectSliceBSource(test.path, []byte(test.source)), "\n")
			if !strings.Contains(issues, test.want) {
				t.Fatalf("issues = %q, want category %q", issues, test.want)
			}
		})
	}
}

func TestSliceBPolicyAllowsCurrentAdmissionAndUncomposedFoundations(t *testing.T) {
	tests := []struct {
		name, path, source string
	}{
		{
			"request binding and CA admission",
			"internal/sessionruntime/owner.go",
			`package sessionruntime
import (
  "context"
  "github.com/weshofmann/boxwarden/internal/sshx"
  "github.com/weshofmann/boxwarden/internal/supervisor"
)
func f(ctx context.Context, ca *sshx.CAStore, expectation struct{ Manifest struct{ Operator string } }) {
  _, _ = ca.Check(ctx, sshx.Domain{}, []sshx.Domain{{}})
  _ = supervisor.LaunchRequest{Binding: supervisor.Binding{}}
  _ = supervisor.Snapshot{PinPresent: false, CertificateCurrent: false, ProbeOK: false, ZoneMatches: false}
  _ = expectation.Manifest.Operator
}`,
		},
		{"host admission manifest", "internal/hostx/manifest.go", `package hostx; type Manifest struct{ Operator string }`},
		{"guest trust foundation", "internal/guestproto/bootstrap.go", `package guestproto; type bindingManifest struct{ Domain string }`},
		{"serial protocol foundation", "internal/serialx/runtime.go", `package serialx; import _ "github.com/weshofmann/boxwarden/internal/guestproto"`},
		{"SSH foundation", "internal/sshx/client.go", `package sshx; func NewClient() {}; func f(c interface{ Probe() }) { c.Probe() }`},
		{"time-zone foundation", "internal/timezonex/guest.go", `package timezonex; func Converge() {}`},
		{"qualification libproc", "internal/qualification/adr024/proc.go", "package adr024\n/* #cgo LDFLAGS: -lproc\n#include <libproc.h> */\nimport \"C\""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if issues := inspectSliceBSource(test.path, []byte(test.source)); len(issues) != 0 {
				t.Fatalf("unexpected issues: %v", issues)
			}
		})
	}
}

func TestSliceBProductionTreeSatisfiesRuntimeBoundaryPolicy(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", ".."))
	policy := &sliceBPolicy{}
	for _, sourceRoot := range []string{filepath.Join(root, "cmd"), filepath.Join(root, "internal")} {
		err := filepath.WalkDir(sourceRoot, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			relative, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			if entry.IsDir() {
				if isQualificationPath(relative) {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			source, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			policy.inspect(relative, source)
			return nil
		})
		if err != nil {
			t.Fatalf("walk production source %s: %v", sourceRoot, err)
		}
	}
	issues := append(policy.issues, policy.finish()...)
	if len(issues) != 0 {
		t.Fatalf("Slice B production runtime boundary violations:\n%s", strings.Join(issues, "\n"))
	}
}

type sliceBPolicy struct {
	issues                     []string
	ptyRuntimeBindings         int
	ptyParameterCalls          int
	lowLevelPTYCalls           int
	serialRuntimeConstructions int
}

func inspectSliceBSource(path string, source []byte) []string {
	policy := &sliceBPolicy{}
	policy.inspect(path, source)
	return policy.issues
}

func (p *sliceBPolicy) inspect(path string, source []byte) {
	path = filepath.ToSlash(filepath.Clean(path))
	if isQualificationPath(path) || strings.HasSuffix(path, "_test.go") {
		return
	}
	composition := isSliceBCompositionPath(path)
	serialFoundation := strings.HasPrefix(path, "internal/serialx/")

	if libprocSourcePattern.Match(source) {
		p.add(path, "discarded libproc", "libproc header, linker flag, or token")
	}
	if ofdSourcePattern.Match(source) {
		p.add(path, "discarded OFD", "OFD token in production source")
	}
	if hmacSourcePattern.Match(source) {
		p.add(path, "discarded HMAC", "HMAC token in production source")
	}

	file, err := parser.ParseFile(token.NewFileSet(), path, source, parser.ParseComments)
	if err != nil {
		p.add(path, "parse", err.Error())
		return
	}
	for _, imported := range file.Imports {
		importPath := strings.ToLower(strings.Trim(imported.Path.Value, `"`))
		if composition && strings.HasSuffix(importPath, "/internal/timezonex") {
			p.add(path, "deferred Slice C/D import", importPath)
		}
		if composition && !serialFoundation && strings.HasSuffix(importPath, "/internal/guestproto") {
			p.add(path, "deferred Slice C/D import", importPath)
		}
	}

	ast.Inspect(file, func(node ast.Node) bool {
		switch value := node.(type) {
		case *ast.CallExpr:
			name := calledName(value.Fun)
			if name == "allocatePTY" {
				p.add(path, "direct PTY allocation", "allocatePTY must only be passed to the single serial runtime constructor")
			}
			if path == "internal/serialx/runtime.go" && name == "allocate" {
				p.ptyParameterCalls++
			}
			if name == "boxwarden_openpty" || name == "Openpty" || name == "OpenPTY" {
				p.lowLevelPTYCalls++
			}
			if name == "createRuntime" && len(value.Args) == 3 {
				if allocator, ok := value.Args[2].(*ast.Ident); ok && allocator.Name == "allocatePTY" {
					p.ptyRuntimeBindings++
				}
			}
			if composition && name == "CreateRuntime" {
				p.serialRuntimeConstructions++
			}
			if name == "FindProcess" {
				p.add(path, "process reconstruction", "os.FindProcess or equivalent name")
			}
			if composition && isDeferredCall(name) {
				p.add(path, "deferred Slice C/D call", name)
			}
		case *ast.AssignStmt:
			if !composition {
				break
			}
			for index, left := range value.Lhs {
				selector, ok := left.(*ast.SelectorExpr)
				if !ok || !isFutureReadinessField(selector.Sel.Name) {
					continue
				}
				if index >= len(value.Rhs) || !isFalseLiteral(value.Rhs[index]) {
					p.add(path, "deferred readiness publication", selector.Sel.Name)
				}
			}
		case *ast.KeyValueExpr:
			name := expressionName(value.Key)
			if composition && isFutureReadinessField(name) && !isFalseLiteral(value.Value) {
				p.add(path, "deferred readiness publication", name)
			}
			if isPersistedProcessName(name) || isOwnershipMetadataName(name) {
				p.add(path, "persisted process authority", name)
			}
		case *ast.Field:
			for _, name := range value.Names {
				if isPersistedProcessName(name.Name) || isOwnershipMetadataName(name.Name) {
					p.add(path, "persisted process authority", name.Name)
				}
			}
			if value.Tag != nil {
				tag, err := strconv.Unquote(value.Tag.Value)
				if err == nil {
					jsonName := strings.Split(reflect.StructTag(tag).Get("json"), ",")[0]
					if isPersistedProcessName(jsonName) || isOwnershipMetadataName(jsonName) {
						p.add(path, "persisted process authority", jsonName)
					}
				}
			}
		case *ast.TypeSpec:
			name := canonicalMetadataName(value.Name.Name)
			if isOwnershipRecordName(name) {
				p.add(path, "ownership record", value.Name.Name)
			}
			if strings.Contains(name, "manifest") && !manifestFoundationPath(path) {
				p.add(path, "ownership record", value.Name.Name)
			}
		case *ast.BasicLit:
			if value.Kind != token.STRING {
				break
			}
			literal, err := strconv.Unquote(value.Value)
			if err != nil {
				break
			}
			lower := strings.ToLower(literal)
			if lower == "screen" || strings.HasSuffix(lower, "/screen") || lower == "socat" || strings.HasSuffix(lower, "/socat") {
				p.add(path, "discarded Screen/socat", literal)
			}
			if isOwnershipFile(lower) {
				p.add(path, "ownership file", literal)
			}
		}
		return true
	})
}

func (p *sliceBPolicy) finish() []string {
	var issues []string
	for _, check := range []struct {
		got  int
		want int
		name string
	}{
		{p.ptyRuntimeBindings, 1, "allocatePTY binding into createRuntime"},
		{p.ptyParameterCalls, 1, "single allocator invocation in serial runtime"},
		{p.lowLevelPTYCalls, 1, "Darwin low-level PTY allocation"},
		{p.serialRuntimeConstructions, 1, "Slice B serial runtime construction"},
	} {
		if check.got != check.want {
			issues = append(issues, check.name+" count = "+strconv.Itoa(check.got)+", want "+strconv.Itoa(check.want))
		}
	}
	return issues
}

func (p *sliceBPolicy) add(path, category, detail string) {
	p.issues = append(p.issues, path+": "+category+": "+detail)
}

func isQualificationPath(path string) bool {
	path = filepath.ToSlash(filepath.Clean(path))
	return path == "internal/qualification" || strings.HasPrefix(path, "internal/qualification/")
}

func isSliceBCompositionPath(path string) bool {
	for _, foundation := range []string{"cmd/boxwarden-guest-bootstrap/", "internal/guestproto/", "internal/sshx/", "internal/timezonex/"} {
		if strings.HasPrefix(path, foundation) {
			return false
		}
	}
	return strings.HasPrefix(path, "cmd/") || strings.HasPrefix(path, "internal/")
}

func manifestFoundationPath(path string) bool {
	return strings.HasPrefix(path, "internal/hostx/") || strings.HasPrefix(path, "internal/guestproto/")
}

func calledName(expression ast.Expr) string {
	switch value := expression.(type) {
	case *ast.Ident:
		return value.Name
	case *ast.SelectorExpr:
		return value.Sel.Name
	default:
		return ""
	}
}

func expressionName(expression ast.Expr) string {
	switch value := expression.(type) {
	case *ast.Ident:
		return value.Name
	case *ast.BasicLit:
		if value.Kind == token.STRING {
			name, _ := strconv.Unquote(value.Value)
			return name
		}
	}
	return ""
}

func isFalseLiteral(expression ast.Expr) bool {
	value, ok := expression.(*ast.Ident)
	return ok && value.Name == "false"
}

func isDeferredCall(name string) bool {
	switch name {
	case "Admit", "ApplyZone", "Bootstrap", "Converge", "Issue", "NewAddressResolver", "NewCertificateIssuer", "NewClient", "NewPinStore", "Probe", "ReadZone", "Resolve", "WriteKnownHosts":
		return true
	default:
		return false
	}
}

func isFutureReadinessField(name string) bool {
	switch name {
	case "CertificateCurrent", "PinPresent", "ProbeOK", "ZoneMatches":
		return true
	default:
		return false
	}
}

func canonicalMetadataName(name string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			return r
		}
		if r >= 'A' && r <= 'Z' {
			return r + ('a' - 'A')
		}
		return -1
	}, name)
}

func isPersistedProcessName(name string) bool {
	switch canonicalMetadataName(name) {
	case "pid", "processbirth", "processid", "processidentity", "processpid", "processstart", "processstarted", "processstarttime", "supervisorpid", "ownerpid":
		return true
	default:
		return false
	}
}

func isOwnershipMetadataName(name string) bool {
	switch canonicalMetadataName(name) {
	case "ownerpid", "ownerrecord", "ownership", "ownershipmetadata", "ownershiprecord", "ownershipstate", "processowner", "runtimeowner", "supervisorowner":
		return true
	default:
		return false
	}
}

func isOwnershipRecordName(name string) bool {
	return strings.Contains(name, "ownership") && (strings.Contains(name, "record") || strings.Contains(name, "manifest") || strings.Contains(name, "metadata") || strings.Contains(name, "state")) ||
		strings.Contains(name, "ownerrecord") || strings.Contains(name, "supervisormanifest")
}

func isOwnershipFile(value string) bool {
	base := filepath.Base(value)
	switch base {
	case "owner.json", "ownership.json", "ownership-record.json", "runtime-owner.json", "supervisor-manifest.json":
		return true
	}
	return strings.Contains(base, "supervisor-manifest") || strings.Contains(base, "ownership_record") || strings.Contains(base, "ownership-record")
}

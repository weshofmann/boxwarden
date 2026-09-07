package architecture

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestOnlyApprovedCompositionPackagesImportTheTartAdapter(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", ".."))
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imported := range file.Imports {
			pathValue := strings.Trim(imported.Path.Value, "\"")
			if pathValue != "github.com/weshofmann/boxwarden/internal/backend/tart" {
				continue
			}
			relative, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			// Slice B adds a focused detached-child composition package. The
			// common control plane and supervisor remain backend-neutral.
			packageDirectory := filepath.Dir(relative)
			if packageDirectory != filepath.Join("cmd", "boxwarden") && packageDirectory != filepath.Join("internal", "sessionruntime") {
				t.Errorf("%s imports the Tart adapter directly; only cmd/boxwarden and internal/sessionruntime composition may do so", relative)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk source tree: %v", err)
	}
}

func TestQualificationCodeCannotEnterProductionPackages(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", ".."))
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return walkErr
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imported := range file.Imports {
			if !strings.Contains(strings.Trim(imported.Path.Value, "\""), "/internal/qualification/") {
				continue
			}
			relative, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			if !strings.HasPrefix(relative, filepath.Join("internal", "qualification")+string(filepath.Separator)) {
				t.Errorf("%s imports qualification-only code", relative)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk source tree: %v", err)
	}
}

func TestSliceBRuntimeExcludesDiscardedAndDeferredMechanisms(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", ".."))
	runtimeRoots := []string{
		filepath.Join(root, "cmd", "boxwarden"),
		filepath.Join(root, "internal", "app"),
		filepath.Join(root, "internal", "backend", "tart"),
		filepath.Join(root, "internal", "serialx"),
		filepath.Join(root, "internal", "session"),
		filepath.Join(root, "internal", "sessionruntime"),
		filepath.Join(root, "internal", "supervisor"),
	}
	forbiddenDeferredCalls := map[string]bool{
		"Admit": true, "Bootstrap": true, "Converge": true,
		"Issue": true, "NewClient": true, "Resolve": true,
		"WriteKnownHosts": true,
	}
	forbiddenReadyFields := map[string]bool{
		"CertificateCurrent": true, "PinPresent": true,
		"ProbeOK": true, "ZoneMatches": true,
	}
	ptyRuntimeConstructions := 0
	ptyAllocatorCalls := 0
	serialRuntimeCreations := 0

	for _, sourceRoot := range runtimeRoots {
		err := filepath.WalkDir(sourceRoot, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return walkErr
			}
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ParseComments)
			if err != nil {
				return err
			}
			relative, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			inSessionRuntime := filepath.Dir(relative) == filepath.Join("internal", "sessionruntime")
			inRuntimeAuthority := inSessionRuntime || filepath.Dir(relative) == filepath.Join("internal", "supervisor")
			for _, imported := range file.Imports {
				pathValue := strings.ToLower(strings.Trim(imported.Path.Value, "\""))
				if strings.Contains(pathValue, "hmac") || strings.Contains(pathValue, "libproc") {
					t.Errorf("%s imports discarded runtime authority mechanism %q", relative, pathValue)
				}
				if inSessionRuntime && (strings.HasSuffix(pathValue, "/internal/guestproto") || strings.HasSuffix(pathValue, "/internal/timezonex")) {
					t.Errorf("%s imports deferred Slice C/D composition %q", relative, pathValue)
				}
			}
			ast.Inspect(file, func(node ast.Node) bool {
				switch value := node.(type) {
				case *ast.CallExpr:
					name := calledName(value.Fun)
					if name == "boxwarden_openpty" || name == "Openpty" || name == "OpenPTY" {
						ptyAllocatorCalls++
					}
					if name == "createRuntime" && len(value.Args) == 3 {
						if allocator, ok := value.Args[2].(*ast.Ident); ok && allocator.Name == "allocatePTY" {
							ptyRuntimeConstructions++
						}
					}
					if name == "CreateRuntime" && inSessionRuntime {
						serialRuntimeCreations++
					}
					if name == "FindProcess" {
						t.Errorf("%s reconstructs process authority with os.FindProcess", relative)
					}
					if inSessionRuntime && forbiddenDeferredCalls[name] {
						t.Errorf("%s invokes deferred Slice C/D mechanism %s", relative, name)
					}
				case *ast.AssignStmt:
					if !inSessionRuntime {
						break
					}
					for _, expression := range value.Lhs {
						if selector, ok := expression.(*ast.SelectorExpr); ok && forbiddenReadyFields[selector.Sel.Name] {
							t.Errorf("%s publishes deferred readiness field %s", relative, selector.Sel.Name)
						}
					}
				case *ast.Field:
					for _, name := range value.Names {
						normalized := strings.ToLower(name.Name)
						if normalized == "pid" || normalized == "processpid" || normalized == "supervisorpid" || normalized == "processstart" || normalized == "processstarttime" || normalized == "ownershipmanifest" {
							t.Errorf("%s declares discarded persisted process authority field %s", relative, name.Name)
						}
					}
					if value.Tag != nil {
						tag, _ := strconv.Unquote(value.Tag.Value)
						lower := strings.ToLower(tag)
						if strings.Contains(lower, `json:"pid`) || strings.Contains(lower, `json:"process_pid`) || strings.Contains(lower, `json:"supervisor_pid`) || strings.Contains(lower, `json:"process_start`) || strings.Contains(lower, `json:"ownership_manifest`) {
							t.Errorf("%s persists discarded process authority in struct tag %q", relative, tag)
						}
					}
				case *ast.TypeSpec:
					if inRuntimeAuthority && strings.Contains(strings.ToLower(value.Name.Name), "manifest") {
						t.Errorf("%s declares discarded runtime ownership manifest type %s", relative, value.Name.Name)
					}
				case *ast.SelectorExpr:
					if strings.Contains(strings.ToLower(value.Sel.Name), "ofd") {
						t.Errorf("%s uses discarded OFD authority mechanism %s", relative, value.Sel.Name)
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
					if lower == "screen" || strings.HasSuffix(lower, "/screen") || lower == "socat" || strings.HasSuffix(lower, "/socat") || strings.Contains(lower, "supervisor-manifest") || strings.Contains(lower, "process_start") {
						t.Errorf("%s contains discarded runtime command or metadata token %q", relative, literal)
					}
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatalf("walk production runtime source %s: %v", sourceRoot, err)
		}
	}
	if ptyRuntimeConstructions != 1 {
		t.Errorf("production serial runtime has %d PTY-backed construction paths, want exactly one", ptyRuntimeConstructions)
	}
	if ptyAllocatorCalls != 1 {
		t.Errorf("production serial runtime makes %d PTY allocator calls, want exactly one", ptyAllocatorCalls)
	}
	if serialRuntimeCreations != 1 {
		t.Errorf("Slice B runtime composition creates %d serial runtimes, want exactly one", serialRuntimeCreations)
	}
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

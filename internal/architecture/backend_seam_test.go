package architecture

import (
	"go/build/constraint"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
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
			source, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			if !qualificationCompositionAllowed(filepath.ToSlash(relative), source) {
				t.Errorf("%s imports qualification-only code without an exact admitted composition constraint", relative)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk source tree: %v", err)
	}
}

// Keep the whole-tree import guard. Only these reviewed composition files may
// cross it, and their parsed constraints must exclude every production graph.
func qualificationCompositionAllowed(relative string, source []byte) bool {
	if strings.HasPrefix(relative, "internal/qualification/") {
		return true
	}
	cleanup := false
	switch relative {
	case "cmd/n1-attend/main.go", "cmd/n1-attend-root/main.go":
	case "cmd/n1-cleanup/main.go", "internal/hostx/diagnostic_cleanup.go":
		cleanup = true
	default:
		return false
	}
	file, err := parser.ParseFile(token.NewFileSet(), relative, source, parser.ImportsOnly|parser.ParseComments)
	if err != nil {
		return false
	}
	packageOffset := int(file.Package) - 1
	// A constraint must be an actual leading line comment, not text inside a
	// string/block comment; reject duplicate, late and legacy directives too.
	directive := ""
	offset := 0
	for _, line := range strings.Split(string(source), "\n") {
		trimmed := strings.TrimSpace(line)
		if constraint.IsPlusBuild(trimmed) || strings.HasPrefix(trimmed, "// +build") {
			return false
		}
		if strings.HasPrefix(trimmed, "//go:build") {
			if directive != "" || offset >= packageOffset || line != trimmed || !strings.HasPrefix(line, "//go:build ") {
				return false
			}
			actualComment := false
			for _, group := range file.Comments {
				for _, comment := range group.List {
					if int(comment.Pos())-1 == offset && comment.Text == line {
						actualComment = true
					}
				}
			}
			if !actualComment || len(line) > 1024 {
				return false
			}
			directive = line
		}
		offset += len(line) + 1
	}
	if directive == "" {
		return false
	}
	expr, err := constraint.Parse(directive)
	if err != nil {
		return false
	}
	tags := map[string]bool{"n1diagnostic": true, "n1candidate": true, "n1cleanup": true}
	nodes := 0
	var collect func(constraint.Expr) bool
	collect = func(e constraint.Expr) bool {
		nodes++
		if nodes > 64 {
			return false
		}
		switch x := e.(type) {
		case *constraint.TagExpr:
			tags[x.Tag] = true
			return len(tags) <= 8
		case *constraint.NotExpr:
			return collect(x.X)
		case *constraint.AndExpr:
			return collect(x.X) && collect(x.Y)
		case *constraint.OrExpr:
			return collect(x.X) && collect(x.Y)
		default:
			return false
		}
	}
	if !collect(expr) {
		return false
	}
	names := make([]string, 0, len(tags))
	for tag := range tags {
		names = append(names, tag)
	}
	// At most eight referenced/required tags: exhaustive <=256 assignments is
	// independent of the test binary's active GOOS/tag/candidate configuration.
	satisfiable := false
	for mask := 0; mask < 1<<len(names); mask++ {
		enabled := func(tag string) bool {
			for i, name := range names {
				if name == tag {
					return mask&(1<<i) != 0
				}
			}
			return false
		}
		if !expr.Eval(enabled) {
			continue
		}
		satisfiable = true
		if !enabled("n1diagnostic") || enabled("n1candidate") || cleanup && !enabled("n1cleanup") {
			return false
		}
	}
	return satisfiable
}

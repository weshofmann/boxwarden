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
			source, readErr := os.ReadFile(path)
			exactH := readErr == nil && tartQualificationCompositionAllowed(filepath.ToSlash(relative), source)
			if !exactH && packageDirectory != filepath.Join("cmd", "boxwarden") && packageDirectory != filepath.Join("internal", "sessionruntime") {
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
	required := []string{"n1diagnostic"}
	forbidden := []string{"n1candidate"}
	switch relative {
	case "cmd/n1-attend/main.go", "cmd/n1-attend-root/main.go":
	case "cmd/n1-cleanup/main.go", "internal/hostx/diagnostic_cleanup.go":
		required = append(required, "n1cleanup")
	case "cmd/n1-window/main.go", "cmd/n1-run-window/main.go", "cmd/n1-closeout/main.go", "cmd/n1-candidate-worker/main.go":
		required = append(required, "n1clipboarddiagnostic")
	case "cmd/n1-stock-worker/main.go":
		required = []string{"n1clipboarddiagnostic"}
		forbidden = append(forbidden, "n1diagnostic")
	default:
		return false
	}
	return sourceImpliesTags(relative, source, required, forbidden)
}

// Reuse the same finite actual-leading-comment implication proof for exact
// diagnostic correlation files. This never grants a path exemption itself.
func sourceImpliesTags(relative string, source []byte, required, forbidden []string) bool {
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
	for _, tag := range append(append([]string(nil), required...), forbidden...) {
		tags[tag] = true
	}
	if len(tags) > 8 {
		return false
	}
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
		for _, tag := range required {
			if !enabled(tag) {
				return false
			}
		}
		for _, tag := range forbidden {
			if enabled(tag) {
				return false
			}
		}
	}
	return satisfiable
}

func tartQualificationCompositionAllowed(relative string, source []byte) bool {
	switch relative {
	case "cmd/n1-cleanup/main.go", "cmd/n1-stock-worker/main.go", "cmd/n1-candidate-worker/main.go":
		return qualificationCompositionAllowed(relative, source)
	default:
		return false
	}
}
func TestOnlyExactConstrainedCleanupMainCanComposeReadOnlyTart(t *testing.T) {
	for _, x := range []struct {
		p, tag string
		good   bool
	}{{"cmd/n1-cleanup/main.go", "n1diagnostic && n1cleanup && !n1candidate", true}, {"cmd/n1-cleanup/sibling.go", "n1diagnostic && n1cleanup && !n1candidate", false}, {"cmd/n1-cleanup/main.go", "n1diagnostic && !n1candidate", false}, {"cmd/n1-cleanup/main.go", "n1diagnostic || n1cleanup", false}, {"cmd/n1-cleanup/main.go", "", false}} {
		s := "package main\n"
		if x.tag != "" {
			s = "//go:build " + x.tag + "\n\n" + s
		}
		if tartQualificationCompositionAllowed(x.p, []byte(s)) != x.good {
			t.Fatalf("%+v", x)
		}
	}
}

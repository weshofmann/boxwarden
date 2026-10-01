package n1

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestRestrictedAttendanceGraphsAndPureContractSource(t *testing.T) {
	base, e := filepath.EvalSymlinks("../../..")
	if e != nil {
		t.Fatal(e)
	}
	base, e = filepath.Abs(base)
	if e != nil {
		t.Fatal(e)
	}
	prefix := "github.com/weshofmann/boxwarden/"
	for _, start := range []string{"internal/qualification/n1/attenduser", "internal/qualification/n1/attendroot", "internal/qualification/n1/contract"} {
		seen := map[string]bool{}
		var walk func(string)
		walk = func(rel string) {
			if seen[rel] {
				return
			}
			seen[rel] = true
			if rel != start && (rel == "internal/hostx" || strings.HasPrefix(rel, "internal/session") || strings.HasPrefix(rel, "internal/lifecycle") || rel == "internal/qualification/n1" || strings.HasPrefix(rel, "internal/qualification/n1/receipt") || strings.HasPrefix(rel, "cmd/")) {
				t.Fatal("restricted graph acquired effect capability", start, rel)
			}
			files, e := os.ReadDir(filepath.Join(base, rel))
			if e != nil {
				t.Fatal(e)
			}
			for _, file := range files {
				if file.IsDir() || !strings.HasSuffix(file.Name(), ".go") || strings.HasSuffix(file.Name(), "_test.go") {
					continue
				}
				f, e := parser.ParseFile(token.NewFileSet(), filepath.Join(base, rel, file.Name()), nil, parser.ImportsOnly)
				if e != nil {
					t.Fatal(e)
				}
				for _, imp := range f.Imports {
					name, _ := strconv.Unquote(imp.Path.Value)
					if rel == "internal/qualification/n1/contract" && (name == "os" || name == "os/exec" || name == "syscall" || name == "net" || name == "C") {
						t.Fatal("contract has effects", name)
					}
					if strings.HasPrefix(name, prefix) {
						walk(strings.TrimPrefix(name, prefix))
					}
				}
			}
		}
		walk(start)
	}
}

// This is a complete source walk, independent of active test tags. F is walked
// only once its source owner creates that graph; an absent F is never a proof.
func TestRestrictedLauncherAndExistingCloseoutGraphs(t *testing.T) {
	base, e := filepath.Abs(filepath.Clean("../../.."))
	if e != nil {
		t.Fatal(e)
	}
	starts := []string{"internal/qualification/n1/caller"}
	if _, e := os.Stat(filepath.Join(base, "internal/qualification/n1/closeout")); e == nil {
		starts = append(starts, "internal/qualification/n1/closeout")
	} else if !os.IsNotExist(e) {
		t.Fatal(e)
	}
	prefix := "github.com/weshofmann/boxwarden/"
	for _, start := range starts {
		t.Run(start, func(t *testing.T) {
			seen := map[string]bool{}
			var walk func(string)
			walk = func(rel string) {
				if seen[rel] {
					return
				}
				seen[rel] = true
				if rel != start && (rel == "internal/hostx" || strings.HasPrefix(rel, "internal/backend") || strings.HasPrefix(rel, "internal/session") || strings.HasPrefix(rel, "internal/lifecycle") || rel == "internal/sshx" || rel == "internal/supervisor" || rel == "internal/networkdiag" || rel == "internal/qualification/n1" || strings.HasPrefix(rel, "internal/qualification/n1/receipt") || strings.HasPrefix(rel, "cmd/")) {
					t.Fatal("launcher/closeout acquired lifecycle or runtime actor", start, rel)
				}
				files, e := os.ReadDir(filepath.Join(base, rel))
				if e != nil {
					t.Fatal(e)
				}
				for _, file := range files {
					if file.IsDir() || !strings.HasSuffix(file.Name(), ".go") || strings.HasSuffix(file.Name(), "_test.go") {
						continue
					}
					f, e := parser.ParseFile(token.NewFileSet(), filepath.Join(base, rel, file.Name()), nil, parser.ImportsOnly)
					if e != nil {
						t.Fatal(e)
					}
					for _, imp := range f.Imports {
						name, e := strconv.Unquote(imp.Path.Value)
						if e != nil {
							t.Fatal(e)
						}
						if strings.HasPrefix(name, prefix) {
							walk(strings.TrimPrefix(name, prefix))
						}
					}
				}
			}
			walk(start)
		})
	}
}

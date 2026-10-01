//go:build darwin

package pathmeta

import (
	"context"
	"errors"
	"github.com/weshofmann/boxwarden/internal/execx"
	"path/filepath"
	"strings"
	"time"
)

type OSInspector struct{}

func (OSInspector) HasExtendedACL(path string) (bool, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return false, errors.New("n1 ACL refused")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r, e := (execx.OSRunner{MaxOutputBytes: 8 << 10, StrictStderr: true}).Run(ctx, execx.Command{Path: "/bin/ls", Args: []string{"-lde", path}, Env: []string{"LC_ALL=C", "LANG=C"}, Stdin: []byte{}})
	if e != nil || r.Truncated || r.Stderr != "" || !r.StderrComplete {
		return false, errors.New("n1 ACL refused")
	}
	line, entries := r.Stdout, ""
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		entries = line[i+1:]
		line = line[:i]
	}
	fields := strings.Fields(line)
	if len(fields) < 9 {
		return false, errors.New("n1 ACL refused")
	}
	mode := fields[0]
	if len(mode) < 10 || len(mode) > 11 || !strings.ContainsRune("-bcdlps", rune(mode[0])) {
		return false, errors.New("n1 ACL refused")
	}
	for _, c := range mode[1:10] {
		if !strings.ContainsRune("rwxStTs-", c) {
			return false, errors.New("n1 ACL refused")
		}
	}
	if len(mode) == 11 && mode[10] != '@' && mode[10] != '+' {
		return false, errors.New("n1 ACL refused")
	}
	return strings.HasSuffix(mode, "+") || strings.TrimSpace(entries) != "", nil
}

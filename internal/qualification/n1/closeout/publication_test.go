package closeout

import (
	"context"
	"errors"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/fixed"
	"os"
	"path/filepath"
	"testing"
)

func TestCloseoutRecordPublicationReturnAndNoOverwrite(t *testing.T) {
	for _, mode := range []string{"positive", "close-after-effect", "sync", "write", "expired", "name"} {
		t.Run(mode, func(t *testing.T) {
			r, e := filepath.EvalSymlinks(t.TempDir())
			if e != nil {
				t.Fatal(e)
			}
			os.Chmod(r, 0700)
			closeFile := func(f *os.File) error {
				e := f.Close()
				if mode == "close-after-effect" {
					return errors.Join(e, errors.New("reported close error"))
				}
				return e
			}
			syncFile := func(f *os.File) error {
				if mode == "sync" {
					return errors.New("fsync refused")
				}
				return f.Sync()
			}
			write := func(f *os.File, b []byte) (int, error) {
				if mode == "write" {
					return 0, errors.New("write refused")
				}
				return f.Write(b)
			}
			tick := func() error {
				if mode == "expired" {
					return context.DeadlineExceeded
				}
				return nil
			}
			name := "closeout-intent.json"
			if mode == "name" {
				name = "../escape"
			}
			body := []byte("{\"version\":1}")
			e = publishRecord(t.Context(), r, os.Getuid(), noACL{}, name, body, tick, closeFile, syncFile, write)
			if mode == "positive" {
				if e != nil {
					t.Fatal(e)
				}
				b, e := os.ReadFile(filepath.Join(r, name))
				if e != nil || string(b) != string(body) {
					t.Fatal(e)
				}
				if publishRecord(t.Context(), r, os.Getuid(), noACL{}, name, body, tick, closeFile, syncFile, write) == nil {
					t.Fatal("existing record adopted")
				}
			} else if e == nil {
				t.Fatal("uncertain publication became success", mode)
			}
			if mode == "close-after-effect" {
				if _, e = os.Lstat(filepath.Join(r, name)); e != nil {
					t.Fatal("after-effect fixture not exercised")
				}
			}
			if mode == "expired" || mode == "name" {
				es, e := os.ReadDir(r)
				if e != nil || len(es) != 0 {
					t.Fatal("effect before gate", mode, e)
				}
			}
		})
	}
}
func TestStockDoctorRequiresActualClosedHealthyReturn(t *testing.T) {
	good := fixed.ChildResult{Raw: []byte("status: healthy\n"), Exit: 0, Closed: true}
	if doctorReturn(good, nil) != nil {
		t.Fatal("healthy return refused")
	}
	for _, r := range []fixed.ChildResult{{Raw: good.Raw, Exit: 1, Closed: true}, {Raw: good.Raw, Exit: 0, Closed: false}, {Raw: []byte("status: drifted/unsafe\n"), Exit: 0, Closed: true}, {Raw: append(append([]byte(nil), good.Raw...), []byte("extra\n")...), Exit: 0, Closed: true}} {
		if doctorReturn(r, nil) == nil {
			t.Fatal("ambiguous doctor accepted")
		}
	}
	if doctorReturn(good, errors.New("actual wait failed")) == nil {
		t.Fatal("marker replaced return")
	}
}

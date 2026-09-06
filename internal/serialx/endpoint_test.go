//go:build darwin || linux

package serialx

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// Missing retention permits inode reuse; missing close leaks the pinned inode
// on either ordinary shutdown or tamper refusal.
func TestRuntimeRetainsEndpointHandlesUntilShutdown(t *testing.T) {
	for _, tamper := range []bool{false, true} {
		t.Run(map[bool]string{false: "normal", true: "replacement"}[tamper], func(t *testing.T) {
			root := t.TempDir()
			if err := os.Chmod(root, 0o700); err != nil {
				t.Fatal(err)
			}
			deps := testRuntimeDeps(&screenStarterFake{}, &ptyAllocatorFake{})
			var handles []*os.File
			deps.openLink = recordEndpointHandles(&handles)
			runtime, err := createRuntime(context.Background(), root, "handles", qualifiedScreenFact(), deps)
			if err != nil {
				t.Fatal(err)
			}
			defer runtime.Close()
			if len(handles) != 2 {
				t.Fatalf("retained endpoint handles = %d, want 2", len(handles))
			}
			for _, handle := range handles {
				info, err := handle.Stat()
				if err != nil || info.Mode()&os.ModeSymlink == 0 {
					t.Fatalf("endpoint handle does not retain symlink: %v", err)
				}
			}
			if tamper {
				target, err := os.Readlink(runtime.TartSlave)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(runtime.TartSlave); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, runtime.TartSlave); err != nil {
					t.Fatal(err)
				}
			}
			if err := runtime.Shutdown(context.Background()); (err != nil) != tamper {
				t.Fatalf("Shutdown() = %v, want error = %v", err, tamper)
			}
			assertEndpointHandlesClosed(t, handles)
			if tamper {
				if _, err := os.Lstat(runtime.TartSlave); err != nil {
					t.Fatalf("replacement was removed: %v", err)
				}
			}
		})
	}
}

// Opening the second link, statting a returned handle, and launching Screen
// can all fail after earlier descriptors have been retained.
func TestCreateRuntimeClosesEndpointHandlesOnFailure(t *testing.T) {
	for _, failure := range []string{"second-open", "handle-stat", "screen-start"} {
		t.Run(failure, func(t *testing.T) {
			root := t.TempDir()
			if err := os.Chmod(root, 0o700); err != nil {
				t.Fatal(err)
			}
			starter := &screenStarterFake{err: errors.New("screen start failure")}
			deps := testRuntimeDeps(starter, &ptyAllocatorFake{})
			var handles []*os.File
			opener := recordEndpointHandles(&handles)
			calls := 0
			deps.openLink = func(path string) (*os.File, error) {
				calls++
				if calls == 2 && failure == "second-open" {
					return nil, errors.New("link open failure")
				}
				file, err := opener(path)
				if err == nil && calls == 2 && failure == "handle-stat" {
					err = file.Close()
				}
				return file, err
			}
			if _, err := createRuntime(context.Background(), root, "failure", qualifiedScreenFact(), deps); err == nil {
				t.Fatal("createRuntime() accepted injected failure")
			}
			if len(handles) == 0 {
				t.Fatal("no endpoint handle was acquired before failure")
			}
			assertEndpointHandlesClosed(t, handles)
			if failure != "screen-start" && starter.called {
				t.Fatal("Screen started after endpoint capture failure")
			}
		})
	}
}

// The absolute opener can resolve a different generation than the retained
// os.Root. Matching target text must not admit that different link inode.
func TestCreateRuntimeRejectsEndpointOpenOutsideRetainedGeneration(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	starter := &screenStarterFake{}
	deps := testRuntimeDeps(starter, &ptyAllocatorFake{})
	var handles []*os.File
	opener := recordEndpointHandles(&handles)
	deps.openLink = func(path string) (*os.File, error) {
		target, err := os.Readlink(path)
		if err != nil {
			return nil, err
		}
		directory := filepath.Dir(path)
		if err := os.Rename(directory, directory+"-original"); err != nil {
			return nil, err
		}
		if err := os.Mkdir(directory, 0o700); err != nil {
			return nil, err
		}
		if err := os.Symlink(target, path); err != nil {
			return nil, err
		}
		return opener(path)
	}
	if _, err := createRuntime(context.Background(), root, "redirected", qualifiedScreenFact(), deps); err == nil {
		t.Fatal("createRuntime() admitted endpoint outside retained generation")
	}
	if starter.called {
		t.Fatal("Screen started with a redirected endpoint")
	}
	if len(handles) != 1 {
		t.Fatalf("endpoint handles = %d, want one before refusal", len(handles))
	}
	assertEndpointHandlesClosed(t, handles)
	for _, generation := range []string{"redirected", "redirected-original"} {
		if _, err := os.Lstat(filepath.Join(root, generation, "tart-serial")); err != nil {
			t.Fatalf("unproven link in %s removed: %v", generation, err)
		}
	}
}

func recordEndpointHandles(handles *[]*os.File) func(string) (*os.File, error) {
	return func(path string) (*os.File, error) {
		file, err := openEndpointLink(path)
		if err == nil {
			*handles = append(*handles, file)
		}
		return file, err
	}
}

func assertEndpointHandlesClosed(t *testing.T, handles []*os.File) {
	t.Helper()
	for _, handle := range handles {
		if _, err := handle.Stat(); !errors.Is(err, os.ErrClosed) {
			t.Errorf("endpoint handle remained open: %v", err)
		}
	}
}

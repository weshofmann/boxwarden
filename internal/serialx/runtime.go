package serialx

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// Root is an existing, exact owner-private runtime parent directory.
type Root = string
type Runtime struct {
	TartSlave, OperatorSlave   string
	TartMaster, OperatorMaster *os.File
	Screen                     ScreenChild
	generationDir              string
}
type ptyAllocator interface {
	Allocate() (*os.File, *os.File, error)
}
type systemPTYAllocator struct{}

func (systemPTYAllocator) Allocate() (*os.File, *os.File, error) { return allocatePTY() }

func CreateRuntime(ctx context.Context, root Root, generation string, screen ScreenBinary, starter ScreenStarter) (Runtime, error) {
	return createRuntime(ctx, root, generation, screen, starter, systemPTYAllocator{})
}
func createRuntime(ctx context.Context, root Root, generation string, screen ScreenBinary, starter ScreenStarter, allocator ptyAllocator) (Runtime, error) {
	if err := ctx.Err(); err != nil {
		return Runtime{}, err
	}
	if err := safeRuntimeRoot(root); err != nil {
		return Runtime{}, err
	}
	if !safeGeneration(generation) {
		return Runtime{}, fmt.Errorf("generation is unsafe")
	}
	if allocator == nil {
		return Runtime{}, fmt.Errorf("PTY allocator is required")
	}
	directory := filepath.Join(root, generation)
	if _, err := os.Lstat(directory); err == nil {
		return Runtime{}, fmt.Errorf("generation directory already exists")
	} else if !os.IsNotExist(err) {
		return Runtime{}, fmt.Errorf("inspect generation directory: %w", err)
	}
	if err := os.Mkdir(directory, 0o700); err != nil {
		return Runtime{}, fmt.Errorf("create generation directory: %w", err)
	}
	runtime := Runtime{generationDir: directory}
	cleanup := func(err error) (Runtime, error) { _ = runtime.Close(); return Runtime{}, err }
	tartMaster, tartSlave, err := allocator.Allocate()
	if err != nil {
		return cleanup(fmt.Errorf("allocate Tart PTY: %w", err))
	}
	runtime.TartMaster = tartMaster
	operatorMaster, operatorSlave, err := allocator.Allocate()
	if err != nil {
		_ = tartSlave.Close()
		return cleanup(fmt.Errorf("allocate operator PTY: %w", err))
	}
	runtime.OperatorMaster = operatorMaster
	defer tartSlave.Close()
	defer operatorSlave.Close()
	if err := checkPrivatePTY(tartSlave); err != nil {
		return cleanup(err)
	}
	if err := checkPrivatePTY(operatorSlave); err != nil {
		return cleanup(err)
	}
	runtime.TartSlave = filepath.Join(directory, "tart-serial")
	runtime.OperatorSlave = filepath.Join(directory, "operator-console")
	if err := os.Symlink(tartSlave.Name(), runtime.TartSlave); err != nil {
		return cleanup(fmt.Errorf("link Tart slave: %w", err))
	}
	if err := os.Symlink(operatorSlave.Name(), runtime.OperatorSlave); err != nil {
		return cleanup(fmt.Errorf("link operator slave: %w", err))
	}
	child, err := StartScreen(ctx, starter, screen, runtime.OperatorSlave, "boxwarden-"+generation)
	if err != nil {
		return cleanup(fmt.Errorf("start Screen: %w", err))
	}
	runtime.Screen = child
	return runtime, nil
}
func (r Runtime) Close() error {
	var first error
	for _, file := range []*os.File{r.TartMaster, r.OperatorMaster} {
		if file != nil {
			if err := file.Close(); err != nil && first == nil {
				first = err
			}
		}
	}
	if r.generationDir != "" {
		for _, path := range []string{r.TartSlave, r.OperatorSlave} {
			if path != "" {
				if err := os.Remove(path); err != nil && !os.IsNotExist(err) && first == nil {
					first = err
				}
			}
		}
		if err := os.Remove(r.generationDir); err != nil && !os.IsNotExist(err) && first == nil {
			first = err
		}
	}
	return first
}

// WatchScreen binds broker health to this exact direct child. The caller owns
// when to install the watcher; no process lookup, PID adoption, or Screen
// control channel is involved.
func (r Runtime) WatchScreen(broker *Broker) {
	if r.Screen == nil || broker == nil {
		return
	}
	go func() { broker.ChildLost(r.Screen.Wait()) }()
}

func safeRuntimeRoot(root string) error {
	if root == "" || !filepath.IsAbs(root) || filepath.Clean(root) != root || root == "/" {
		return fmt.Errorf("runtime root must be canonical and non-root")
	}
	info, err := os.Lstat(root)
	if err != nil {
		return fmt.Errorf("inspect runtime root: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o700 || !ownedByCurrentUser(info) {
		return fmt.Errorf("runtime root is unsafe")
	}
	return nil
}
func safeGeneration(value string) bool {
	if value == "" || len(value) > 128 || value == "." || value == ".." {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return !strings.Contains(value, "/")
}
func checkPrivatePTY(file *os.File) error {
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("inspect PTY slave: %w", err)
	}
	if info.Mode().Perm() != 0o600 || !ownedByCurrentUser(info) {
		return fmt.Errorf("PTY slave is not owner-private")
	}
	return nil
}
func ownedByCurrentUser(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && int(stat.Uid) == os.Getuid()
}

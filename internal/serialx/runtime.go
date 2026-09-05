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
	TartSlave, OperatorSlave     string
	TartMaster, OperatorMaster   *os.File
	Screen                       ScreenChild
	generationDir                string
	root, generationRoot         *os.Root
	generationDev, generationIno uint64
	tartLink, operatorLink       endpointIdentity
}
type endpointIdentity struct {
	name, target               string
	dev, ino, linkDev, linkIno uint64
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
	rootHandle, err := os.OpenRoot(root)
	if err != nil {
		return Runtime{}, fmt.Errorf("open runtime root: %w", err)
	}
	before, err := os.Lstat(root)
	if err != nil {
		rootHandle.Close()
		return Runtime{}, fmt.Errorf("reinspect runtime root: %w", err)
	}
	opened, err := rootHandle.Stat(".")
	if err != nil || !sameFileIdentity(before, opened) || !privateDirectory(opened) {
		rootHandle.Close()
		return Runtime{}, fmt.Errorf("runtime root changed during admission")
	}
	if _, err := rootHandle.Lstat(generation); err == nil {
		rootHandle.Close()
		return Runtime{}, fmt.Errorf("generation directory already exists")
	} else if !os.IsNotExist(err) {
		rootHandle.Close()
		return Runtime{}, fmt.Errorf("inspect generation directory: %w", err)
	}
	if err := rootHandle.Mkdir(generation, 0o700); err != nil {
		rootHandle.Close()
		return Runtime{}, fmt.Errorf("create generation directory: %w", err)
	}
	generationRoot, err := rootHandle.OpenRoot(generation)
	if err != nil {
		rootHandle.Remove(generation)
		rootHandle.Close()
		return Runtime{}, fmt.Errorf("open generation directory: %w", err)
	}
	directory := filepath.Join(root, generation)
	genInfo, err := generationRoot.Stat(".")
	if err != nil {
		generationRoot.Close()
		rootHandle.Remove(generation)
		rootHandle.Close()
		return Runtime{}, fmt.Errorf("inspect generation directory: %w", err)
	}
	dev, ino, ok := deviceIdentity(genInfo)
	if !ok {
		generationRoot.Close()
		rootHandle.Remove(generation)
		rootHandle.Close()
		return Runtime{}, fmt.Errorf("generation identity unavailable")
	}
	runtime := Runtime{generationDir: directory, root: rootHandle, generationRoot: generationRoot, generationDev: dev, generationIno: ino}
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
	if err := generationRoot.Symlink(tartSlave.Name(), "tart-serial"); err != nil {
		return cleanup(fmt.Errorf("link Tart slave: %w", err))
	}
	runtime.tartLink, err = identityForEndpoint("tart-serial", tartSlave.Name())
	if err != nil {
		return cleanup(err)
	}
	if err := runtime.captureLinkIdentity(&runtime.tartLink); err != nil {
		return cleanup(err)
	}
	if err := generationRoot.Symlink(operatorSlave.Name(), "operator-console"); err != nil {
		return cleanup(fmt.Errorf("link operator slave: %w", err))
	}
	runtime.operatorLink, err = identityForEndpoint("operator-console", operatorSlave.Name())
	if err != nil {
		return cleanup(err)
	}
	if err := runtime.captureLinkIdentity(&runtime.operatorLink); err != nil {
		return cleanup(err)
	}
	child, err := StartScreen(ctx, starter, screen, operatorSlave, "boxwarden-"+generation)
	if err != nil {
		return cleanup(fmt.Errorf("start Screen: %w", err))
	}
	runtime.Screen = child
	return runtime, nil
}
func (r Runtime) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), ExchangeDeadline)
	defer cancel()
	return r.Shutdown(ctx)
}

// Shutdown stops and reaps only the Screen child returned by this runtime's
// exact starter evidence before closing endpoints and attempting cleanup.
func (r Runtime) Shutdown(ctx context.Context) error {
	var first error
	if r.Screen != nil {
		if err := r.Screen.Stop(ctx); err != nil && first == nil {
			first = err
		}
		if err := r.Screen.Wait(ctx); err != nil && first == nil {
			first = err
		}
	}
	for _, file := range []*os.File{r.TartMaster, r.OperatorMaster} {
		if file != nil {
			if err := file.Close(); err != nil && first == nil {
				first = err
			}
		}
	}
	if r.generationRoot != nil && r.root != nil {
		for _, endpoint := range []endpointIdentity{r.tartLink, r.operatorLink} {
			if err := r.revalidateEndpoint(endpoint); err != nil {
				if first == nil {
					first = err
				}
				continue
			}
			if err := r.generationRoot.Remove(endpoint.name); err != nil && first == nil {
				first = err
			}
		}
		if err := r.generationRoot.Close(); err != nil && first == nil {
			first = err
		}
		if first == nil {
			if info, err := r.root.Lstat(filepath.Base(r.generationDir)); err != nil || !sameDeviceIdentity(info, r.generationDev, r.generationIno) || !privateDirectory(info) {
				first = fmt.Errorf("generation directory changed before cleanup")
			} else if err := r.root.Remove(filepath.Base(r.generationDir)); err != nil && first == nil {
				first = err
			}
		}
		if err := r.root.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func identityForEndpoint(name, target string) (endpointIdentity, error) {
	info, err := os.Stat(target)
	if err != nil {
		return endpointIdentity{}, fmt.Errorf("inspect PTY endpoint: %w", err)
	}
	dev, ino, ok := deviceIdentity(info)
	if !ok {
		return endpointIdentity{}, fmt.Errorf("PTY endpoint identity unavailable")
	}
	return endpointIdentity{name: name, target: target, dev: dev, ino: ino}, nil
}
func (r Runtime) revalidateEndpoint(endpoint endpointIdentity) error {
	if endpoint.name == "" {
		return fmt.Errorf("endpoint identity missing")
	}
	info, err := r.generationRoot.Lstat(endpoint.name)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		return fmt.Errorf("endpoint link changed")
	}
	if !sameDeviceIdentity(info, endpoint.linkDev, endpoint.linkIno) {
		return fmt.Errorf("endpoint link identity changed")
	}
	target, err := r.generationRoot.Readlink(endpoint.name)
	if err != nil || target != endpoint.target {
		return fmt.Errorf("endpoint link target changed")
	}
	actual, err := os.Stat(target)
	if err != nil {
		return fmt.Errorf("endpoint target unavailable: %w", err)
	}
	dev, ino, ok := deviceIdentity(actual)
	if !ok || dev != endpoint.dev || ino != endpoint.ino {
		return fmt.Errorf("endpoint target identity changed")
	}
	return nil
}
func (r Runtime) captureLinkIdentity(endpoint *endpointIdentity) error {
	info, err := r.generationRoot.Lstat(endpoint.name)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		return fmt.Errorf("inspect endpoint link: %w", err)
	}
	dev, ino, ok := deviceIdentity(info)
	if !ok {
		return fmt.Errorf("endpoint link identity unavailable")
	}
	endpoint.linkDev, endpoint.linkIno = dev, ino
	return nil
}
func privateDirectory(info os.FileInfo) bool {
	return info.IsDir() && info.Mode().Perm() == 0o700 && ownedByCurrentUser(info)
}
func sameFileIdentity(left, right os.FileInfo) bool {
	dl, il, ok := deviceIdentity(left)
	if !ok {
		return false
	}
	dr, ir, ok := deviceIdentity(right)
	return ok && dl == dr && il == ir
}
func sameDeviceIdentity(info os.FileInfo, dev, ino uint64) bool {
	gotDev, gotIno, ok := deviceIdentity(info)
	return ok && gotDev == dev && gotIno == ino
}
func deviceIdentity(info os.FileInfo) (uint64, uint64, bool) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0, false
	}
	return uint64(stat.Dev), uint64(stat.Ino), true
}

// WatchScreen binds broker health to this exact direct child. The caller owns
// when to install the watcher; no process lookup, PID adoption, or Screen
// control channel is involved.
func (r Runtime) WatchScreen(broker *Broker) {
	if r.Screen == nil || broker == nil {
		return
	}
	go func() { broker.ChildLost(r.Screen.Wait(context.Background())) }()
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

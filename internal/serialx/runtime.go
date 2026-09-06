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
	// ScreenEvidence identifies only the Screen child this runtime started.
	// It is observation, not an authority for callers to adopt a process.
	ScreenEvidence               ScreenEvidence
	screen                       *ownedScreen
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

// runtimeDeps is the deterministic in-package test seam. Production callers
// receive no process injection surface; only platform-owned dependencies can
// construct the retained direct Screen child.
type runtimeDeps struct {
	allocatePTY     func() (*os.File, *os.File, error)
	startScreen     func(context.Context, screenLaunch) (*ownedScreen, error)
	qualifiedScreen func(ScreenBinary) bool
	lstat           func(string) (os.FileInfo, error)
	openRoot        func(string) (*os.Root, error)
	openGeneration  func(*os.Root, string) (*os.Root, error)
}

func (systemPTYAllocator) Allocate() (*os.File, *os.File, error) { return allocatePTY() }

func CreateRuntime(ctx context.Context, root Root, generation string, screen ScreenBinary) (Runtime, error) {
	deps, err := productionRuntimeDeps()
	if err != nil {
		return Runtime{}, err
	}
	return createRuntime(ctx, root, generation, screen, deps)
}
func createRuntime(ctx context.Context, root Root, generation string, screen ScreenBinary, deps runtimeDeps) (Runtime, error) {
	if err := ctx.Err(); err != nil {
		return Runtime{}, err
	}
	if deps.qualifiedScreen == nil || !deps.qualifiedScreen(screen) {
		return Runtime{}, fmt.Errorf("screen binary is not qualified")
	}
	if deps.allocatePTY == nil || deps.startScreen == nil {
		return Runtime{}, fmt.Errorf("Screen runtime dependencies are unavailable")
	}
	if !safeRuntimeRootPath(root) {
		return Runtime{}, fmt.Errorf("runtime root must be canonical and non-root")
	}
	if !safeGeneration(generation) {
		return Runtime{}, fmt.Errorf("generation is unsafe")
	}
	before, err := deps.lstatRoot(root)
	if err != nil {
		return Runtime{}, fmt.Errorf("inspect runtime root before open: %w", err)
	}
	if !privateDirectory(before) || before.Mode()&os.ModeSymlink != 0 {
		return Runtime{}, fmt.Errorf("runtime root is unsafe")
	}
	rootHandle, err := deps.openRuntimeRoot(root)
	if err != nil {
		return Runtime{}, fmt.Errorf("open runtime root: %w", err)
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
	created, err := rootHandle.Lstat(generation)
	if err != nil || !privateDirectory(created) {
		rootHandle.Close()
		return Runtime{}, fmt.Errorf("generation entry changed after creation")
	}
	generationRoot, err := deps.openGenerationRoot(rootHandle, generation)
	if err != nil {
		removeGenerationIfExact(rootHandle, generation, created)
		rootHandle.Close()
		return Runtime{}, fmt.Errorf("open generation directory: %w", err)
	}
	directory := filepath.Join(root, generation)
	genInfo, err := generationRoot.Stat(".")
	if err != nil {
		generationRoot.Close()
		removeGenerationIfExact(rootHandle, generation, created)
		rootHandle.Close()
		return Runtime{}, fmt.Errorf("inspect generation directory: %w", err)
	}
	if !sameFileIdentity(created, genInfo) {
		generationRoot.Close()
		rootHandle.Close()
		return Runtime{}, fmt.Errorf("generation changed during open")
	}
	dev, ino, ok := deviceIdentity(genInfo)
	if !ok {
		generationRoot.Close()
		removeGenerationIfExact(rootHandle, generation, created)
		rootHandle.Close()
		return Runtime{}, fmt.Errorf("generation identity unavailable")
	}
	runtime := Runtime{generationDir: directory, root: rootHandle, generationRoot: generationRoot, generationDev: dev, generationIno: ino}
	cleanup := func(err error) (Runtime, error) { _ = runtime.Close(); return Runtime{}, err }
	tartMaster, tartSlave, err := deps.allocatePTY()
	if err != nil {
		return cleanup(fmt.Errorf("allocate Tart PTY: %w", err))
	}
	runtime.TartMaster = tartMaster
	operatorMaster, operatorSlave, err := deps.allocatePTY()
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
	child, err := deps.startScreen(ctx, screenLaunch{path: ScreenPath, args: []string{"-D", "-m", "-S", "boxwarden-" + generation}, stdin: operatorSlave})
	if err != nil {
		return cleanup(fmt.Errorf("start Screen: %w", err))
	}
	if child == nil || !child.Evidence().valid() {
		if child != nil {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), ExchangeDeadline)
			_ = child.Stop(cleanupCtx)
			_ = child.Wait(cleanupCtx)
			cancel()
		}
		return cleanup(fmt.Errorf("start Screen: direct child evidence is invalid"))
	}
	runtime.screen = child
	runtime.ScreenEvidence = child.Evidence()
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
	if r.screen != nil {
		if err := r.screen.Stop(ctx); err != nil && first == nil {
			first = err
		}
		if err := r.screen.Wait(ctx); err != nil && first == nil {
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
		endpointsSafe := true
		for _, endpoint := range []endpointIdentity{r.tartLink, r.operatorLink} {
			if err := r.revalidateEndpoint(endpoint); err != nil {
				if first == nil {
					first = err
				}
				endpointsSafe = false
				continue
			}
			if err := r.generationRoot.Remove(endpoint.name); err != nil && first == nil {
				first = err
			}
		}
		if err := r.generationRoot.Close(); err != nil && first == nil {
			first = err
		}
		// A Screen that was already reaped is a lifecycle error, not evidence
		// that these descriptor-validated filesystem objects became unsafe.
		// Only endpoint validation can prevent exact generation cleanup.
		if endpointsSafe {
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
func (r Runtime) CheckScreen(ctx context.Context) error {
	if r.screen == nil {
		return fmt.Errorf("direct Screen child is unavailable")
	}
	return r.screen.Check(ctx)
}

func (r Runtime) WatchScreen(broker *Broker) {
	if r.screen == nil || broker == nil {
		return
	}
	go func() {
		if err := r.CheckScreen(context.Background()); err != nil {
			broker.ChildLost(err)
			return
		}
		broker.ChildLost(r.screen.Wait(context.Background()))
	}()
}

func (deps runtimeDeps) lstatRoot(path string) (os.FileInfo, error) {
	if deps.lstat != nil {
		return deps.lstat(path)
	}
	return os.Lstat(path)
}

func (deps runtimeDeps) openRuntimeRoot(path string) (*os.Root, error) {
	if deps.openRoot != nil {
		return deps.openRoot(path)
	}
	return os.OpenRoot(path)
}

func (deps runtimeDeps) openGenerationRoot(root *os.Root, generation string) (*os.Root, error) {
	if deps.openGeneration != nil {
		return deps.openGeneration(root, generation)
	}
	return root.OpenRoot(generation)
}

func removeGenerationIfExact(root *os.Root, generation string, expected os.FileInfo) {
	current, err := root.Lstat(generation)
	if err == nil && sameFileIdentity(current, expected) {
		_ = root.Remove(generation)
	}
}

func safeRuntimeRootPath(root string) bool {
	return root != "" && filepath.IsAbs(root) && filepath.Clean(root) == root && root != "/"
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

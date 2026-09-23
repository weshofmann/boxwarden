package basebuild

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/weshofmann/boxwarden/internal/execx"
)

var sha512CryptOutput = regexp.MustCompile(`^\$6\$(?:rounds=[0-9]+\$)?[A-Za-z0-9./]{1,16}\$[A-Za-z0-9./]{86}$`)

// HostSeedBuilder executes only the two tracked guest-definition scripts. The
// SHA-512 crypt producer and xorriso are supplied as exact-digest executables;
// caller configuration must come from an admitted host-toolchain decision.
type HostSeedBuilder struct {
	Runner        execx.Runner
	ScriptRunner  OwnedScriptRunner
	OpenSSLPath   string
	OpenSSLSHA256 string
	XorrisoPath   string
	XorrisoSHA256 string
}

// CheckTools admits both exact host executables and the capabilities the build
// needs before reserving an attempt. Each use still rechecks its executable to
// detect later drift.
func (b HostSeedBuilder) CheckTools() error {
	if b.Runner == nil {
		return errors.New("seed command runner is required")
	}
	if err := exactExecutable(b.OpenSSLPath, "openssl", b.OpenSSLSHA256); err != nil {
		return fmt.Errorf("SHA-512 crypt producer: %w", err)
	}
	if err := exactExecutable(b.XorrisoPath, "xorriso", b.XorrisoSHA256); err != nil {
		return fmt.Errorf("ISO remaster tool: %w", err)
	}
	probeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := b.Runner.Run(probeCtx, execx.Command{Path: b.OpenSSLPath, Args: []string{"passwd", "-6", "-stdin"}, Env: []string{"PATH=/usr/bin:/bin", "LANG=C", "LC_ALL=C"}, Stdin: []byte("boxwarden-preflight-probe\n")})
	if err != nil {
		return fmt.Errorf("SHA-512 crypt producer cannot run required mode: %w", err)
	}
	if result.Truncated || len(result.Stdout) > 256 || !sha512CryptOutput.MatchString(strings.TrimSuffix(result.Stdout, "\n")) {
		return errors.New("SHA-512 crypt producer did not return a valid verifier")
	}
	result, err = b.Runner.Run(probeCtx, execx.Command{Path: b.XorrisoPath, Args: []string{"-version"}, Env: []string{"PATH=/usr/bin:/bin", "LANG=C", "LC_ALL=C"}})
	if err != nil {
		return fmt.Errorf("ISO remaster tool cannot report its version: %w", err)
	}
	if result.Truncated || len(result.Stdout) > 4096 || !strings.Contains(result.Stdout, "xorriso version") {
		return errors.New("ISO remaster tool returned invalid version output")
	}
	return nil
}

func (b HostSeedBuilder) BuilderVerifier(ctx context.Context, attemptDir string) (string, error) {
	if b.Runner == nil {
		return "", errors.New("seed command runner is required")
	}
	if err := privateStateRoot(attemptDir); err != nil {
		return "", fmt.Errorf("private attempt: %w", err)
	}
	if err := exactExecutable(b.OpenSSLPath, "openssl", b.OpenSSLSHA256); err != nil {
		return "", fmt.Errorf("SHA-512 crypt producer: %w", err)
	}
	var random [32]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", fmt.Errorf("create builder-only random password: %w", err)
	}
	secret := make([]byte, hex.EncodedLen(len(random))+1)
	hex.Encode(secret, random[:])
	secret[len(secret)-1] = '\n'
	defer func() {
		for i := range random {
			random[i] = 0
		}
		for i := range secret {
			secret[i] = 0
		}
	}()
	runCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	result, err := b.Runner.Run(runCtx, execx.Command{Path: b.OpenSSLPath, Args: []string{"passwd", "-6", "-stdin"}, Env: []string{"PATH=/usr/bin:/bin", "LANG=C", "LC_ALL=C"}, Stdin: secret})
	if err != nil {
		return "", fmt.Errorf("generate builder verifier: %w", err)
	}
	if result.Truncated {
		return "", errors.New("builder verifier output exceeded limit")
	}
	output := strings.TrimSuffix(result.Stdout, "\n")
	if len(result.Stdout) > 256 || !sha512CryptOutput.MatchString(output) {
		return "", errors.New("builder verifier producer returned invalid SHA-512 crypt output")
	}
	root, err := os.OpenRoot(attemptDir)
	if err != nil {
		return "", err
	}
	defer root.Close()
	const name = "builder-verifier"
	file, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return "", err
	}
	created := true
	defer func() {
		if created {
			_ = root.Remove(name)
		}
	}()
	_, writeErr := file.Write([]byte(output + "\n"))
	syncErr := file.Sync()
	closeErr := file.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		return "", err
	}
	if err := syncPreparedRoot(root); err != nil {
		return "", err
	}
	created = false
	return filepath.Join(attemptDir, name), nil
}

func (b HostSeedBuilder) Render(ctx context.Context, request RenderRequest) error {
	if b.ScriptRunner == nil {
		return errors.New("owned script runner is required")
	}
	command, err := RenderCommand(request)
	if err != nil {
		return err
	}
	if err := privateStateRoot(filepath.Dir(request.OutputDirectory)); err != nil {
		return fmt.Errorf("render destination parent: %w", err)
	}
	runCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	result, err := b.ScriptRunner.RunOwned(runCtx, execx.Command{Path: command.Path, Args: command.Args,
		Env: []string{"PATH=/usr/bin:/bin", "HOME=" + filepath.Dir(request.OutputDirectory), "TMPDIR=" + filepath.Dir(request.OutputDirectory), "LANG=C", "LC_ALL=C"}})
	if err != nil {
		return fmt.Errorf("render generic installer seed: %w", err)
	}
	if result.Truncated {
		return errors.New("generic seed renderer output exceeded limit")
	}
	return nil
}

func (b HostSeedBuilder) Remaster(ctx context.Context, request RemasterRequest) (retErr error) {
	if b.ScriptRunner == nil {
		return errors.New("owned script runner is required")
	}
	command, err := RemasterCommand(request)
	if err != nil {
		return err
	}
	if err := privateStateRoot(filepath.Dir(request.OutputISO)); err != nil {
		return fmt.Errorf("remaster destination parent: %w", err)
	}
	if err := exactExecutable(b.XorrisoPath, "xorriso", b.XorrisoSHA256); err != nil {
		return fmt.Errorf("ISO remaster tool: %w", err)
	}
	privateDir := filepath.Dir(request.OutputISO)
	toolDir := filepath.Join(privateDir, "tool-bin")
	if err := os.Mkdir(toolDir, 0700); err != nil {
		return fmt.Errorf("create isolated remaster PATH: %w", err)
	}
	toolLink := filepath.Join(toolDir, "xorriso")
	if err := os.Symlink(b.XorrisoPath, toolLink); err != nil {
		return errors.Join(err, os.Remove(toolDir))
	}
	defer func() {
		if errors.Is(retErr, ErrScriptReapUnproven) {
			return // Keep the exact tool binding while child ownership is uncertain.
		}
		target, err := os.Readlink(toolLink)
		if err != nil || target != b.XorrisoPath {
			retErr = errors.Join(retErr, errors.New("isolated remaster tool binding changed"))
			return
		}
		retErr = errors.Join(retErr, os.Remove(toolLink), os.Remove(toolDir))
	}()
	runCtx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	result, err := b.ScriptRunner.RunOwned(runCtx, execx.Command{Path: command.Path, Args: command.Args,
		Env: []string{"PATH=" + toolDir + ":/usr/bin:/bin", "HOME=" + privateDir, "TMPDIR=" + privateDir, "LANG=C", "LC_ALL=C"}})
	if err != nil {
		return fmt.Errorf("remaster generic installer ISO: %w", err)
	}
	if result.Truncated {
		return errors.New("generic ISO remaster output exceeded limit")
	}
	return nil
}

func exactExecutable(path, basename, expected string) error {
	if !absoluteClean(path) || filepath.Base(path) != basename || !lowerHexDigest(expected) {
		return errors.New("qualified executable path or digest is invalid")
	}
	entry, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !entry.Mode().IsRegular() || entry.Mode().Perm()&0111 == 0 || entry.Size() <= 0 || entry.Size() > 256<<20 {
		return errors.New("qualified executable has unsafe type, mode, or size")
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(entry, opened) {
		return errors.New("qualified executable changed while opening")
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return err
	}
	if fmt.Sprintf("%x", hash.Sum(nil)) != expected {
		return errors.New("qualified executable digest drifted")
	}
	return nil
}

var _ SeedBuilder = HostSeedBuilder{}

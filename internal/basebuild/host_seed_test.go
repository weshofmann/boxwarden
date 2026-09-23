package basebuild

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/weshofmann/boxwarden/internal/execx"
)

type seedCommandRunner struct {
	commands          []execx.Command
	result            execx.Result
	stdinBytes        int
	stdinNewline      bool
	secretInArgv      bool
	xorrisoLinkTarget string
	remasterPATH      string
}

func (r *seedCommandRunner) Run(_ context.Context, command execx.Command) (execx.Result, error) {
	if len(command.Stdin) != 0 {
		r.stdinBytes = len(command.Stdin)
		r.stdinNewline = command.Stdin[len(command.Stdin)-1] == '\n'
		r.secretInArgv = strings.Contains(strings.Join(command.Args, ""), string(command.Stdin[:len(command.Stdin)-1]))
	}
	command.Stdin = nil
	if strings.HasSuffix(command.Path, "remaster-golden-iso.sh") {
		for _, item := range command.Env {
			if strings.HasPrefix(item, "PATH=") {
				r.remasterPATH = strings.TrimPrefix(item, "PATH=")
				link := filepath.Join(strings.SplitN(r.remasterPATH, ":", 2)[0], "xorriso")
				r.xorrisoLinkTarget, _ = os.Readlink(link)
			}
		}
	}
	r.commands = append(r.commands, command)
	return r.result, nil
}

func (r *seedCommandRunner) RunOwned(ctx context.Context, command execx.Command) (execx.Result, error) {
	return r.Run(ctx, command)
}

func seedTool(t *testing.T, dir, name string) (string, string) {
	t.Helper()
	path := filepath.Join(dir, name)
	data := []byte("fake executable " + name)
	if err := os.WriteFile(path, data, 0700); err != nil {
		t.Fatal(err)
	}
	return path, fmt.Sprintf("%x", sha256.Sum256(data))
}

func TestHostSeedBuilderUsesDirectPinnedCommandsAndPrivateVerifier(t *testing.T) {
	usePortableACLFixture(t)
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	openssl, opensslHash := seedTool(t, root, "openssl")
	xorriso, xorrisoHash := seedTool(t, root, "xorriso")
	runner := &seedCommandRunner{result: execx.Result{Stdout: "$6$abcdefghijklmnop$" + strings.Repeat("A", 86) + "\n"}}
	builder := HostSeedBuilder{Runner: runner, ScriptRunner: runner, OpenSSLPath: openssl, OpenSSLSHA256: opensslHash, XorrisoPath: xorriso, XorrisoSHA256: xorrisoHash}
	attempt := filepath.Join(root, "attempt")
	if err := os.Mkdir(attempt, 0700); err != nil {
		t.Fatal(err)
	}
	verifier, err := builder.BuilderVerifier(context.Background(), attempt)
	if err != nil {
		t.Fatal(err)
	}
	if verifier != filepath.Join(attempt, "builder-verifier") {
		t.Fatalf("verifier path = %q", verifier)
	}
	info, err := os.Lstat(verifier)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("verifier mode: %v, %v", info, err)
	}
	if len(runner.commands) != 1 {
		t.Fatalf("commands: %d", len(runner.commands))
	}
	command := runner.commands[0]
	if command.Path != openssl || strings.Join(command.Args, "|") != "passwd|-6|-stdin" || runner.stdinBytes < 64 || !runner.stdinNewline {
		t.Fatalf("openssl command shape: path=%q argv=%q stdin-bytes=%d newline=%t", command.Path, command.Args, runner.stdinBytes, runner.stdinNewline)
	}
	if runner.secretInArgv {
		t.Fatal("secret placed in argv")
	}

	definition := filepath.Join(root, "guest")
	if err := os.Mkdir(definition, 0700); err != nil {
		t.Fatal(err)
	}
	runner.result = execx.Result{}
	if err := builder.Render(context.Background(), RenderRequest{GuestDefinitionRoot: definition, RunID: "run-1", VerifierFile: verifier, OutputDirectory: filepath.Join(attempt, "seed")}); err != nil {
		t.Fatal(err)
	}
	if err := builder.Remaster(context.Background(), RemasterRequest{GuestDefinitionRoot: definition, SourceISO: "/private/ubuntu.iso", RenderedUserData: filepath.Join(attempt, "seed", "user-data"), PreparationJSON: filepath.Join(attempt, "recipe-prepare.json"), OutputISO: filepath.Join(attempt, "installer.iso")}); err != nil {
		t.Fatal(err)
	}
	if len(runner.commands) != 3 {
		t.Fatalf("commands: %d", len(runner.commands))
	}
	if got := runner.commands[1]; got.Path != filepath.Join(definition, "render-golden-seed.sh") || strings.Join(got.Args, "|") != "run-1|"+verifier+"|"+filepath.Join(attempt, "seed") {
		t.Fatalf("render command: %+v", got)
	}
	if got := runner.commands[2]; got.Path != filepath.Join(definition, "remaster-golden-iso.sh") || strings.Join(got.Args, "|") != "/private/ubuntu.iso|"+filepath.Join(attempt, "seed", "user-data")+"|"+filepath.Join(attempt, "recipe-prepare.json")+"|"+filepath.Join(attempt, "installer.iso") || !strings.Contains(strings.Join(got.Env, "|"), "TMPDIR="+attempt) {
		t.Fatalf("remaster command: %+v", got)
	}
	if runner.remasterPATH != filepath.Join(attempt, "tool-bin")+":/usr/bin:/bin" || runner.xorrisoLinkTarget != xorriso {
		t.Fatalf("remaster PATH or xorriso binding: path=%q target=%q", runner.remasterPATH, runner.xorrisoLinkTarget)
	}
	if _, err := os.Stat(filepath.Join(attempt, "tool-bin")); !os.IsNotExist(err) {
		t.Fatalf("temporary tool bin retained: %v", err)
	}
}

func TestOwnedScriptCancellationKillsActiveChild(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "child-survived")
	ready := filepath.Join(dir, "child-started")
	script := filepath.Join(dir, "parent.sh")
	// The child would write after cancellation if only the shell were killed.
	if err := os.WriteFile(script, []byte("#!/bin/sh\n(sleep 1; echo survived > \"$1\") &\necho ready > \"$2\"\nwait\n"), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan error, 1)
	go func() {
		_, err := (OSOwnedScriptRunner{}).RunOwned(ctx, execx.Command{Path: script, Args: []string{marker, ready}, Env: []string{"PATH=/usr/bin:/bin"}})
		finished <- err
	}()
	deadline := time.After(3 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		select {
		case <-deadline:
			t.Fatal("child never reached active state")
		case <-time.After(10 * time.Millisecond):
		}
	}
	started := time.Now()
	cancel()
	select {
	case err := <-finished:
		if err == nil {
			t.Fatal("cancelled script succeeded")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancelled process group did not reap within bound")
	}
	if time.Since(started) > 3*time.Second {
		t.Fatal("cancelled process group did not reap within bound")
	}
	time.Sleep(1200 * time.Millisecond)
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("child survived cancellation: %v", err)
	}
}

func TestOwnedScriptDoesNotSignalAfterSerializedReap(t *testing.T) {
	process, err := os.FindProcess(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	continuePoll := make(chan struct{})
	signaled := make(chan struct{}, 1)
	owned := &ownedScriptProcess{
		process: process,
		done:    make(chan struct{}),
		poll: func(child int) (int, syscall.WaitStatus, error) {
			close(entered)
			<-continuePoll
			return child, 0, nil
		},
		signal:  func(int, syscall.Signal) error { signaled <- struct{}{}; return nil },
		release: func() error { return nil },
	}
	go owned.reap()
	<-entered
	stopped := make(chan error, 1)
	go func() { stopped <- owned.stop() }()
	close(continuePoll)
	if err := <-stopped; err != nil {
		t.Fatal(err)
	}
	<-owned.done
	select {
	case <-signaled:
		t.Fatal("signaled a reaped process group")
	default:
	}
}

func TestHostSeedBuilderRejectsToolDigestDriftBeforeExecution(t *testing.T) {
	usePortableACLFixture(t)
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	openssl, _ := seedTool(t, root, "openssl")
	runner := &seedCommandRunner{}
	builder := HostSeedBuilder{Runner: runner, OpenSSLPath: openssl, OpenSSLSHA256: strings.Repeat("0", 64)}
	if _, err := builder.BuilderVerifier(context.Background(), root); err == nil {
		t.Fatal("digest drift accepted")
	}
	if len(runner.commands) != 0 {
		t.Fatal("drifted executable ran")
	}
	if _, err := os.Stat(filepath.Join(root, "builder-verifier")); !os.IsNotExist(err) {
		t.Fatalf("verifier created: %v", err)
	}
}

func TestHostSeedBuilderChecksBothExecutablesBeforeAttempt(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	openssl, opensslHash := seedTool(t, root, "openssl")
	xorriso, xorrisoHash := seedTool(t, root, "xorriso")
	builder := HostSeedBuilder{OpenSSLPath: openssl, OpenSSLSHA256: opensslHash, XorrisoPath: xorriso, XorrisoSHA256: xorrisoHash}
	if err := builder.CheckTools(); err != nil {
		t.Fatalf("qualified tools rejected: %v", err)
	}
	if err := os.WriteFile(xorriso, []byte("drifted"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := builder.CheckTools(); err == nil {
		t.Fatal("drifted ISO remaster executable admitted before attempt")
	}
}

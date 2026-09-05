package serialx

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/weshofmann/boxwarden/internal/guestproto"
	"github.com/weshofmann/boxwarden/internal/hostx"
)

const serialTestKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

func TestCreateRuntimeRejectsExistingOrUnsafeGenerationPath(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, generation := range map[string]string{"existing": "generation", "traversal": "../outside", "empty": ""} {
		t.Run(name, func(t *testing.T) {
			if name == "existing" {
				if err := os.Mkdir(filepath.Join(root, generation), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			starter := &screenStarterFake{}
			if _, err := createRuntime(context.Background(), root, generation, qualifiedScreenFact(), starter, &ptyAllocatorFake{}); err == nil {
				t.Fatal("CreateRuntime() error = nil, want safe-path refusal")
			}
			if starter.called {
				t.Fatal("CreateRuntime() started Screen after unsafe path admission")
			}
		})
	}
	target := t.TempDir()
	if err := os.Chmod(target, 0o700); err != nil {
		t.Fatal(err)
	}
	unsafeRoot := filepath.Join(t.TempDir(), "runtime-link")
	if err := os.Symlink(target, unsafeRoot); err != nil {
		t.Fatal(err)
	}
	if _, err := createRuntime(context.Background(), unsafeRoot, "generation", qualifiedScreenFact(), &screenStarterFake{}, &ptyAllocatorFake{}); err == nil {
		t.Fatal("CreateRuntime() accepted symlinked runtime root")
	}
}

func TestCreateRuntimeUsesTwoOwnerOnlyPTYSlavesAndFixedScreenSpec(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	allocator := &ptyAllocatorFake{}
	starter := &screenStarterFake{}
	runtime, err := createRuntime(context.Background(), root, "generation-1", qualifiedScreenFact(), starter, allocator)
	if err != nil {
		t.Fatalf("CreateRuntime() error = %v", err)
	}
	defer runtime.Close()
	if allocator.calls != 2 {
		t.Fatalf("PTY allocations = %d, want two", allocator.calls)
	}
	if got := starter.spec; got.Path != ScreenPath || !sameStrings(got.Args, []string{"-D", "-m", "-S", "boxwarden-generation-1"}) || got.Stdin == nil || got.Stdin.Name() != allocator.slaves[1] {
		t.Fatalf("Screen spec = %#v, want exact opened operator slave %q", got, allocator.slaves[1])
	}
	for _, link := range []string{runtime.TartSlave, runtime.OperatorSlave} {
		linkInfo, err := os.Lstat(link)
		if err != nil {
			t.Fatalf("lstat endpoint %q: %v", link, err)
		}
		if linkInfo.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("endpoint %q is not the exact private generation link", link)
		}
		target, err := os.Readlink(link)
		if err != nil {
			t.Fatalf("readlink endpoint %q: %v", link, err)
		}
		if target != allocator.slaves[len(allocator.slaves)-2] && target != allocator.slaves[len(allocator.slaves)-1] {
			t.Fatalf("endpoint %q target = %q, want one allocated slave %#v", link, target, allocator.slaves)
		}
		info, err := os.Stat(link)
		if err != nil {
			t.Fatalf("stat endpoint %q: %v", link, err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("endpoint %q mode = %04o, want 0600", link, info.Mode().Perm())
		}
		if filepath.Dir(link) != filepath.Join(root, "generation-1") {
			t.Fatalf("endpoint %q escapes generation directory", link)
		}
	}
	if err := runtime.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if _, err := os.Lstat(filepath.Join(root, "generation-1")); !os.IsNotExist(err) {
		t.Fatalf("Close() left generation state: %v", err)
	}
}

func TestCreateRuntimeRollsBackOnlyItsPartialGeneration(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	starter := &screenStarterFake{err: errors.New("screen refused")}
	if _, err := createRuntime(context.Background(), root, "generation-2", qualifiedScreenFact(), starter, &ptyAllocatorFake{}); err == nil {
		t.Fatal("CreateRuntime() error = nil, want child-start failure")
	}
	if _, err := os.Lstat(filepath.Join(root, "generation-2")); !os.IsNotExist(err) {
		t.Fatalf("partial generation remained after rollback: %v", err)
	}
}

func TestCreateRuntimeRejectsInvalidDirectScreenEvidence(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	child := &invalidScreenChild{}
	starter := &screenStarterFake{child: child}
	if _, err := createRuntime(context.Background(), root, "generation-invalid", qualifiedScreenFact(), starter, &ptyAllocatorFake{}); err == nil {
		t.Fatal("CreateRuntime() accepted invalid Screen child evidence")
	}
	if _, err := os.Lstat(filepath.Join(root, "generation-invalid")); !os.IsNotExist(err) {
		t.Fatalf("invalid child cleanup left generation: %v", err)
	}
	if !child.stopped || !child.waited {
		t.Fatalf("invalid child cleanup = %#v, want stop and reap", child)
	}
}

func TestRuntimeShutdownRefusesTamperedEndpointReplacement(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	runtime, err := createRuntime(context.Background(), root, "generation-tamper", qualifiedScreenFact(), &screenStarterFake{}, &ptyAllocatorFake{})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(runtime.TartSlave); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/dev/null", runtime.TartSlave); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Shutdown(context.Background()); err == nil {
		t.Fatal("Shutdown() accepted tampered endpoint")
	}
	if _, err := os.Lstat(runtime.TartSlave); err != nil {
		t.Fatalf("Shutdown() removed tampered replacement: %v", err)
	}
}

func TestBrokerDiscardsOperatorInputOutsideConsole(t *testing.T) {
	var tart bytes.Buffer
	broker := NewBroker(BrokerConfig{Tart: writeCloser{&tart}, Generation: testRequest().StartGeneration})
	broker.OperatorInput([]byte("danger"))
	if got, want := broker.InputDiscarded(), uint64(len("danger")); got != want {
		t.Fatalf("InputDiscarded() = %d, want %d", got, want)
	}
	if got := tart.String(); got != "" {
		t.Fatalf("Tart input = %q, want no forwarded input", got)
	}
}

func TestExchangeAcceptsOnlyOneCanonicalAssociatedFrame(t *testing.T) {
	request := testRequest()
	result := testResult(request)
	_, end, err := guestproto.EncodeSerialFrame(request, result)
	if err != nil {
		t.Fatal(err)
	}
	tart := &recordingWriter{}
	broker := NewBroker(BrokerConfig{Tart: tart, Screen: discardCloser{}, Generation: request.StartGeneration})
	tart.after = func() {
		broker.OperatorInput([]byte("operator-must-not-replay"))
		_ = broker.TartOutput([]byte("banner\r\nBOXWARDEN-BEGIN " + request.Nonce + " " + request.SessionID + "\r\n" + end + "\r\n"))
	}
	got, err := broker.Exchange(context.Background(), ExchangeRequest{Request: request})
	if err != nil {
		broker.mu.Lock()
		cause := broker.poisonCause
		broker.mu.Unlock()
		t.Fatalf("Exchange() error = %v (poison cause: %v)", err, cause)
	}
	var decoded guestproto.SerialResult
	if err := json.Unmarshal(got, &decoded); err != nil {
		t.Fatalf("Exchange() result is not canonical JSON: %v", err)
	}
	if !reflect.DeepEqual(decoded, result) {
		t.Fatalf("Exchange() = %#v, want %#v", decoded, result)
	}
	if got, want := broker.InputDiscarded(), uint64(len("operator-must-not-replay")); got != want {
		t.Fatalf("automation discarded input = %d, want %d", got, want)
	}
	if got, want := tart.values(), []string{bootstrapCommand, mustJSON(t, request) + "\n"}; !sameStrings(got, want) {
		t.Fatalf("Tart writes = %#v, want %#v", got, want)
	}
}

func TestExchangePoisonsDuplicateOrMismatchedFrames(t *testing.T) {
	request := testRequest()
	for name, frame := range map[string]string{
		"mismatched begin": "BOXWARDEN-BEGIN other " + request.SessionID + "\n",
		"mismatched end":   "BOXWARDEN-BEGIN " + request.Nonce + " " + request.SessionID + "\nBOXWARDEN-END other " + request.SessionID + " ignored\n",
		"duplicate begin":  "BOXWARDEN-BEGIN " + request.Nonce + " " + request.SessionID + "\nBOXWARDEN-BEGIN " + request.Nonce + " " + request.SessionID + "\n",
	} {
		t.Run(name, func(t *testing.T) {
			tart := &recordingWriter{}
			broker := NewBroker(BrokerConfig{Tart: tart, Screen: discardCloser{}, Generation: request.StartGeneration})
			tart.after = func() { _ = broker.TartOutput([]byte(frame)) }
			if _, err := broker.Exchange(context.Background(), ExchangeRequest{Request: request}); !errors.Is(err, ErrPoisoned) {
				t.Fatalf("Exchange() error = %v, want ErrPoisoned", err)
			}
			if !broker.Poisoned() {
				t.Fatal("broker did not poison ambiguous frame sequence")
			}
		})
	}
}

func TestLateEndAfterExchangeResultPoisonsIdleBroker(t *testing.T) {
	request, result := testRequest(), testResult(testRequest())
	_, end, err := guestproto.EncodeSerialFrame(request, result)
	if err != nil {
		t.Fatal(err)
	}
	tart := &recordingWriter{}
	broker := NewBroker(BrokerConfig{Tart: tart, Screen: discardCloser{}, Generation: request.StartGeneration})
	tart.after = func() {
		_ = broker.TartOutput([]byte("BOXWARDEN-BEGIN " + request.Nonce + " " + request.SessionID + "\n" + end + "\n"))
	}
	if _, err := broker.Exchange(context.Background(), ExchangeRequest{Request: request}); err != nil {
		t.Fatalf("Exchange() error = %v", err)
	}
	if err := broker.TartOutput([]byte(end + "\n")); !errors.Is(err, ErrPoisoned) {
		t.Fatalf("late TartOutput() error = %v, want ErrPoisoned", err)
	}
	if !broker.Poisoned() {
		t.Fatal("late end did not poison idle broker")
	}
}

func TestScreenChildLossPoisonsGeneration(t *testing.T) {
	broker := NewBroker(BrokerConfig{})
	broker.ChildLost(errors.New("unexpected exit"))
	if !broker.Poisoned() || broker.State() != StateFailed {
		t.Fatalf("child loss state = %q, want failed", broker.State())
	}
}

func TestRuntimeWatchScreenPoisonsOnlyOnItsDirectChildExit(t *testing.T) {
	child := &waitScreenChild{result: make(chan error, 1)}
	broker := NewBroker(BrokerConfig{})
	Runtime{Screen: child}.WatchScreen(broker)
	child.result <- errors.New("screen exited")
	deadline := time.After(time.Second)
	for !broker.Poisoned() {
		select {
		case <-deadline:
			t.Fatal("direct Screen exit did not poison broker")
		default:
			time.Sleep(time.Millisecond)
		}
	}
}

func TestRuntimeShutdownStopsAndReapsOnlyItsDirectScreenChild(t *testing.T) {
	child := &trackedScreenChild{evidence: testScreenEvidence()}
	runtime := Runtime{Screen: child}
	if err := runtime.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	if !child.stopped || !child.waited {
		t.Fatalf("Shutdown() child state = %#v, want direct stop and reap", child)
	}
}

func TestBlockedScreenWriterCannotWedgeTartReaderAndIsBounded(t *testing.T) {
	screen := &blockingWriter{started: make(chan struct{}), release: make(chan struct{})}
	broker := NewBroker(BrokerConfig{Screen: screen})
	if err := broker.TartOutput([]byte("first")); err != nil {
		t.Fatalf("first TartOutput() error = %v", err)
	}
	select {
	case <-screen.started:
	case <-time.After(time.Second):
		t.Fatal("Screen drain did not begin")
	}
	done := make(chan error, 1)
	go func() { done <- broker.TartOutput(bytes.Repeat([]byte("x"), MaxScreenQueueBytes)) }()
	select {
	case err := <-done:
		if !errors.Is(err, ErrPoisoned) {
			t.Fatalf("bounded TartOutput() error = %v, want ErrPoisoned", err)
		}
	case <-time.After(time.Second):
		t.Fatal("blocked Screen writer wedged TartOutput")
	}
	if err := broker.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestBrokerCloseUnblocksBlockedConsoleWriteWithoutLeaseReplay(t *testing.T) {
	tart := &blockingWriter{started: make(chan struct{}), release: make(chan struct{})}
	broker := NewBroker(BrokerConfig{Tart: tart, Screen: discardCloser{}})
	lease, err := broker.AcquireConsole(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { broker.OperatorInput([]byte("console")); close(done) }()
	<-tart.started
	if err := broker.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Close() did not unblock console writer")
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestScreenDrainRejectsConcurrentWriteCountBeyondExactSnapshot(t *testing.T) {
	screen := &snapshotCountWriter{started: make(chan struct{}), release: make(chan struct{})}
	broker := NewBroker(BrokerConfig{Screen: screen})
	if err := broker.TartOutput([]byte("first")); err != nil {
		t.Fatal(err)
	}
	<-screen.started
	if err := broker.TartOutput([]byte("second")); err != nil {
		t.Fatal(err)
	}
	close(screen.release)
	deadline := time.After(time.Second)
	for !broker.Poisoned() {
		select {
		case <-deadline:
			t.Fatal("oversized stale write count did not poison")
		default:
			time.Sleep(time.Millisecond)
		}
	}
}

func TestTartOutputPoisonsWithoutScreenWriter(t *testing.T) {
	broker := NewBroker(BrokerConfig{})
	if err := broker.TartOutput([]byte("guest output")); !errors.Is(err, ErrPoisoned) {
		t.Fatalf("TartOutput() error = %v, want ErrPoisoned", err)
	}
	if !broker.Poisoned() {
		t.Fatal("missing Screen writer did not poison broker")
	}
}

func TestExchangePoisonsOnOverflowInterleavingAndTimeout(t *testing.T) {
	request := testRequest()
	for name, respond := range map[string]func(*Broker){
		"overflow": func(b *Broker) { _ = b.TartOutput(bytes.Repeat([]byte("x"), MaxScreenQueueBytes+1)) },
		"interleaving": func(b *Broker) {
			_ = b.TartOutput([]byte("BOXWARDEN-BEGIN " + request.Nonce + " " + request.SessionID + "\nnoise\n"))
		},
		"timeout": func(b *Broker) {},
	} {
		t.Run(name, func(t *testing.T) {
			clock := newClockFake()
			tart := &recordingWriter{}
			broker := NewBroker(BrokerConfig{Tart: tart, Screen: discardCloser{}, Generation: request.StartGeneration, Clock: clock})
			tart.after = func() {
				respond(broker)
				if name == "timeout" {
					clock.fire()
				}
			}
			if _, err := broker.Exchange(context.Background(), ExchangeRequest{Request: request}); !errors.Is(err, ErrPoisoned) {
				t.Fatalf("Exchange() error = %v, want ErrPoisoned", err)
			}
			if !broker.Poisoned() {
				t.Fatal("broker did not poison on unsafe exchange")
			}
		})
	}
}

func TestConsoleEOFDoesNotCloseScreenOrTartEndpoint(t *testing.T) {
	var tart bytes.Buffer
	broker := NewBroker(BrokerConfig{Tart: writeCloser{&tart}})
	lease, err := broker.AcquireConsole(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	broker.ConsoleEOF()
	if broker.State() != StateIdle {
		t.Fatalf("State() = %q, want idle", broker.State())
	}
	broker.OperatorInput([]byte("discard"))
	if tart.String() != "" {
		t.Fatalf("Tart received post-EOF operator input %q", tart.String())
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
}

func testRequest() guestproto.SerialRequest {
	raw, _ := base64.StdEncoding.DecodeString(strings.Fields(serialTestKey)[1])
	sum := sha256.Sum256(raw)
	return guestproto.SerialRequest{Version: guestproto.Version, Nonce: "nonce-1", StartGeneration: "9b2d12d8-7014-4c5e-9d5c-627c2fcc1575", Association: guestproto.Association{Domain: "work", SessionID: "123e4567-e89b-42d3-a456-426614174000", BackendKind: "tart", BackendObject: "workstation"}, CAPublicKey: serialTestKey, CAFingerprint: "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:]), Principal: "boxwarden-session-123e4567-e89b-42d3-a456-426614174000"}
}

func testResult(request guestproto.SerialRequest) guestproto.SerialResult {
	return guestproto.SerialResult{Version: guestproto.Version, StartGeneration: request.StartGeneration, Association: request.Association, CAFingerprint: request.CAFingerprint, Principal: request.Principal, HostPublicKey: serialTestKey, InstalledSHA256: map[string]string{"trusted-user-ca.pub": strings.Repeat("a", 64), "authorized_principals/boxwarden": strings.Repeat("b", 64), "management-binding.json": strings.Repeat("c", 64)}, SSHD: map[string]string{"trustedusercakeys": "/etc/ssh/boxwarden/active/trusted-user-ca.pub", "authorizedprincipalsfile": "/etc/ssh/boxwarden/active/authorized_principals/%u", "authorizedkeysfile": "none", "permituserenvironment": "no", "permituserrc": "no", "passwordauthentication": "no", "kbdinteractiveauthentication": "no", "permitrootlogin": "no", "allowagentforwarding": "no", "x11forwarding": "no", "allowtcpforwarding": "no", "allowstreamlocalforwarding": "no", "gatewayports": "no", "permittunnel": "no"}}
}

type recordingWriter struct {
	mu     sync.Mutex
	writes []string
	after  func()
}

func (w *recordingWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	w.writes = append(w.writes, string(data))
	n := len(w.writes)
	after := w.after
	w.mu.Unlock()
	if n == 2 && after != nil {
		after()
	}
	return len(data), nil
}
func (w *recordingWriter) values() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]string(nil), w.writes...)
}
func (*recordingWriter) Close() error { return nil }

type clockFake struct{ ch chan time.Time }

func newClockFake() *clockFake                            { return &clockFake{ch: make(chan time.Time, 1)} }
func (c *clockFake) After(time.Duration) <-chan time.Time { return c.ch }
func (c *clockFake) fire()                                { c.ch <- time.Now() }

type screenStarterFake struct {
	called bool
	spec   ScreenSpec
	child  ScreenChild
	err    error
}

func (s *screenStarterFake) StartScreen(_ context.Context, spec ScreenSpec) (ScreenChild, error) {
	s.called = true
	s.spec = spec
	if s.err != nil {
		return nil, s.err
	}
	if s.child == nil {
		s.child = screenChildFake{}
	}
	return s.child, nil
}

type screenChildFake struct{}

func (screenChildFake) Stop(context.Context) error { return nil }
func (screenChildFake) Wait(context.Context) error { return nil }
func (screenChildFake) Evidence() ScreenEvidence   { return testScreenEvidence() }

type invalidScreenChild struct{ stopped, waited bool }

func (c *invalidScreenChild) Stop(context.Context) error { c.stopped = true; return nil }
func (c *invalidScreenChild) Wait(context.Context) error { c.waited = true; return nil }
func (c *invalidScreenChild) Evidence() ScreenEvidence   { return ScreenEvidence{} }

type waitScreenChild struct{ result chan error }

func (c *waitScreenChild) Stop(context.Context) error { return nil }
func (c *waitScreenChild) Wait(context.Context) error { return <-c.result }
func (c *waitScreenChild) Evidence() ScreenEvidence   { return testScreenEvidence() }

type trackedScreenChild struct {
	evidence        ScreenEvidence
	stopped, waited bool
}

func (c *trackedScreenChild) Stop(context.Context) error { c.stopped = true; return nil }
func (c *trackedScreenChild) Wait(context.Context) error { c.waited = true; return nil }
func (c *trackedScreenChild) Evidence() ScreenEvidence   { return c.evidence }

type ptyAllocatorFake struct {
	calls  int
	slaves []string
}

func (p *ptyAllocatorFake) Allocate() (*os.File, *os.File, error) {
	p.calls++
	master, err := os.CreateTemp("", "serial-master-")
	if err != nil {
		return nil, nil, err
	}
	slave, err := os.CreateTemp("", "serial-slave-")
	if err != nil {
		master.Close()
		return nil, nil, err
	}
	if err := slave.Chmod(0o600); err != nil {
		master.Close()
		slave.Close()
		return nil, nil, err
	}
	p.slaves = append(p.slaves, slave.Name())
	return master, slave, nil
}

type blockingWriter struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (w *blockingWriter) Write(data []byte) (int, error) {
	w.once.Do(func() { close(w.started) })
	<-w.release
	return len(data), nil
}
func (w *blockingWriter) Close() error {
	w.once.Do(func() { close(w.started) })
	select {
	case <-w.release:
	default:
		close(w.release)
	}
	return nil
}

type snapshotCountWriter struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (w *snapshotCountWriter) Write(data []byte) (int, error) {
	w.once.Do(func() { close(w.started) })
	<-w.release
	return len(data) + 1, nil
}
func (w *snapshotCountWriter) Close() error {
	select {
	case <-w.release:
	default:
		close(w.release)
	}
	return nil
}

type writeCloser struct{ io.Writer }

func (writeCloser) Close() error { return nil }

type discardCloser struct{}

func (discardCloser) Write(data []byte) (int, error) { return len(data), nil }
func (discardCloser) Close() error                   { return nil }
func qualifiedScreenFact() ScreenBinary {
	fact, err := hostx.AdmitScreen(hostx.PathFact{Exists: true, Regular: true, Mode: 0o755, UID: 0, GID: 0, Links: 1, SHA256: ScreenSHA256}, ScreenVersion)
	if err != nil {
		panic(err)
	}
	return fact
}
func mustJSON(t *testing.T, value any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
func testScreenEvidence() ScreenEvidence {
	return ScreenEvidence{pid: 1, started: time.Unix(1, 0), token: [16]byte{1}}
}

var _ io.Writer = (*recordingWriter)(nil)

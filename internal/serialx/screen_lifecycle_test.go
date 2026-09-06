package serialx

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// A regression here would allow a caller-controlled launch shape or use an
// arbitrary operator endpoint instead of the already-open private slave.
func TestCreateRuntimePassesFixedScreenLaunchToPrivateStarter(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	allocator := &ptyAllocatorFake{}
	var got screenLaunch
	runtime, err := createRuntime(context.Background(), root, "generation-launch", qualifiedScreenFact(), runtimeDeps{
		allocatePTY:     allocator.Allocate,
		qualifiedScreen: func(ScreenBinary) bool { return true },
		startScreen: func(_ context.Context, launch screenLaunch) (*ownedScreen, error) {
			got = launch
			return newTestOwnedScreen(screenIdentity{pid: 81, started: time.Unix(81, 0), unique: 81}), nil
		},
	})
	if err != nil {
		t.Fatalf("createRuntime() error = %v", err)
	}
	defer runtime.Close()
	if got.path != ScreenPath || !sameStrings(got.args, []string{"-D", "-m", "-S", "boxwarden-generation-launch"}) {
		t.Fatalf("screen launch = %#v, want fixed Screen argv", got)
	}
	if got.stdin == nil || got.stdin.Name() != allocator.slaves[1] {
		t.Fatalf("screen stdin = %v, want already-open operator slave %q", got.stdin, allocator.slaves[1])
	}
}

// A regression here would allow kernel identity collection before a child
// exists, turning a PID lookup into adoption authority.
func TestStartOwnedScreenStartsBeforeObservingExactChild(t *testing.T) {
	events := make([]string, 0, 2)
	child := &screenCommandFake{pid: 82}
	owned, err := startOwnedScreen(context.Background(), screenLaunch{path: ScreenPath}, screenStartDeps{
		start: func(context.Context, screenLaunch) (startedScreen, error) {
			events = append(events, "start")
			return startedScreen{direct: child}, nil
		},
		observe: func(_ context.Context, pid int) (screenIdentity, error) {
			events = append(events, "observe")
			if pid != 82 {
				t.Fatalf("observed PID = %d, want exact child 82", pid)
			}
			return screenIdentity{pid: 82, started: time.Unix(82, 0), unique: 82}, nil
		},
	})
	if err != nil {
		t.Fatalf("startOwnedScreen() error = %v", err)
	}
	if !sameStrings(events, []string{"start", "observe"}) || owned.Evidence().PID() != 82 {
		t.Fatalf("events/evidence = %#v/%#v, want start then exact observation", events, owned.Evidence())
	}
}

// A regression here would leave a successfully spawned but untrusted child
// behind when kernel inspection fails.
func TestStartOwnedScreenReapsChildWhenObservationFails(t *testing.T) {
	child := &screenCommandFake{pid: 83}
	_, err := startOwnedScreen(context.Background(), screenLaunch{path: ScreenPath}, screenStartDeps{
		start: func(context.Context, screenLaunch) (startedScreen, error) { return startedScreen{direct: child}, nil },
		observe: func(context.Context, int) (screenIdentity, error) {
			return screenIdentity{}, errors.New("observer unavailable")
		},
	})
	if err == nil {
		t.Fatal("startOwnedScreen() error = nil, want failed kernel observation")
	}
	if child.signals != 1 || child.waits != 1 {
		t.Fatalf("observer-failure cleanup signals/waits = %d/%d, want 1/1", child.signals, child.waits)
	}
}

// A regression here would signal a reused PID after the direct child has
// exited. A mismatch must poison the generation without sending a signal.
func TestRuntimeWatchScreenPoisonsIdentityMismatchWithoutSignaling(t *testing.T) {
	child := &screenCommandFake{pid: 84, waitResult: make(chan error)}
	identity := screenIdentity{pid: 84, started: time.Unix(84, 0), unique: 84}
	owned := newTestOwnedScreen(identity, child)
	owned.observe = func(context.Context, int) (screenIdentity, error) {
		return screenIdentity{pid: 84, started: time.Unix(85, 0), unique: 85}, nil
	}
	runtime := Runtime{screen: owned, ScreenEvidence: owned.Evidence()}
	broker := NewBroker(BrokerConfig{})
	runtime.WatchScreen(broker)
	deadline := time.After(time.Second)
	for !broker.Poisoned() {
		select {
		case <-deadline:
			t.Fatal("identity mismatch did not poison broker")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	if child.signals != 0 {
		t.Fatalf("identity mismatch signals = %d, want no signal", child.signals)
	}
}

// A regression here would collapse a stale/reused-PID condition into an
// ordinary Screen exit, making recovery policy unable to distinguish them.
func TestCheckScreenClassifiesObservationAndIdentityFailures(t *testing.T) {
	identity := screenIdentity{pid: 841, started: time.Unix(841, 0), unique: 841}
	owned := newTestOwnedScreen(identity)
	owned.observe = func(context.Context, int) (screenIdentity, error) {
		return screenIdentity{}, errors.New("proc unavailable")
	}
	if err := owned.Check(context.Background()); !errors.Is(err, ErrScreenObservation) {
		t.Fatalf("observation failure = %v, want ErrScreenObservation", err)
	}
	owned.observe = func(context.Context, int) (screenIdentity, error) {
		return screenIdentity{pid: 841, started: time.Unix(842, 0), unique: 842}, nil
	}
	if err := owned.Check(context.Background()); !errors.Is(err, ErrScreenIdentityMismatch) {
		t.Fatalf("identity mismatch = %v, want ErrScreenIdentityMismatch", err)
	}
}

// A regression here would make Shutdown signal the process currently holding
// a reused PID instead of refusing the stale direct-child evidence.
func TestOwnedScreenStopRefusesIdentityMismatchWithoutSignal(t *testing.T) {
	child := &screenCommandFake{pid: 842}
	owned := newTestOwnedScreen(screenIdentity{pid: 842, started: time.Unix(842, 0), unique: 842}, child)
	owned.observe = func(context.Context, int) (screenIdentity, error) {
		return screenIdentity{pid: 842, started: time.Unix(843, 0), unique: 843}, nil
	}
	if err := owned.Stop(context.Background()); !errors.Is(err, ErrScreenIdentityMismatch) {
		t.Fatalf("Stop() error = %v, want ErrScreenIdentityMismatch", err)
	}
	if child.signals != 0 {
		t.Fatalf("identity-mismatched Stop() signals = %d, want 0", child.signals)
	}
}

// A regression here would permit concurrent watcher and shutdown paths to
// double-reap the exact direct child.
func TestOwnedScreenWaitCachesDirectChildReap(t *testing.T) {
	child := &screenCommandFake{pid: 85, waitResult: make(chan error, 1)}
	owned := newTestOwnedScreen(screenIdentity{pid: 85, started: time.Unix(85, 0), unique: 85}, child)
	first := make(chan error, 1)
	second := make(chan error, 1)
	go func() { first <- owned.Wait(context.Background()) }()
	go func() { second <- owned.Wait(context.Background()) }()
	deadline := time.After(time.Second)
	for child.waitCount() != 1 {
		select {
		case <-deadline:
			t.Fatal("Wait() did not invoke direct child exactly once")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	child.waitResult <- errors.New("screen exited")
	for _, result := range []<-chan error{first, second} {
		if err := <-result; err == nil || err.Error() != "screen exited" {
			t.Fatalf("cached Wait() = %v, want direct-child exit", err)
		}
	}
}

func newTestOwnedScreen(identity screenIdentity, child ...*screenCommandFake) *ownedScreen {
	var direct screenCommand = &screenCommandFake{pid: identity.pid}
	if len(child) != 0 {
		direct = child[0]
	}
	return newOwnedScreen(startedScreen{direct: direct}, identity, func(context.Context, int) (screenIdentity, error) {
		return identity, nil
	})
}

type screenCommandFake struct {
	pid        int
	mu         sync.Mutex
	signals    int
	waits      int
	waitResult chan error
}

func (c *screenCommandFake) PID() int { return c.pid }
func (c *screenCommandFake) Signal() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.signals++
	return nil
}
func (c *screenCommandFake) Kill() error { return c.Signal() }
func (c *screenCommandFake) Wait() error {
	c.mu.Lock()
	c.waits++
	result := c.waitResult
	c.mu.Unlock()
	if result == nil {
		return nil
	}
	return <-result
}
func (c *screenCommandFake) waitCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.waits
}

func TestCreateRuntimeProductionUnavailableDoesNotCreateGeneration(t *testing.T) {
	if productionRuntimeSupported() {
		t.Skip("production Screen runtime is available on this platform")
	}
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	_, err := CreateRuntime(context.Background(), root, "generation-unavailable", qualifiedScreenFact())
	if err == nil {
		t.Fatal("CreateRuntime() error = nil, want unavailable platform refusal")
	}
	if _, err := os.Lstat(filepath.Join(root, "generation-unavailable")); !os.IsNotExist(err) {
		t.Fatalf("unavailable production runtime created generation state: %v", err)
	}
}

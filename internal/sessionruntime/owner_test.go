package sessionruntime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/backend/tart"
	"github.com/weshofmann/boxwarden/internal/hostx"
	"github.com/weshofmann/boxwarden/internal/serialx"
	"github.com/weshofmann/boxwarden/internal/session"
	"github.com/weshofmann/boxwarden/internal/sshx"
	"github.com/weshofmann/boxwarden/internal/supervisor"
)

// Keep configuration and durable session loading real. Only external host
// qualification, CA inspection, PTY allocation and VM mechanics are doubled.
type fixture struct {
	owner                                       *Owner
	request                                     supervisor.LaunchRequest
	root, personal, tartPath, tartHome, softnet string
	record                                      session.Record
	trace                                       *traceLog
	serial                                      *fakeSerial
	handle                                      *fakeHandle
	observe                                     func(context.Context, string) (backend.Observation, error)
	hostErr, caErr, launchErr                   error
	serialErr                                   error
	launchConfig                                tart.LaunchConfig
	startRequest                                backend.StartRequest
	observationPaths                            [2]string
	observations                                int
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{root: filepath.Join(base, "work"), personal: filepath.Join(base, "personal"), tartPath: filepath.Join(base, "tart"), tartHome: filepath.Join(base, "tart-home"), softnet: filepath.Join(base, "softnet"), trace: &traceLog{}}
	for _, path := range []string{f.root, f.personal, f.tartHome} {
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{f.tartPath, f.softnet} {
		if err := os.WriteFile(path, []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	f.record = session.Record{Version: 2, Domain: "work", Name: "dev", ID: "13b0bf73-3bd5-4f1c-8bdc-71d50c36d6d0", Mode: session.ModeClean, IntendedState: session.StateStarting, Backend: session.BackendRef{Kind: "tart", ObjectID: "boxwarden-work-dev"}, GoldenRevision: "golden-r1", StartGeneration: "00112233-4455-4677-8899-aabbccddeeff", Readiness: session.ReadinessRecord{Status: session.ReadinessStarting}}
	if err := session.SaveRecord(f.root, "work", f.record); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(base, "config.json")
	contents := fmt.Sprintf(`{"version":2,"domains":{"work":{"state_root":%q},"personal":{"state_root":%q}},"host":{"tart_executable":%q,"tart_home":%q,"softnet_source":%q}}`, f.root, f.personal, f.tartPath, f.tartHome, f.softnet)
	if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
	f.request = supervisor.LaunchRequest{Binding: supervisor.Binding{Domain: "work", SessionID: f.record.ID, BackendKind: "tart", BackendObject: "boxwarden-work-dev", Generation: f.record.StartGeneration}, RuntimeDirectory: filepath.Join(f.root, "runtime", "work", f.record.ID, f.record.StartGeneration), HostConfigPath: path, SessionRecordName: "dev"}
	f.serial = &fakeSerial{trace: f.trace, endpoint: filepath.Join(f.request.RuntimeDirectory, "serial", "tart-serial")}
	f.handle = &fakeHandle{trace: f.trace, done: make(chan struct{}), waiting: make(chan struct{})}
	f.owner = &Owner{deps: dependencies{
		host: hostFunc(func(_ context.Context, r hostx.Request) (hostx.RuntimeExpectation, error) {
			f.trace.add("host")
			if !reflect.DeepEqual(r, hostx.Request{TartPath: f.tartPath, TartHome: f.tartHome, SoftnetPath: f.softnet, ConfiguredStateRoots: []string{f.personal, f.root}}) {
				t.Errorf("host request = %#v", r)
			}
			return hostx.RuntimeExpectation{Manifest: hostx.Manifest{Tart: hostx.ToolIdentity{Path: f.tartPath}, TartHome: f.tartHome, Operator: hostx.Operator{Home: "/admitted/operator", Name: "admitted-operator"}}, SoftnetBinDir: "/qualified/softnet/bin"}, f.hostErr
		}),
		ca: caFunc(func(_ context.Context, selected sshx.Domain, all []sshx.Domain) (sshx.CAIdentity, error) {
			f.trace.add("ca")
			if selected != (sshx.Domain{ID: "work", StateRoot: f.root}) || !reflect.DeepEqual(all, []sshx.Domain{{ID: "personal", StateRoot: f.personal}, {ID: "work", StateRoot: f.root}}) {
				t.Errorf("CA selection/configuration = %#v/%#v", selected, all)
			}
			return sshx.CAIdentity{Domain: "work"}, f.caErr
		}),
		observer: func(path, home string) backend.Observer {
			f.observationPaths = [2]string{path, home}
			return observerFunc(func(ctx context.Context, object string) (backend.Observation, error) {
				f.trace.add("observe:" + object)
				if f.observe != nil {
					return f.observe(ctx, object)
				}
				f.observations++
				state := backend.ObjectRunning
				if f.observations == 1 {
					state = backend.ObjectStopped
				}
				select {
				case <-f.handle.done:
					state = backend.ObjectStopped
				default:
				}
				return backend.Observation{ObjectID: object, Exists: true, State: state}, nil
			})
		},
		serial: func(_ context.Context, dir string) (serialRuntime, error) {
			f.trace.add("serial")
			if dir != f.request.RuntimeDirectory {
				t.Errorf("serial directory = %s", dir)
			}
			if f.serialErr != nil {
				return nil, f.serialErr
			}
			return f.serial, nil
		},
		launcher: func(c tart.LaunchConfig) backend.Starter {
			f.launchConfig = c
			return starterFunc(func(_ context.Context, r backend.StartRequest) (backend.Handle, error) {
				f.trace.add("launch")
				f.startRequest = r
				if f.launchErr != nil {
					return nil, f.launchErr
				}
				return f.handle, nil
			})
		},
		pollInterval: time.Millisecond, startupTimeout: time.Second,
	}}
	return f
}

// Catch omitted authoritative reload, narrowed CA/host admission, request
// fields used as launch authority, or readiness inferred from process start.
func TestStartReloadsAdmissionAndRetainsExactRuntimeWithoutReady(t *testing.T) {
	f := newFixture(t)
	if err := f.owner.Start(context.Background(), f.request); err != nil {
		t.Fatal(err)
	}
	if got := f.trace.all(); !reflect.DeepEqual(got, []string{"host", "ca", "observe:boxwarden-work-dev", "serial", "launch", "observe:boxwarden-work-dev"}) {
		t.Fatalf("admission/launch order: %v", got)
	}
	if f.observationPaths != [2]string{f.tartPath, f.tartHome} {
		t.Fatalf("observer paths: %#v", f.observationPaths)
	}
	want := tart.LaunchConfig{TartPath: f.tartPath, TartHome: f.tartHome, SoftnetBinDir: "/qualified/softnet/bin", OperatorHome: "/admitted/operator", OperatorName: "admitted-operator"}
	if f.launchConfig != want {
		t.Fatalf("launch configuration = %#v", f.launchConfig)
	}
	if f.startRequest != (backend.StartRequest{ObjectID: "boxwarden-work-dev", SerialDevice: f.serial.endpoint, GenerationDirectory: f.request.RuntimeDirectory}) {
		t.Fatalf("start request = %#v", f.startRequest)
	}
	s := f.owner.Snapshot(context.Background())
	if s.Binding != f.request.Binding || !s.BackendRunning || !s.SerialHealthy || s.PinPresent || s.CertificateCurrent || s.ProbeOK || s.ZoneMatches || s.ObservedAt.IsZero() {
		t.Fatalf("snapshot = %#v", s)
	}
	stored, err := session.LoadRecord(f.root, "work", "dev")
	if err != nil || stored != f.record {
		t.Fatalf("durable record changed: %#v %v", stored, err)
	}
	if err := f.owner.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := f.owner.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestStartRejectsBindingOrAdmissionBeforeMutation(t *testing.T) {
	for name, mutate := range map[string]func(*fixture){
		"missing exact configuration": func(f *fixture) { f.request.HostConfigPath += ".absent" },
		"unknown domain":              func(f *fixture) { f.request.Binding.Domain = "unknown" },
		"exact named record":          func(f *fixture) { f.request.SessionRecordName = "other" },
		"session identity":            func(f *fixture) { f.request.Binding.SessionID = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee" },
		"generation":                  func(f *fixture) { f.request.Binding.Generation = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee" },
		"backend object":              func(f *fixture) { f.request.Binding.BackendObject = "boxwarden-work-other" },
		"backend kind":                func(f *fixture) { f.request.Binding.BackendKind = "other" },
		"foreign runtime root": func(f *fixture) {
			f.request.RuntimeDirectory = filepath.Join(f.personal, "runtime", "work", f.record.ID, f.record.StartGeneration)
		},
		"host rejected": func(f *fixture) { f.hostErr = errors.New("host drift") },
		"CA rejected":   func(f *fixture) { f.caErr = errors.New("another configured CA is partial") },
		"running durable intent": func(f *fixture) {
			f.record.IntendedState = session.StateRunning
			f.record.Readiness.Status = session.ReadinessReady
			if err := session.SaveRecord(f.root, "work", f.record); err != nil {
				t.Fatal(err)
			}
		},
		"running backend": func(f *fixture) {
			f.observe = func(_ context.Context, object string) (backend.Observation, error) {
				return backend.Observation{ObjectID: object, Exists: true, State: backend.ObjectRunning}, nil
			}
		},
		"wrong observed object": func(f *fixture) {
			f.observe = func(context.Context, string) (backend.Observation, error) {
				return backend.Observation{ObjectID: "foreign", Exists: true, State: backend.ObjectStopped}, nil
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			mutate(f)
			if err := f.owner.Start(context.Background(), f.request); err == nil {
				t.Fatal("accepted invalid request/admission")
			}
			for _, event := range f.trace.all() {
				if event == "serial" || event == "launch" || event == "stop" {
					t.Fatalf("mutated before admission: %v", f.trace.all())
				}
			}
		})
	}
}

func TestStartPollsStoppedUntilExactRunning(t *testing.T) {
	f := newFixture(t)
	calls := 0
	f.observe = func(_ context.Context, object string) (backend.Observation, error) {
		calls++
		state := backend.ObjectStopped
		if calls >= 4 {
			state = backend.ObjectRunning
		}
		return backend.Observation{ObjectID: object, Exists: true, State: state}, nil
	}
	if err := f.owner.Start(context.Background(), f.request); err != nil {
		t.Fatal(err)
	}
	if calls != 4 {
		t.Fatalf("running proved after %d observations, want 4", calls)
	}
	_ = f.owner.Stop(context.Background())
	_ = f.owner.Wait(context.Background())
}

// Catch early serial close, reconstructed stop authority, or repeated actual
// Wait. The canceled waiter must not release resources while Tart still lives.
func TestWaitRetainsSerialUntilActualReapAndStopsOnlyExactHandle(t *testing.T) {
	f := newFixture(t)
	if err := f.owner.Start(context.Background(), f.request); err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan error, 1)
	go func() { done <- f.owner.Wait(canceled) }()
	select {
	case <-f.handle.waiting:
	case <-time.After(time.Second):
		t.Fatal("actual wait never began")
	}
	select {
	case err := <-done:
		t.Fatalf("Wait returned before actual reap: %v", err)
	default:
	}
	if strings.Contains(strings.Join(f.trace.all(), ","), "close") {
		t.Fatal("serial closed before reap")
	}
	if err := f.owner.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := f.owner.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := f.owner.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	events := strings.Join(f.trace.all(), ",")
	if strings.Count(events, "wait") != 1 || strings.Count(events, "stop") != 1 || !strings.HasSuffix(events, "wait,stop,reap,close") {
		t.Fatalf("lifetime order = %s", events)
	}
	if s := f.owner.Snapshot(context.Background()); s.BackendRunning || s.SerialHealthy {
		t.Fatalf("snapshot after reap: %#v", s)
	}
}

func TestStartFailureCleansOnlyAcquiredRuntimeInOrder(t *testing.T) {
	for _, phase := range []string{"serial", "launch", "poison", "observe", "wrong object", "never running"} {
		t.Run(phase, func(t *testing.T) {
			f := newFixture(t)
			if phase == "serial" {
				f.serialErr = errors.New("exclusive serial admission failed")
			}
			if phase == "launch" {
				f.launchErr = errors.New("spawn failed")
			}
			f.owner.deps.startupTimeout = 10 * time.Millisecond
			calls := 0
			f.observe = func(_ context.Context, object string) (backend.Observation, error) {
				calls++
				if calls > 1 {
					select {
					case <-f.handle.done:
						return backend.Observation{ObjectID: object, Exists: true, State: backend.ObjectStopped}, nil
					default:
					}
				}
				if calls > 1 && phase == "observe" {
					return backend.Observation{}, errors.New("list failed")
				}
				if calls > 1 && phase == "wrong object" {
					return backend.Observation{ObjectID: "foreign", Exists: true, State: backend.ObjectRunning}, nil
				}
				if calls == 1 && phase == "poison" {
					f.serial.poison()
				}
				return backend.Observation{ObjectID: object, Exists: true, State: backend.ObjectStopped}, nil
			}
			if err := f.owner.Start(context.Background(), f.request); err == nil {
				t.Fatal("failure accepted")
			}
			events := strings.Join(f.trace.all(), ",")
			switch phase {
			case "serial":
				if strings.Contains(events, "close") || strings.Contains(events, "launch") {
					t.Fatalf("cleaned unacquired resource: %s", events)
				}
			case "launch", "poison":
				if !strings.HasSuffix(events, "close") || strings.Contains(events, "stop") {
					t.Fatalf("pre-handle cleanup = %s", events)
				}
			default:
				if !strings.HasSuffix(events, "stop,wait,reap,close,observe:boxwarden-work-dev") {
					t.Fatalf("post-handle cleanup = %s", events)
				}
			}
		})
	}
}

func TestSnapshotFailsClosedOnSerialPoisonAndObservationError(t *testing.T) {
	f := newFixture(t)
	if err := f.owner.Start(context.Background(), f.request); err != nil {
		t.Fatal(err)
	}
	f.serial.poison()
	if s := f.owner.Snapshot(context.Background()); s.SerialHealthy {
		t.Fatalf("poisoned snapshot = %#v", s)
	}
	f.observe = func(context.Context, string) (backend.Observation, error) {
		return backend.Observation{}, errors.New("observation failed")
	}
	if s := f.owner.Snapshot(context.Background()); s.BackendRunning {
		t.Fatalf("unproved running snapshot = %#v", s)
	}
	_ = f.owner.Stop(context.Background())
	_ = f.owner.Wait(context.Background())
}

func TestSnapshotCannotPublishRunningAfterConcurrentReap(t *testing.T) {
	f := newFixture(t)
	if err := f.owner.Start(context.Background(), f.request); err != nil {
		t.Fatal(err)
	}
	observing, release := make(chan struct{}), make(chan struct{})
	f.observe = func(_ context.Context, object string) (backend.Observation, error) {
		close(observing)
		<-release
		return backend.Observation{ObjectID: object, Exists: true, State: backend.ObjectRunning}, nil
	}
	snapshotDone := make(chan supervisor.Snapshot, 1)
	go func() { snapshotDone <- f.owner.Snapshot(context.Background()) }()
	<-observing
	var group sync.WaitGroup
	for range 8 {
		group.Add(1)
		go func() {
			defer group.Done()
			if err := f.owner.Stop(context.Background()); err != nil {
				t.Error(err)
			}
			if err := f.owner.Wait(context.Background()); err != nil {
				t.Error(err)
			}
		}()
	}
	group.Wait()
	close(release)
	if snapshot := <-snapshotDone; snapshot.BackendRunning || snapshot.SerialHealthy {
		t.Fatalf("published running after reap: %#v", snapshot)
	}
	events := strings.Join(f.trace.all(), ",")
	for _, event := range []string{"stop", "wait", "reap", "close"} {
		if strings.Count(events, event) != 1 {
			t.Fatalf("%s repeated or absent: %s", event, events)
		}
	}
}

func TestPostHandleFailureJoinsCleanupErrorsAndRequiresStoppedProof(t *testing.T) {
	f := newFixture(t)
	f.handle.stopErr = errors.New("stop failure")
	f.handle.waitErr = errors.New("wait failure")
	f.serial.closeErr = errors.New("serial close failure")
	calls := 0
	f.observe = func(_ context.Context, object string) (backend.Observation, error) {
		calls++
		if calls == 1 {
			return backend.Observation{ObjectID: object, Exists: true, State: backend.ObjectStopped}, nil
		}
		if calls == 2 {
			return backend.Observation{}, errors.New("startup observation failure")
		}
		return backend.Observation{ObjectID: object, Exists: true, State: backend.ObjectRunning}, nil
	}
	err := f.owner.Start(context.Background(), f.request)
	for _, want := range []string{"startup observation failure", "stop failure", "wait failure", "serial close failure", "stopped state was not proved"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("missing %q from cleanup error: %v", want, err)
		}
	}
	if events := strings.Join(f.trace.all(), ","); !strings.HasSuffix(events, "stop,wait,reap,close,observe:boxwarden-work-dev") {
		t.Fatalf("failed cleanup sequence: %s", events)
	}
}

func TestCancellationOrPoisonAfterHandleStillReapsAndCloses(t *testing.T) {
	for _, failure := range []string{"cancel", "poison"} {
		t.Run(failure, func(t *testing.T) {
			f := newFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			launch := f.owner.deps.launcher
			f.owner.deps.launcher = func(c tart.LaunchConfig) backend.Starter {
				starter := launch(c)
				return starterFunc(func(ctx context.Context, r backend.StartRequest) (backend.Handle, error) {
					h, err := starter.Start(ctx, r)
					if failure == "cancel" {
						cancel()
					} else {
						f.serial.poison()
					}
					return h, err
				})
			}
			if err := f.owner.Start(ctx, f.request); err == nil {
				t.Fatal("accepted failed launch")
			}
			if events := strings.Join(f.trace.all(), ","); !strings.HasSuffix(events, "stop,wait,reap,close,observe:boxwarden-work-dev") {
				t.Fatalf("canceled/poisoned cleanup: %s", events)
			}
		})
	}
}

func TestOwnerCannotBeReusedToAcquireSecondRuntime(t *testing.T) {
	f := newFixture(t)
	if err := f.owner.Start(context.Background(), f.request); err != nil {
		t.Fatal(err)
	}
	if err := f.owner.Start(context.Background(), f.request); err == nil {
		t.Fatal("reused owner")
	}
	if events := strings.Join(f.trace.all(), ","); strings.Count(events, "launch") != 1 {
		t.Fatalf("second launch: %s", events)
	}
	_ = f.owner.Stop(context.Background())
	_ = f.owner.Wait(context.Background())
}

type traceLog struct {
	mu     sync.Mutex
	events []string
}

func (l *traceLog) add(s string) { l.mu.Lock(); defer l.mu.Unlock(); l.events = append(l.events, s) }
func (l *traceLog) all() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.events...)
}

type fakeSerial struct {
	mu       sync.Mutex
	trace    *traceLog
	endpoint string
	err      error
	closeErr error
}

func (s *fakeSerial) TartSlave() string { return s.endpoint }
func (s *fakeSerial) Err() error        { s.mu.Lock(); defer s.mu.Unlock(); return s.err }
func (s *fakeSerial) poison()           { s.mu.Lock(); defer s.mu.Unlock(); s.err = serialx.ErrPoisoned }
func (s *fakeSerial) Close() error      { s.trace.add("close"); s.poison(); return s.closeErr }

type fakeHandle struct {
	trace            *traceLog
	done, waiting    chan struct{}
	stop             sync.Once
	stopErr, waitErr error
}

func (h *fakeHandle) Stop(context.Context) error {
	h.trace.add("stop")
	h.stop.Do(func() { close(h.done) })
	return h.stopErr
}
func (h *fakeHandle) Wait(ctx context.Context) error {
	h.trace.add("wait")
	close(h.waiting)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-h.done:
	}
	h.trace.add("reap")
	return h.waitErr
}

type hostFunc func(context.Context, hostx.Request) (hostx.RuntimeExpectation, error)

func (f hostFunc) CheckRuntime(c context.Context, r hostx.Request) (hostx.RuntimeExpectation, error) {
	return f(c, r)
}

type caFunc func(context.Context, sshx.Domain, []sshx.Domain) (sshx.CAIdentity, error)

func (f caFunc) Check(c context.Context, d sshx.Domain, all []sshx.Domain) (sshx.CAIdentity, error) {
	return f(c, d, all)
}

type observerFunc func(context.Context, string) (backend.Observation, error)

func (f observerFunc) Observe(c context.Context, s string) (backend.Observation, error) {
	return f(c, s)
}

type starterFunc func(context.Context, backend.StartRequest) (backend.Handle, error)

func (f starterFunc) Start(c context.Context, r backend.StartRequest) (backend.Handle, error) {
	return f(c, r)
}

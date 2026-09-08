// Package sessionruntime composes the authoritative detached session runtime.
package sessionruntime

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/backend/tart"
	"github.com/weshofmann/boxwarden/internal/config"
	"github.com/weshofmann/boxwarden/internal/execx"
	"github.com/weshofmann/boxwarden/internal/guestproto"
	"github.com/weshofmann/boxwarden/internal/hostx"
	"github.com/weshofmann/boxwarden/internal/serialx"
	"github.com/weshofmann/boxwarden/internal/session"
	"github.com/weshofmann/boxwarden/internal/sshx"
	"github.com/weshofmann/boxwarden/internal/supervisor"
)

type serialRuntime interface {
	TartSlave() string
	Bootstrap(context.Context, guestproto.SerialRequest) (guestproto.SerialResult, error)
	Err() error
	Close() error
}

type pinStore interface {
	Admit(context.Context, sshx.Binding, sshx.ObservedHostKey) (sshx.HostKeyPin, error)
	Load(context.Context, sshx.Binding) (sshx.HostKeyPin, error)
}

type dependencies struct {
	host           session.RuntimeChecker
	ca             session.CAValidator
	observer       func(string, string) backend.Observer
	launcher       func(tart.LaunchConfig) backend.Starter
	serial         func(context.Context, string) (serialRuntime, error)
	pin            func(sshx.Domain) pinStore
	newNonce       func() (string, error)
	pollInterval   time.Duration
	startupTimeout time.Duration
}

// Owner is a single-use runtime owner. Only the exact returned backend handle
// and the serialx runtime confer lifetime authority; neither is reconstructed.
type Owner struct {
	deps                dependencies
	mu                  sync.Mutex
	observationMu       sync.Mutex
	bootstrapMu         sync.Mutex
	attempted, active   bool
	binding             supervisor.Binding
	sshBinding          sshx.Binding
	ca                  sshx.CAIdentity
	observer            backend.Observer
	handle              backend.Handle
	serial              serialRuntime
	pins                pinStore
	bootstrapRequest    guestproto.SerialRequest
	bootstrapResult     guestproto.SerialResult
	bootstrapResolved   bool
	expectedPin         sshx.HostKeyPin
	bootstrapDiagnostic string
	stopOnce, waitOnce  sync.Once
	stopErr, waitErr    error
}

// NewOwner constructs the production detached-child composition. LaunchRequest
// carries no admitted objects: Start reads the configuration and record again.
func NewOwner() *Owner {
	return &Owner{deps: dependencies{
		host: hostx.NewSystemDoctor(),
		ca:   sshx.NewCAStore(sshx.CAStoreOptions{Runner: sshx.NewExecRunner(), Identity: sshx.OSIdentity{}}),
		observer: func(path, home string) backend.Observer {
			return tart.NewQualifiedObserver(execx.OSRunner{MaxOutputBytes: 1 << 20}, path, home)
		},
		launcher:     func(c tart.LaunchConfig) backend.Starter { return tart.NewLauncher(c) },
		serial:       func(ctx context.Context, dir string) (serialRuntime, error) { return serialx.CreateRuntime(ctx, dir) },
		pin:          func(domain sshx.Domain) pinStore { return sshx.NewPinStore(domain) },
		newNonce:     sshx.RandomUUID,
		pollInterval: 100 * time.Millisecond, startupTimeout: 30 * time.Second,
	}}
}

func (o *Owner) Start(ctx context.Context, request supervisor.LaunchRequest) error {
	o.mu.Lock()
	if o.attempted {
		o.mu.Unlock()
		return fmt.Errorf("runtime owner already used")
	}
	o.attempted = true
	o.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if o.deps.host == nil || o.deps.ca == nil || o.deps.observer == nil || o.deps.launcher == nil || o.deps.serial == nil || o.deps.pin == nil || o.deps.newNonce == nil || o.deps.pollInterval <= 0 || o.deps.startupTimeout <= 0 {
		return fmt.Errorf("runtime owner dependencies are required")
	}
	if !filepath.IsAbs(request.HostConfigPath) || filepath.Clean(request.HostConfigPath) != request.HostConfigPath {
		return fmt.Errorf("configuration path must be canonical and absolute")
	}
	loaded, err := config.Load(request.HostConfigPath)
	if err != nil {
		return fmt.Errorf("reload host configuration: %w", err)
	}
	selected, err := loaded.Domain(request.Binding.Domain)
	if err != nil {
		return err
	}
	record, err := session.LoadRecord(selected.StateRoot, string(selected.ID), request.SessionRecordName)
	if err != nil {
		return fmt.Errorf("reload durable session: %w", err)
	}
	binding := supervisor.Binding{Domain: string(record.Domain), SessionID: record.ID, BackendKind: record.Backend.Kind, BackendObject: record.Backend.ObjectID, Generation: record.StartGeneration}
	if record.IntendedState != session.StateStarting || record.StartGeneration == "" || record.Backend.Kind != "tart" || binding != request.Binding {
		return fmt.Errorf("durable starting session does not match exact launch binding")
	}
	directory := filepath.Join(selected.StateRoot, "runtime", binding.Domain, binding.SessionID, binding.Generation)
	if request.RuntimeDirectory != directory {
		return fmt.Errorf("runtime directory does not match configured exact generation")
	}
	admission, err := loaded.HostAdmission()
	if err != nil {
		return err
	}
	expectation, err := o.deps.host.CheckRuntime(ctx, hostx.Request{ConfiguredStateRoots: admission.ConfiguredStateRoots, TartPath: admission.Host.TartExecutable, TartHome: admission.Host.TartHome, SoftnetPath: admission.Host.SoftnetSource})
	if err != nil {
		return fmt.Errorf("admit current host runtime: %w", err)
	}
	var domains []sshx.Domain
	for _, d := range loaded.Domains() {
		domains = append(domains, sshx.Domain{ID: d.ID, StateRoot: d.StateRoot})
	}
	selectedDomain := sshx.Domain{ID: selected.ID, StateRoot: selected.StateRoot}
	ca, err := o.deps.ca.Check(ctx, selectedDomain, domains)
	if err != nil {
		return fmt.Errorf("admit configured domain CAs: %w", err)
	}
	pins := o.deps.pin(selectedDomain)
	if pins == nil {
		return fmt.Errorf("host-key pin store is required")
	}
	observer := o.deps.observer(admission.Host.TartExecutable, admission.Host.TartHome)
	if observer == nil {
		return fmt.Errorf("qualified backend observer is required")
	}
	observation, err := observeExact(ctx, observer, binding.BackendObject)
	if err != nil {
		return err
	}
	if observation.State != backend.ObjectStopped {
		return fmt.Errorf("exact backend must be stopped before launch")
	}
	serial, err := o.deps.serial(ctx, directory)
	if err != nil {
		return fmt.Errorf("create serial runtime: %w", err)
	}
	if serial == nil {
		return fmt.Errorf("serial runtime is unavailable")
	}
	if err := serial.Err(); err != nil {
		return errors.Join(err, serial.Close())
	}
	launchConfig := tart.LaunchConfig{
		TartPath: admission.Host.TartExecutable, TartHome: admission.Host.TartHome,
		SoftnetBinDir: expectation.SoftnetBinDir,
		OperatorHome:  expectation.Manifest.Operator.Home, OperatorName: expectation.Manifest.Operator.Name,
	}
	launcher := o.deps.launcher(launchConfig)
	if launcher == nil {
		return errors.Join(fmt.Errorf("Tart launcher is unavailable"), serial.Close())
	}
	handle, err := launcher.Start(ctx, backend.StartRequest{ObjectID: record.Backend.ObjectID, SerialDevice: serial.TartSlave(), GenerationDirectory: directory})
	if handle == nil {
		if err == nil {
			err = fmt.Errorf("Tart returned no retained handle")
		}
		return errors.Join(err, serial.Close())
	}
	sshBinding := sshx.Binding{Domain: record.Domain, SessionID: record.ID, BackendKind: record.Backend.Kind, BackendObject: record.Backend.ObjectID}
	o.mu.Lock()
	o.binding, o.sshBinding, o.ca, o.observer, o.handle, o.serial, o.pins = binding, sshBinding, ca, observer, handle, serial, pins
	o.mu.Unlock()
	if err == nil {
		err = o.awaitRunning(ctx)
	}
	if err != nil {
		return o.failedStart(err)
	}
	o.mu.Lock()
	o.active = true
	o.mu.Unlock()
	return nil
}

// Bootstrap performs the one fixed guest exchange and exact pin admission for
// this retained runtime. Once serialx has validated a result, a retry reuses it
// only to repeat absent-or-exact pin admission; it never emits another command.
func (o *Owner) Bootstrap(ctx context.Context) error {
	o.bootstrapMu.Lock()
	defer o.bootstrapMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	o.mu.Lock()
	active, binding, sshBinding, ca, serial, pins := o.active, o.binding, o.sshBinding, o.ca, o.serial, o.pins
	resolved, request, result := o.bootstrapResolved, o.bootstrapRequest, o.bootstrapResult
	o.mu.Unlock()
	if !active || serial == nil || pins == nil {
		return fmt.Errorf("exact runtime is not available for bootstrap")
	}
	if !resolved {
		nonce, err := o.deps.newNonce()
		if err != nil {
			return fmt.Errorf("generate bootstrap nonce: %w", err)
		}
		request = guestproto.SerialRequest{
			Version: guestproto.Version, Nonce: nonce, StartGeneration: binding.Generation,
			Association: guestproto.Association{Domain: binding.Domain, SessionID: binding.SessionID, BackendKind: binding.BackendKind, BackendObject: binding.BackendObject},
			CAPublicKey: ca.PublicKey, CAFingerprint: ca.Fingerprint, Principal: sshBinding.Principal(),
		}
		if err := request.Validate(); err != nil {
			return fmt.Errorf("construct exact serial bootstrap request: %w", err)
		}
		result, err = serial.Bootstrap(ctx, request)
		if err != nil {
			o.setBootstrapDiagnostic("serial bootstrap failed")
			return fmt.Errorf("perform exact serial bootstrap: %w", err)
		}
		if _, _, err := guestproto.EncodeSerialFrame(request, result); err != nil {
			o.setBootstrapDiagnostic("serial bootstrap result failed validation")
			return fmt.Errorf("validate exact serial bootstrap result: %w", err)
		}
		o.mu.Lock()
		o.bootstrapRequest, o.bootstrapResult, o.bootstrapResolved = request, result, true
		o.mu.Unlock()
	}
	pin, err := pins.Admit(ctx, sshBinding, sshx.ObservedHostKey{Algorithm: "ssh-ed25519", PublicKey: result.HostPublicKey})
	if err != nil {
		o.setBootstrapDiagnostic("host-key pin admission failed")
		return fmt.Errorf("admit exact serial-observed host-key pin: %w", err)
	}
	o.mu.Lock()
	o.expectedPin = pin
	o.bootstrapDiagnostic = ""
	o.mu.Unlock()
	return nil
}

func (o *Owner) setBootstrapDiagnostic(value string) {
	o.mu.Lock()
	o.bootstrapDiagnostic = value
	o.mu.Unlock()
}

func observeExact(ctx context.Context, observer backend.Observer, object string) (backend.Observation, error) {
	observation, err := observer.Observe(ctx, object)
	if err != nil {
		return backend.Observation{}, fmt.Errorf("observe exact backend: %w", err)
	}
	if observation.ObjectID != object || !observation.Exists || (observation.State != backend.ObjectStopped && observation.State != backend.ObjectRunning) {
		return backend.Observation{}, fmt.Errorf("backend observation is missing or does not prove the exact object")
	}
	return observation, nil
}

func (o *Owner) awaitRunning(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, o.deps.startupTimeout)
	defer cancel()
	ticker := time.NewTicker(o.deps.pollInterval)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := o.serial.Err(); err != nil {
			return err
		}
		observation, err := observeExact(ctx, o.observer, o.binding.BackendObject)
		if err != nil {
			return err
		}
		if observation.State == backend.ObjectRunning {
			return o.serial.Err()
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (o *Owner) failedStart(cause error) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	stopErr := o.Stop(ctx)
	cancel()
	// Never let a request timeout stand in for actual reap. The supervisor must
	// retain the generation lock until this wait and serial cleanup finish.
	waitErr := o.Wait(context.Background())
	ctx, cancel = context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	observation, observeErr := observeExact(ctx, o.observer, o.binding.BackendObject)
	if observeErr == nil && observation.State != backend.ObjectStopped {
		observeErr = fmt.Errorf("exact backend stopped state was not proved after reap")
	}
	if observeErr != nil {
		observeErr = fmt.Errorf("%w: %w", supervisor.ErrRuntimeCleanupUnproven, observeErr)
	}
	return errors.Join(cause, stopErr, waitErr, observeErr)
}

// Snapshot observes afresh within the control caller's bound and never promotes
// backend/serial facts into future guest trust, SSH, time-zone or READY
// predicates.
func (o *Owner) Snapshot(ctx context.Context) supervisor.Snapshot {
	o.observationMu.Lock()
	defer o.observationMu.Unlock()
	o.mu.Lock()
	snapshot := supervisor.Snapshot{Binding: o.binding, ObservedAt: time.Now()}
	active, observer, serial, pins, binding, expectedPin, diagnostic := o.active, o.observer, o.serial, o.pins, o.sshBinding, o.expectedPin, o.bootstrapDiagnostic
	o.mu.Unlock()
	if !active {
		return snapshot
	}
	observation, err := observeExact(ctx, observer, snapshot.Binding.BackendObject)
	o.mu.Lock()
	defer o.mu.Unlock()
	snapshot.ObservedAt = time.Now()
	snapshot.BackendRunning = o.active && err == nil && observation.State == backend.ObjectRunning
	snapshot.SerialHealthy = o.active && serial.Err() == nil
	if snapshot.BackendRunning && snapshot.SerialHealthy && expectedPin.Version != 0 {
		pin, pinErr := pins.Load(ctx, binding)
		snapshot.PinPresent = pinErr == nil && pin == expectedPin
		if !snapshot.PinPresent {
			diagnostic = "exact host-key pin verification failed"
		}
	}
	if err != nil {
		snapshot.Diagnostic = "exact backend observation failed"
	} else {
		snapshot.Diagnostic = diagnostic
	}
	return snapshot
}

func (o *Owner) Stop(ctx context.Context) error {
	o.mu.Lock()
	handle := o.handle
	o.mu.Unlock()
	if handle == nil {
		return fmt.Errorf("runtime handle is unavailable")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	o.stopOnce.Do(func() { o.stopErr = handle.Stop(ctx) })
	return o.stopErr
}

// Wait deliberately outlives ctx: returning authorizes outer namespace cleanup
// and therefore requires actual reap, even when a caller has stopped waiting.
func (o *Owner) Wait(context.Context) error {
	o.mu.Lock()
	handle, serial := o.handle, o.serial
	o.mu.Unlock()
	if handle == nil {
		return fmt.Errorf("runtime handle is unavailable")
	}
	o.waitOnce.Do(func() {
		err := handle.Wait(context.Background())
		o.mu.Lock()
		o.active = false
		o.mu.Unlock()
		o.waitErr = errors.Join(err, serial.Close())
	})
	return o.waitErr
}

var _ supervisor.RuntimeOwner = (*Owner)(nil)

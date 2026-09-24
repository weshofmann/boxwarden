// Package sessionruntime composes the authoritative detached session runtime.
package sessionruntime

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"os"
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
	"github.com/weshofmann/boxwarden/internal/timezonex"
	"github.com/weshofmann/boxwarden/internal/workspacex"
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

type rebuildPinStore interface {
	TransitionRebuild(context.Context, string, sshx.Binding, sshx.Binding, bool, string, sshx.ObservedHostKey) (sshx.HostKeyPin, error)
}

type certificateIssuer interface {
	Issue(context.Context, sshx.Binding, string, string) (sshx.Certificate, error)
}

type managementClient interface {
	timezonex.ZoneClient
	Probe(context.Context, sshx.Connection, sshx.ProbeRequest) (sshx.ProbeResult, error)
	EnsureWorkspaces(context.Context, sshx.Connection, []sshx.WorkspaceMount) error
	InspectPackages(context.Context, sshx.Connection, []string) ([]sshx.PackageVersion, error)
	InspectIdentity(context.Context, sshx.Connection) (sshx.GuestIdentity, error)
}

type importClient interface {
	TransferImport(context.Context, sshx.Connection, string, string, string, string) (sshx.ImportReceipt, error)
}

type dependencies struct {
	host           session.RuntimeChecker
	ca             session.CAValidator
	observer       func(string, string) backend.Observer
	launcher       func(tart.LaunchConfig) backend.Starter
	serial         func(context.Context, string) (serialRuntime, error)
	pin            func(sshx.Domain) pinStore
	address        func(string, string) backend.AddressResolver
	key            func(context.Context, string) (string, error)
	issuer         func(sshx.CAIdentity) certificateIssuer
	client         managementClient
	importer       importClient
	zone           func() (string, error)
	now            func() time.Time
	newNonce       func() (string, error)
	pollInterval   time.Duration
	startupTimeout time.Duration
	renewInterval  time.Duration
}

// Owner is a single-use runtime owner. Only the exact returned backend handle
// and the serialx runtime confer lifetime authority; neither is reconstructed.
type Owner struct {
	deps                            dependencies
	mu                              sync.Mutex
	observationMu                   sync.Mutex
	bootstrapMu                     sync.Mutex
	readyMu                         sync.Mutex
	attempted, active               bool
	binding                         supervisor.Binding
	sshBinding                      sshx.Binding
	ca                              sshx.CAIdentity
	observer                        backend.Observer
	handle                          backend.Handle
	serial                          serialRuntime
	pins                            pinStore
	rebuildJournal                  *session.RebuildJournal
	stateRoot, sessionName          string
	bootstrapRequest                guestproto.SerialRequest
	bootstrapResult                 guestproto.SerialResult
	bootstrapResolved               bool
	expectedPin                     sshx.HostKeyPin
	bootstrapDiagnostic             string
	connection                      sshx.Connection
	certificate                     sshx.Certificate
	readyEstablished                bool
	workspaceMounts                 []sshx.WorkspaceMount
	readyAttempted                  bool
	maintenanceCancel               context.CancelFunc
	maintenanceDone                 chan struct{}
	runtimePath, tartPath, tartHome string
	stopMu                          sync.Mutex
	stopSent, graceSent             bool
	waitOnce                        sync.Once
	waitErr                         error
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
		launcher: func(c tart.LaunchConfig) backend.Starter { return tart.NewLauncher(c) },
		serial:   func(ctx context.Context, dir string) (serialRuntime, error) { return serialx.CreateRuntime(ctx, dir) },
		pin:      func(domain sshx.Domain) pinStore { return sshx.NewPinStore(domain) },
		address: func(path, home string) backend.AddressResolver {
			return tart.NewAddressResolver(execx.OSRunner{MaxOutputBytes: 1 << 20}, path, home)
		},
		key: func(ctx context.Context, directory string) (string, error) {
			return sshx.EnsureClientKey(ctx, sshx.NewExecRunner(), directory)
		},
		issuer: func(ca sshx.CAIdentity) certificateIssuer {
			return sshx.NewCertificateIssuer(ca, sshx.NewExecRunner(), sshx.OSIdentity{}, time.Now)
		},
		client:       sshx.NewClient(sshx.NewExecRunner()),
		importer:     sshx.NewSFTPClient(),
		zone:         timezonex.DetectHost,
		now:          time.Now,
		newNonce:     sshx.RandomUUID,
		pollInterval: 100 * time.Millisecond, startupTimeout: 30 * time.Second, renewInterval: time.Minute,
	}}
}

func (o *Owner) Start(ctx context.Context, request supervisor.LaunchRequest) (result error) {
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
	var rebuild *session.RebuildJournal
	journal, journalErr := session.LoadRebuildJournal(selected.StateRoot, selected.ID, string(record.Name))
	if journalErr == nil {
		if journal.Phase != session.RebuildCutover || journal.SessionID != record.ID || journal.CandidateBackend != record.Backend.ObjectID ||
			journal.CandidateRevision != record.GoldenRevision || journal.Domain != record.Domain {
			return fmt.Errorf("starting session does not match exact rebuild cutover journal")
		}
		rebuild = &journal
	} else if !errors.Is(journalErr, os.ErrNotExist) {
		return fmt.Errorf("inspect exact rebuild journal before launch: %w", journalErr)
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
	if rebuild != nil {
		if _, ok := pins.(rebuildPinStore); !ok {
			return fmt.Errorf("rebuild pin transition is unavailable before candidate launch")
		}
	}
	observer := o.deps.observer(admission.Host.TartExecutable, admission.Host.TartHome)
	if observer == nil {
		return fmt.Errorf("qualified backend observer is required")
	}
	if rebuild != nil {
		oldObservation, oldErr := observeExact(ctx, observer, rebuild.OldBackend)
		if oldErr != nil || !oldObservation.Exists || oldObservation.State != backend.ObjectStopped {
			return fmt.Errorf("old rebuild system must remain exactly stopped before candidate launch: %v", oldErr)
		}
	}
	observation, err := observeExact(ctx, observer, binding.BackendObject)
	if err != nil {
		return err
	}
	if observation.State != backend.ObjectStopped {
		return fmt.Errorf("exact backend must be stopped before launch")
	}
	managedDisks, err := workspacex.AdmitLaunchDisks(ctx, selected.StateRoot, selected.ID, record, observer)
	if err != nil {
		return fmt.Errorf("admit exact workspace disks: %w", err)
	}
	defer func() { result = errors.Join(result, managedDisks.CloseUnclaimed()) }()
	attachments, err := workspacex.ListSessionAttachments(ctx, selected.StateRoot, selected.ID, record.ID, string(record.Name))
	if err != nil {
		return fmt.Errorf("capture exact workspace mount bindings: %w", err)
	}
	if (managedDisks == nil) != (len(attachments) == 0) {
		return fmt.Errorf("workspace disk leases do not match attachments")
	}
	mounts := make([]sshx.WorkspaceMount, 0, len(attachments))
	wantUse := workspacex.Use{BackendKind: record.Backend.Kind, BackendObject: record.Backend.ObjectID, Generation: record.StartGeneration}
	for _, attached := range attachments {
		if attached.Attachment == nil || attached.Use == nil || *attached.Use != wantUse || attached.State != workspacex.StateAvailable {
			return fmt.Errorf("workspace attachment lacks exact launch use")
		}
		mounts = append(mounts, sshx.WorkspaceMount{VolumeID: attached.VolumeID, FilesystemUUID: attached.FilesystemUUID, MountPath: attached.Attachment.MountPath})
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
	handle, err := launcher.Start(ctx, backend.StartRequest{ObjectID: record.Backend.ObjectID, SerialDevice: serial.TartSlave(), GenerationDirectory: directory, ManagedDisks: managedDisks})
	if handle == nil {
		if err == nil {
			err = fmt.Errorf("Tart returned no retained handle")
		}
		return errors.Join(err, serial.Close())
	}
	sshBinding := sshx.Binding{Domain: record.Domain, SessionID: record.ID, BackendKind: record.Backend.Kind, BackendObject: record.Backend.ObjectID}
	o.mu.Lock()
	o.binding, o.sshBinding, o.ca, o.observer, o.handle, o.serial, o.pins = binding, sshBinding, ca, observer, handle, serial, pins
	o.rebuildJournal, o.stateRoot, o.sessionName = rebuild, selected.StateRoot, string(record.Name)
	o.workspaceMounts = mounts
	o.runtimePath, o.tartPath, o.tartHome = directory, admission.Host.TartExecutable, admission.Host.TartHome
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
	rebuild, rebuildRoot := o.rebuildJournal, o.stateRoot
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
	observedKey := sshx.ObservedHostKey{Algorithm: "ssh-ed25519", PublicKey: result.HostPublicKey}
	var pin sshx.HostKeyPin
	var err error
	if rebuild == nil {
		pin, err = pins.Admit(ctx, sshBinding, observedKey)
	} else {
		current, loadErr := session.LoadRebuildJournal(rebuildRoot, rebuild.Domain, rebuild.SessionName)
		if loadErr != nil || current != *rebuild || current.Phase != session.RebuildCutover {
			return fmt.Errorf("exact rebuild journal changed before host-key transition: %v", loadErr)
		}
		transition, ok := pins.(rebuildPinStore)
		if !ok {
			return fmt.Errorf("rebuild pin transition is unavailable")
		}
		oldBinding := sshx.Binding{Domain: rebuild.Domain, SessionID: rebuild.SessionID, BackendKind: "tart", BackendObject: rebuild.OldBackend}
		pin, err = transition.TransitionRebuild(ctx, rebuild.OperationID, oldBinding, sshBinding, rebuild.OldPinPresent, rebuild.OldPinDigest, observedKey)
	}
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

// Ready establishes only the current generation's management channel. Every
// candidate fact is bound to the serial-observed durable pin and the retained
// owner; a failed phase publishes no READY evidence.
func (o *Owner) Ready(ctx context.Context) error {
	o.readyMu.Lock()
	defer o.readyMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if o.deps.address == nil || o.deps.key == nil || o.deps.issuer == nil || o.deps.client == nil || o.deps.zone == nil || o.deps.now == nil {
		return fmt.Errorf("management readiness dependencies are required")
	}
	pre := o.Snapshot(ctx)
	if !pre.BackendRunning || !pre.SerialHealthy || !pre.PinPresent {
		return fmt.Errorf("exact bootstrapped runtime is required before management readiness")
	}
	o.mu.Lock()
	o.readyAttempted = true
	servo, ca, binding, pin, directory, tartPath, tartHome := o.binding, o.ca, o.sshBinding, o.expectedPin, o.runtimePath, o.tartPath, o.tartHome
	mounts := append([]sshx.WorkspaceMount(nil), o.workspaceMounts...)
	o.mu.Unlock()
	key, err := o.deps.key(ctx, directory)
	if err != nil {
		return fmt.Errorf("create exact generation client identity: %w", err)
	}
	knownHosts, err := sshx.WriteKnownHosts(directory, pin)
	if err != nil {
		return fmt.Errorf("materialize exact host-key pin: %w", err)
	}
	issuer := o.deps.issuer(ca)
	if issuer == nil {
		return fmt.Errorf("management certificate issuer is unavailable")
	}
	certificate, err := issuer.Issue(ctx, binding, directory, key)
	if err != nil {
		return fmt.Errorf("issue exact management certificate: %w", err)
	}
	if certificate.Path != key+"-cert.pub" || certificate.Identity != binding.CertificateIdentity() || certificate.Principal != binding.Principal() || sshx.RenewalRequired(certificate, o.deps.now()) {
		return fmt.Errorf("management certificate is not current for exact binding")
	}
	addressResolver := o.deps.address(tartPath, tartHome)
	if addressResolver == nil {
		return fmt.Errorf("qualified address resolver is unavailable")
	}
	address, err := addressResolver.Resolve(ctx, servo.BackendObject)
	if err != nil {
		return fmt.Errorf("resolve exact management address: %w", err)
	}
	if _, err := netip.ParseAddr(address); err != nil {
		return fmt.Errorf("management address is not a literal IP: %w", err)
	}
	connection := sshx.Connection{Address: address, Port: 22, Binding: binding, Pin: pin, RuntimeDirectory: directory, IdentityFile: key, CertificateFile: certificate.Path, KnownHostsFile: knownHosts}
	probe, err := o.deps.client.Probe(ctx, connection, sshx.ProbeRequest{})
	if err != nil || !probe.OK {
		return fmt.Errorf("strict management SSH probe failed: %w", errOrProbe(err))
	}
	zone, err := o.deps.zone()
	if err != nil {
		return fmt.Errorf("detect trusted host time zone: %w", err)
	}
	if err := timezonex.Converge(ctx, o.deps.client, connection, zone); err != nil {
		return fmt.Errorf("converge guest time zone: %w", err)
	}
	if len(mounts) > 0 {
		if err := o.deps.client.EnsureWorkspaces(ctx, connection, mounts); err != nil {
			return fmt.Errorf("mount exact guest workspaces: %w", err)
		}
		probe, err := o.deps.client.Probe(ctx, connection, sshx.ProbeRequest{Workspaces: mounts})
		if err != nil || !probe.OK {
			return fmt.Errorf("mount-bound management SSH probe failed: %w", errOrProbe(err))
		}
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.active || o.binding != servo || o.expectedPin != pin {
		return fmt.Errorf("exact runtime changed during readiness convergence")
	}
	o.connection, o.certificate, o.readyEstablished = connection, certificate, true
	if o.maintenanceCancel == nil && o.deps.renewInterval > 0 {
		maintenanceCtx, cancel := context.WithCancel(context.Background())
		o.maintenanceCancel = cancel
		o.maintenanceDone = make(chan struct{})
		go o.maintainCertificate(maintenanceCtx, o.maintenanceDone)
	}
	return nil
}

func (o *Owner) maintainCertificate(ctx context.Context, done chan struct{}) {
	defer close(done)
	ticker := time.NewTicker(o.deps.renewInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		o.mu.Lock()
		active, certificate := o.active, o.certificate
		o.mu.Unlock()
		if !active {
			return
		}
		if !sshx.RenewalRequired(certificate, o.deps.now()) {
			continue
		}
		attemptCtx, cancel := context.WithTimeout(ctx, 4*time.Minute)
		_ = o.Ready(attemptCtx)
		cancel()
	}
}

// InspectPackages uses only the current pinned management connection retained
// by this owner. Both observations must still prove READY for this generation.
func (o *Owner) InspectPackages(ctx context.Context, names []string) ([]supervisor.PackageVersion, error) {
	if o == nil || o.deps.client == nil {
		return nil, fmt.Errorf("runtime package inspector is unavailable")
	}
	o.readyMu.Lock()
	defer o.readyMu.Unlock()
	before := o.Snapshot(ctx)
	if !before.BackendRunning || !before.SerialHealthy || !before.PinPresent || !before.CertificateCurrent || !before.ProbeOK || !before.ZoneMatches {
		return nil, fmt.Errorf("exact runtime is not ready for package inspection")
	}
	o.mu.Lock()
	connection, binding, sshBinding, runtimePath := o.connection, o.binding, o.sshBinding, o.runtimePath
	o.mu.Unlock()
	if before.Binding != binding || connection.Binding != sshBinding || connection.Binding.SessionID != binding.SessionID || connection.Binding.BackendObject != binding.BackendObject || connection.RuntimeDirectory != runtimePath {
		return nil, fmt.Errorf("management connection no longer matches exact runtime")
	}
	observed, err := o.deps.client.InspectPackages(ctx, connection, names)
	if err != nil {
		return nil, err
	}
	after := o.Snapshot(ctx)
	if after.Binding != binding || !after.BackendRunning || !after.SerialHealthy || !after.PinPresent || !after.CertificateCurrent || !after.ProbeOK || !after.ZoneMatches {
		return nil, fmt.Errorf("exact runtime changed during package inspection")
	}
	result := make([]supervisor.PackageVersion, 0, len(observed))
	for _, pkg := range observed {
		result = append(result, supervisor.PackageVersion{Name: pkg.Name, Version: pkg.Version})
	}
	return result, nil
}

// InspectIdentity uses the retained pinned SSH connection for one fixed
// read-only clone identity query. It is valid only during current READY.
func (o *Owner) InspectIdentity(ctx context.Context) (supervisor.GuestIdentity, error) {
	if o == nil || o.deps.client == nil {
		return supervisor.GuestIdentity{}, fmt.Errorf("runtime identity inspector is unavailable")
	}
	o.readyMu.Lock()
	defer o.readyMu.Unlock()
	before := o.Snapshot(ctx)
	if !before.BackendRunning || !before.SerialHealthy || !before.PinPresent || !before.CertificateCurrent || !before.ProbeOK || !before.ZoneMatches {
		return supervisor.GuestIdentity{}, fmt.Errorf("exact runtime is not ready for identity inspection")
	}
	o.mu.Lock()
	connection, binding, sshBinding, runtimePath := o.connection, o.binding, o.sshBinding, o.runtimePath
	o.mu.Unlock()
	if before.Binding != binding || connection.Binding != sshBinding || connection.Binding.SessionID != binding.SessionID || connection.Binding.BackendObject != binding.BackendObject || connection.RuntimeDirectory != runtimePath {
		return supervisor.GuestIdentity{}, fmt.Errorf("management connection no longer matches exact runtime")
	}
	observed, err := o.deps.client.InspectIdentity(ctx, connection)
	if err != nil {
		return supervisor.GuestIdentity{}, err
	}
	after := o.Snapshot(ctx)
	if after.Binding != binding || !after.BackendRunning || !after.SerialHealthy || !after.PinPresent || !after.CertificateCurrent || !after.ProbeOK || !after.ZoneMatches {
		return supervisor.GuestIdentity{}, fmt.Errorf("exact runtime changed during identity inspection")
	}
	return supervisor.GuestIdentity{MachineID: observed.MachineID, Hostname: observed.Hostname}, nil
}

func errOrProbe(err error) error {
	if err != nil {
		return err
	}
	return fmt.Errorf("guest rejected probe")
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

// Snapshot observes every READY predicate within the caller's bound. It does
// not renew credentials or change guest state; failed checks only demote this
// one observation, leaving recovery to the explicit Ready operation.
func (o *Owner) Snapshot(ctx context.Context) supervisor.Snapshot {
	o.observationMu.Lock()
	defer o.observationMu.Unlock()
	o.mu.Lock()
	snapshot := supervisor.Snapshot{Binding: o.binding, ObservedAt: time.Now()}
	active, observer, handle, serial, pins, binding, expectedPin, diagnostic := o.active, o.observer, o.handle, o.serial, o.pins, o.sshBinding, o.expectedPin, o.bootstrapDiagnostic
	connection, certificate, ready := o.connection, o.certificate, o.readyEstablished
	mounts := append([]sshx.WorkspaceMount(nil), o.workspaceMounts...)
	o.mu.Unlock()
	if !active {
		return snapshot
	}
	observation, err := observeExact(ctx, observer, snapshot.Binding.BackendObject)
	liveness, retained := handle.(backend.RetainedChildLiveness)
	childLive := retained && liveness.RetainedChildLive()
	o.mu.Lock()
	snapshot.ObservedAt = time.Now()
	snapshot.BackendRunning = o.active && err == nil && childLive
	snapshot.SerialHealthy = o.active && serial.Err() == nil
	o.mu.Unlock()
	if snapshot.BackendRunning && snapshot.SerialHealthy && expectedPin.Version != 0 {
		pin, pinErr := pins.Load(ctx, binding)
		snapshot.PinPresent = pinErr == nil && pin == expectedPin
		if !snapshot.PinPresent {
			diagnostic = "exact host-key pin verification failed"
		}
	}
	if err != nil {
		snapshot.Diagnostic = "exact backend observation failed"
	} else if !childLive {
		snapshot.Diagnostic = "exact retained Tart child lifetime is unavailable"
	} else {
		snapshot.Diagnostic = diagnostic
	}
	if snapshot.BackendRunning && snapshot.SerialHealthy && snapshot.PinPresent && ready {
		now := o.deps.now()
		snapshot.CertificateCurrent = certificate.Path == connection.CertificateFile && certificate.Identity == binding.CertificateIdentity() && certificate.Principal == binding.Principal() && !sshx.RenewalRequired(certificate, now)
		if !snapshot.CertificateCurrent {
			snapshot.Diagnostic = "management certificate requires renewal"
		} else if connection.Binding != binding || connection.Pin != expectedPin || connection.RuntimeDirectory != o.runtimePath {
			snapshot.Diagnostic = "management connection binding changed"
		} else {
			probe, probeErr := o.deps.client.Probe(ctx, connection, sshx.ProbeRequest{Workspaces: mounts})
			snapshot.ProbeOK = probeErr == nil && probe.OK
			if !snapshot.ProbeOK {
				snapshot.Diagnostic = "strict management SSH probe failed"
			} else if zone, zoneErr := o.deps.zone(); zoneErr != nil {
				snapshot.Diagnostic = "trusted host time zone detection failed"
			} else if guestZone, readErr := o.deps.client.ReadZone(ctx, connection, sshx.ReadZoneRequest{}); readErr != nil || guestZone != zone || !timezonex.Valid(guestZone) {
				snapshot.Diagnostic = "guest time zone verification failed"
			} else {
				snapshot.ZoneMatches = true
			}
		}
	}
	if observation.State == backend.ObjectStopped && snapshot.BackendRunning && snapshot.SerialHealthy && snapshot.PinPresent && snapshot.CertificateCurrent && snapshot.ProbeOK && snapshot.ZoneMatches && snapshot.Diagnostic == "" {
		snapshot.Diagnostic = "Tart listing reports stopped; exact retained child and guest checks were observed"
	}
	o.mu.Lock()
	if !o.active || o.binding != snapshot.Binding || o.expectedPin != expectedPin || !retained || !liveness.RetainedChildLive() {
		snapshot.BackendRunning, snapshot.SerialHealthy, snapshot.PinPresent = false, false, false
		snapshot.CertificateCurrent, snapshot.ProbeOK, snapshot.ZoneMatches = false, false, false
		snapshot.Diagnostic = "exact runtime changed during observation"
	}
	o.mu.Unlock()
	snapshot.ObservedAt = time.Now()
	return snapshot
}

// RequestStop asks the retained backend handle to let the guest OS shut down.
// The supervisor observes actual reap and sends a bounded force stop if the
// guest does not cooperate; this request grants no guest authority over state.
func (o *Owner) RequestStop(ctx context.Context) error {
	o.mu.Lock()
	handle := o.handle
	cancelMaintenance := o.maintenanceCancel
	o.mu.Unlock()
	if handle == nil {
		return fmt.Errorf("runtime handle is unavailable")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if cancelMaintenance != nil {
		cancelMaintenance()
	}
	o.stopMu.Lock()
	defer o.stopMu.Unlock()
	if o.graceSent || o.stopSent {
		return nil
	}
	requester, ok := handle.(interface{ RequestStop(context.Context) error })
	if !ok {
		return fmt.Errorf("backend handle cannot request guest shutdown")
	}
	if err := requester.RequestStop(ctx); err != nil {
		return err
	}
	o.graceSent = true
	return nil
}

func (o *Owner) Stop(ctx context.Context) error {
	o.mu.Lock()
	handle := o.handle
	cancelMaintenance := o.maintenanceCancel
	o.mu.Unlock()
	if handle == nil {
		return fmt.Errorf("runtime handle is unavailable")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if cancelMaintenance != nil {
		cancelMaintenance()
	}
	o.stopMu.Lock()
	defer o.stopMu.Unlock()
	if o.stopSent {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := handle.Stop(ctx); err != nil {
		return err
	}
	o.stopSent = true
	return nil
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
		if errors.Is(err, tart.ErrScratchCleanupUnproven) || errors.Is(err, tart.ErrReapUnproven) {
			err = fmt.Errorf("%w: %w", supervisor.ErrRuntimeCleanupUnproven, err)
		}
		o.readyMu.Lock()
		o.mu.Lock()
		o.active = false
		cancel, done := o.maintenanceCancel, o.maintenanceDone
		o.mu.Unlock()
		o.readyMu.Unlock()
		if cancel != nil {
			cancel()
			<-done
		}
		o.readyMu.Lock()
		o.mu.Lock()
		directory := o.runtimePath
		attempted := o.readyAttempted
		o.mu.Unlock()
		var credentialErr error
		if attempted {
			credentialErr = sshx.CleanupGenerationCredentials(directory)
		}
		o.readyMu.Unlock()
		if credentialErr != nil {
			credentialErr = fmt.Errorf("%w: clean exact generation credentials: %w", supervisor.ErrRuntimeCleanupUnproven, credentialErr)
		}
		o.waitErr = errors.Join(err, credentialErr, serial.Close())
	})
	return o.waitErr
}

var _ supervisor.RuntimeOwner = (*Owner)(nil)

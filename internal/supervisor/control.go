package supervisor

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

// controlIOTimeout caps snapshot RPC expiry as well as bounded request/response
// I/O; the client sends its possibly earlier operation deadline on the wire.
const controlIOTimeout = 2 * time.Second
const lifecycleTimeout = 5 * time.Second

// Includes serialx's 3m login wait, 30s exchange, and one bounded backend
// observation for the response snapshot.
const bootstrapTimeout = 4 * time.Minute
const readyTimeout = 5 * time.Minute
const inspectTimeout = 90 * time.Second

type controlRequest struct {
	Version   int       `json:"version"`
	Action    string    `json:"action"`
	Binding   Binding   `json:"binding"`
	ExpiresAt time.Time `json:"expires_at"`
	Packages  []string  `json:"packages,omitempty"`
}
type controlResponse struct {
	Version  int              `json:"version"`
	Binding  Binding          `json:"binding"`
	Snapshot Snapshot         `json:"snapshot"`
	Error    string           `json:"error"`
	Packages []PackageVersion `json:"packages,omitempty"`
	Identity *GuestIdentity   `json:"identity,omitempty"`
}

// PackageInspector is an optional read-only capability of a live runtime
// owner. The ordinary lifecycle owner interface carries no generic exec API.
type PackageInspector interface {
	InspectPackages(context.Context, []string) ([]PackageVersion, error)
}
type IdentityInspector interface {
	InspectIdentity(context.Context) (GuestIdentity, error)
}

func validControlAction(request controlRequest) bool {
	switch request.Action {
	case "snapshot", "bootstrap", "ready", "stop", "inspect_identity":
		return len(request.Packages) == 0
	case "inspect_packages":
		if len(request.Packages) == 0 || len(request.Packages) > 32 {
			return false
		}
		seen := make(map[string]bool, len(request.Packages))
		for _, name := range request.Packages {
			if !validControlPackageName(name) || seen[name] {
				return false
			}
			seen[name] = true
		}
		return true
	default:
		return false
	}
}

func validControlPackageName(name string) bool {
	if len(name) == 0 || len(name) > 128 || name[0] < 'a' || name[0] > 'z' {
		return false
	}
	for _, c := range name[1:] {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '+' || c == '.' || c == '-') {
			return false
		}
	}
	return true
}

func sameControlPackages(names []string, packages []PackageVersion) bool {
	if len(names) != len(packages) {
		return false
	}
	for i, pkg := range packages {
		if pkg.Name != names[i] || len(pkg.Version) == 0 || len(pkg.Version) > 128 {
			return false
		}
		for _, c := range pkg.Version {
			if c < 0x21 || c > 0x7e {
				return false
			}
		}
	}
	return true
}

func validControlIdentity(identity *GuestIdentity) bool {
	if identity == nil || len(identity.MachineID) != 32 || identity.MachineID == strings.Repeat("0", 32) {
		return false
	}
	for _, c := range identity.MachineID {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return identity.Hostname == "boxwarden-"+identity.MachineID[:12]
}

func listenSocket(path string) (*controlListener, error) {
	if filepath.Base(path) != socketName || !privateDirectory(filepath.Dir(path)) {
		return nil, fmt.Errorf("unsafe control socket path")
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		return nil, fmt.Errorf("control socket already exists")
	}
	address, cleanup, err := socketAddress(path)
	if err != nil {
		return nil, err
	}
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: address, Net: "unix"})
	if err != nil {
		return nil, errors.Join(err, cleanup())
	}
	listener.SetUnlinkOnClose(false)
	info, statErr := os.Lstat(path)
	if statErr != nil {
		return nil, errors.Join(statErr, listener.Close(), cleanup())
	}
	owned := &controlListener{UnixListener: listener, path: path, info: info}
	if err := errors.Join(os.Chmod(path, 0600), cleanup()); err != nil {
		return nil, errors.Join(err, owned.Close())
	}
	return owned, nil
}
func serveControl(ctx context.Context, listener *controlListener, binding Binding, owner RuntimeOwner, stop func() error) error {
	for {
		connection, err := listener.AcceptUnix()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		// One bounded connection at a time; no unbounded handler pool.
		handleControl(ctx, connection, binding, owner, stop)
	}
}
func handleControl(ctx context.Context, connection net.Conn, binding Binding, owner RuntimeOwner, stop func() error) {
	defer connection.Close()
	acceptedAt := time.Now()
	serverDeadline := acceptedAt.Add(controlIOTimeout)
	if err := connection.SetDeadline(serverDeadline); err != nil {
		return
	}
	data, err := readBounded(connection)
	if err != nil {
		return
	}
	var request controlRequest
	if err := decodeExact(data, &request); err != nil {
		return
	}
	if request.Version != 1 || request.Binding != binding || !validControlAction(request) {
		return
	}
	if request.Action == "bootstrap" {
		serverDeadline = acceptedAt.Add(bootstrapTimeout)
	} else if request.Action == "ready" {
		serverDeadline = acceptedAt.Add(readyTimeout)
	} else if request.Action == "stop" {
		serverDeadline = acceptedAt.Add(lifecycleTimeout + controlIOTimeout)
	} else if request.Action == "inspect_packages" || request.Action == "inspect_identity" {
		serverDeadline = acceptedAt.Add(inspectTimeout)
	}
	now := time.Now()
	if request.ExpiresAt.IsZero() || !request.ExpiresAt.After(now) || request.ExpiresAt.After(serverDeadline) {
		return
	}
	effectiveDeadline := request.ExpiresAt
	if deadline, ok := ctx.Deadline(); ok && deadline.Before(effectiveDeadline) {
		effectiveDeadline = deadline
	}
	if !effectiveDeadline.After(now) {
		return
	}
	if err := connection.SetDeadline(effectiveDeadline); err != nil {
		return
	}
	response := controlResponse{Version: 1, Binding: binding}
	operationCtx, cancelOperation := context.WithDeadline(ctx, effectiveDeadline)
	defer cancelOperation()
	if request.Action == "bootstrap" {
		if err := owner.Bootstrap(operationCtx); err != nil {
			response.Error = err.Error()
		}
	} else if request.Action == "ready" {
		if err := owner.Ready(operationCtx); err != nil {
			response.Error = err.Error()
		}
	} else if request.Action == "stop" {
		if err := stop(); err != nil {
			response.Error = err.Error()
		}
	} else if request.Action == "inspect_packages" {
		before := owner.Snapshot(operationCtx)
		if before.Binding != binding || !snapshotReady(before) {
			response.Error = "exact runtime is not ready for package inspection"
		} else if inspector, ok := owner.(PackageInspector); !ok {
			response.Error = "package inspector is unavailable"
		} else {
			response.Packages, err = inspector.InspectPackages(operationCtx, request.Packages)
			if err != nil {
				response.Error = "package inspection failed"
			}
		}
	} else if request.Action == "inspect_identity" {
		before := owner.Snapshot(operationCtx)
		if before.Binding != binding || !snapshotReady(before) {
			response.Error = "exact runtime is not ready for identity inspection"
		} else if inspector, ok := owner.(IdentityInspector); !ok {
			response.Error = "identity inspector is unavailable"
		} else {
			identity, inspectErr := inspector.InspectIdentity(operationCtx)
			if inspectErr != nil {
				response.Error = "identity inspection failed"
			} else {
				response.Identity = &identity
			}
		}
	}
	response.Snapshot = owner.Snapshot(operationCtx)
	exactAfterBinding := response.Snapshot.Binding == binding
	response.Snapshot.Binding = binding
	response.Snapshot.ObservedAt = time.Now().UTC()
	if request.Action == "inspect_packages" && (!exactAfterBinding || !snapshotReady(response.Snapshot) || !sameControlPackages(request.Packages, response.Packages)) {
		response.Packages = nil
		if response.Error == "" {
			response.Error = "package inspection result or readiness changed"
		}
	}
	if request.Action == "inspect_identity" && (!exactAfterBinding || !snapshotReady(response.Snapshot) || !validControlIdentity(response.Identity)) {
		response.Identity = nil
		if response.Error == "" {
			response.Error = "identity inspection result or readiness changed"
		}
	}
	data, err = encodeControlResponse(response)
	if err != nil {
		return
	}
	_ = writeFrame(connection, data)
}

// Measure the complete encoded response: JSON escaping can expand each
// diagnostic byte sixfold. Reduce both diagnostic prefixes until the actual
// frame fits, preserving the exact binding and a valid UTF-8 boundary.
func encodeControlResponse(response controlResponse) ([]byte, error) {
	for limit := maxDiagnosticBytes; ; limit /= 2 {
		response.Error = truncateDiagnostic(response.Error, limit)
		response.Snapshot.Diagnostic = truncateDiagnostic(response.Snapshot.Diagnostic, limit)
		data, err := json.Marshal(response)
		if err != nil || len(data) <= maxControlBytes {
			return data, err
		}
		if limit == 0 {
			return nil, fmt.Errorf("control response binding exceeds bound")
		}
	}
}
func readBounded(reader io.Reader) ([]byte, error) {
	var size uint32
	if err := binary.Read(reader, binary.BigEndian, &size); err != nil {
		return nil, err
	}
	if size == 0 || size > maxControlBytes {
		return nil, fmt.Errorf("control message exceeds bound")
	}
	data := make([]byte, size)
	_, err := io.ReadFull(reader, data)
	return data, err
}
func writeFrame(writer io.Writer, data []byte) error {
	if len(data) == 0 || len(data) > maxControlBytes {
		return fmt.Errorf("control message exceeds bound")
	}
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(data)))
	for _, part := range [][]byte{header[:], data} {
		for len(part) > 0 {
			n, err := writer.Write(part)
			if err != nil {
				return err
			}
			if n == 0 {
				return io.ErrShortWrite
			}
			part = part[n:]
		}
	}
	return nil
}
func boundedDiagnostic(s string) string {
	return truncateDiagnostic(s, maxDiagnosticBytes)
}
func truncateDiagnostic(s string, limit int) string {
	var prefix strings.Builder
	for _, r := range s {
		if prefix.Len()+utf8.RuneLen(r) > limit {
			break
		}
		prefix.WriteRune(r)
	}
	return prefix.String()
}

// Client trusts cooperating host processes, but admits only the private socket
// inside the exact structurally valid, currently owned generation namespace.
type Client struct {
	RuntimeDirectory string
	MaxSnapshotAge   time.Duration
	Now              func() time.Time
}

func (c *Client) Snapshot(ctx context.Context, binding Binding) (Snapshot, error) {
	response, err := c.call(ctx, binding, "snapshot")
	if err != nil {
		return Snapshot{}, err
	}
	if err := c.validateSnapshot(response.Snapshot); err != nil {
		return Snapshot{}, err
	}
	return response.Snapshot, nil
}
func (c *Client) validateSnapshot(snapshot Snapshot) error {
	now := time.Now
	if c.Now != nil {
		now = c.Now
	}
	return validateSnapshotFreshness(snapshot, now(), c.MaxSnapshotAge)
}
func (c *Client) Bootstrap(ctx context.Context, binding Binding) (Snapshot, error) {
	response, err := c.call(ctx, binding, "bootstrap")
	if err != nil {
		return response.Snapshot, err
	}
	if err := c.validateSnapshot(response.Snapshot); err != nil {
		return Snapshot{}, err
	}
	return response.Snapshot, nil
}
func (c *Client) Ready(ctx context.Context, binding Binding) (Snapshot, error) {
	response, err := c.call(ctx, binding, "ready")
	if err != nil {
		return response.Snapshot, err
	}
	if err := c.validateSnapshot(response.Snapshot); err != nil {
		return Snapshot{}, err
	}
	return response.Snapshot, nil
}
func validateSnapshotFreshness(s Snapshot, now time.Time, maxAge time.Duration) error {
	if s.ObservedAt.IsZero() || s.ObservedAt.After(now) || maxAge > 0 && now.Sub(s.ObservedAt) > maxAge {
		return fmt.Errorf("supervisor snapshot is stale")
	}
	return nil
}
func (c *Client) Stop(ctx context.Context, binding Binding) error {
	_, err := c.call(ctx, binding, "stop")
	return err
}
func (c *Client) InspectPackages(ctx context.Context, binding Binding, names []string) ([]PackageVersion, error) {
	request := controlRequest{Action: "inspect_packages", Packages: append([]string(nil), names...)}
	if !validControlAction(request) {
		return nil, fmt.Errorf("invalid package inspection request")
	}
	response, err := c.callWithPackages(ctx, binding, request.Action, request.Packages)
	if err != nil {
		return nil, err
	}
	if err := c.validateSnapshot(response.Snapshot); err != nil {
		return nil, err
	}
	if !snapshotReady(response.Snapshot) || !sameControlPackages(names, response.Packages) {
		return nil, fmt.Errorf("exact package inspection result is not ready or matching")
	}
	return response.Packages, nil
}
func (c *Client) InspectIdentity(ctx context.Context, binding Binding) (GuestIdentity, error) {
	response, err := c.call(ctx, binding, "inspect_identity")
	if err != nil {
		return GuestIdentity{}, err
	}
	if err := c.validateSnapshot(response.Snapshot); err != nil {
		return GuestIdentity{}, err
	}
	if !snapshotReady(response.Snapshot) || !validControlIdentity(response.Identity) {
		return GuestIdentity{}, fmt.Errorf("exact identity inspection result is not ready or valid")
	}
	return *response.Identity, nil
}
func (c *Client) call(ctx context.Context, binding Binding, action string) (controlResponse, error) {
	return c.callWithPackages(ctx, binding, action, nil)
}
func (c *Client) callWithPackages(ctx context.Context, binding Binding, action string, packages []string) (controlResponse, error) {
	var response controlResponse
	if err := ctx.Err(); err != nil {
		return response, err
	}
	if c == nil || !binding.valid() {
		return response, fmt.Errorf("invalid control binding")
	}
	request, err := readLaunchRequest(filepath.Join(c.RuntimeDirectory, requestName))
	if err != nil {
		return response, err
	}
	if request.Binding != binding {
		return response, fmt.Errorf("supervisor binding mismatch")
	}
	// Before the socket exists, a read-only poll must not briefly acquire the
	// lock and make the starting child's nonblocking ownership claim lose.
	if err := validateGenerationEntry(c.RuntimeDirectory, socketName); err != nil {
		return response, err
	}
	state, err := classifyExactGeneration(request)
	if err != nil {
		return response, err
	}
	if state != exactGenerationLive {
		return response, fmt.Errorf("supervisor generation has no live owner")
	}
	timeout := controlIOTimeout
	if action == "bootstrap" {
		timeout = bootstrapTimeout
	} else if action == "ready" {
		timeout = readyTimeout
	} else if action == "stop" {
		timeout += lifecycleTimeout
	} else if action == "inspect_packages" || action == "inspect_identity" {
		timeout = inspectTimeout
	}
	operationCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	connection, err := dialControl(operationCtx, filepath.Join(c.RuntimeDirectory, socketName))
	if err != nil {
		return response, err
	}
	defer connection.Close()
	deadline, ok := operationCtx.Deadline()
	if !ok {
		return response, fmt.Errorf("control operation has no deadline")
	}
	if err := connection.SetDeadline(deadline); err != nil {
		return response, err
	}
	cancelIO := context.AfterFunc(operationCtx, func() { connection.SetDeadline(time.Now()) })
	defer cancelIO()
	data, err := json.Marshal(controlRequest{Version: 1, Action: action, Binding: binding, ExpiresAt: deadline.UTC(), Packages: packages})
	if err != nil {
		return response, err
	}
	if err := writeFrame(connection, data); err != nil {
		return response, errors.Join(operationCtx.Err(), err)
	}
	data, err = readBounded(connection)
	if err != nil {
		return response, errors.Join(operationCtx.Err(), err)
	}
	if err := decodeExact(data, &response); err != nil {
		return response, err
	}
	if response.Version != 1 || response.Binding != binding || response.Snapshot.Binding != binding {
		return response, fmt.Errorf("control response binding mismatch")
	}
	if len(response.Snapshot.Diagnostic) > maxDiagnosticBytes || len(response.Error) > maxDiagnosticBytes {
		return response, fmt.Errorf("control response diagnostic exceeds bound")
	}
	if response.Error != "" {
		return response, fmt.Errorf("supervisor %s: %s", action, response.Error)
	}
	return response, nil
}

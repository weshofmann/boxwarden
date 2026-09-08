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

type controlRequest struct {
	Version   int       `json:"version"`
	Action    string    `json:"action"`
	Binding   Binding   `json:"binding"`
	ExpiresAt time.Time `json:"expires_at"`
}
type controlResponse struct {
	Version  int      `json:"version"`
	Binding  Binding  `json:"binding"`
	Snapshot Snapshot `json:"snapshot"`
	Error    string   `json:"error"`
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
	if request.Version != 1 || request.Binding != binding || (request.Action != "snapshot" && request.Action != "stop") {
		return
	}
	if request.Action == "stop" {
		serverDeadline = acceptedAt.Add(lifecycleTimeout + controlIOTimeout)
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
	if request.Action == "stop" {
		if err := stop(); err != nil {
			response.Error = err.Error()
		}
	}
	snapshotCtx, cancelSnapshot := context.WithDeadline(ctx, effectiveDeadline)
	response.Snapshot = owner.Snapshot(snapshotCtx)
	cancelSnapshot()
	response.Snapshot.Binding = binding
	response.Snapshot.ObservedAt = time.Now().UTC()
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
	now := time.Now
	if c.Now != nil {
		now = c.Now
	}
	if err := validateSnapshotFreshness(response.Snapshot, now(), c.MaxSnapshotAge); err != nil {
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
func (c *Client) call(ctx context.Context, binding Binding, action string) (controlResponse, error) {
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
	if action == "stop" {
		timeout += lifecycleTimeout
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
	data, err := json.Marshal(controlRequest{Version: 1, Action: action, Binding: binding, ExpiresAt: deadline.UTC()})
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
		return response, fmt.Errorf("supervisor stop: %s", response.Error)
	}
	return response, nil
}

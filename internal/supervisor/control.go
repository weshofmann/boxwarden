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
	"time"
)

const controlIOTimeout = 2 * time.Second
const lifecycleTimeout = 5 * time.Second

type controlRequest struct {
	Version int     `json:"version"`
	Action  string  `json:"action"`
	Binding Binding `json:"binding"`
}
type controlResponse struct {
	Version  int      `json:"version"`
	Binding  Binding  `json:"binding"`
	Snapshot Snapshot `json:"snapshot"`
	Error    string   `json:"error"`
}

func listenSocket(path string) (*net.UnixListener, error) {
	if filepath.Base(path) != socketName || !privateDirectory(filepath.Dir(path)) {
		return nil, fmt.Errorf("unsafe control socket path")
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		return nil, fmt.Errorf("control socket already exists")
	}
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0600); err != nil {
		listener.Close()
		return nil, err
	}
	return listener, nil
}
func serveControl(ctx context.Context, listener *net.UnixListener, binding Binding, owner RuntimeOwner, stop func() error) error {
	for {
		connection, err := listener.AcceptUnix()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		// One bounded connection at a time; no unbounded handler pool.
		handleControl(connection, binding, owner, stop)
	}
}
func handleControl(connection net.Conn, binding Binding, owner RuntimeOwner, stop func() error) {
	defer connection.Close()
	if err := connection.SetDeadline(time.Now().Add(controlIOTimeout)); err != nil {
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
	response := controlResponse{Version: 1, Binding: binding}
	if request.Action == "stop" {
		if err := connection.SetDeadline(time.Now().Add(lifecycleTimeout + controlIOTimeout)); err != nil {
			return
		}
		if err := stop(); err != nil {
			response.Error = boundedDiagnostic(err.Error())
		}
	}
	response.Snapshot = owner.Snapshot()
	response.Snapshot.Binding = binding
	response.Snapshot.ObservedAt = time.Now().UTC()
	response.Snapshot.Diagnostic = boundedDiagnostic(response.Snapshot.Diagnostic)
	data, err = json.Marshal(response)
	if err != nil {
		return
	}
	_ = writeFrame(connection, data)
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
	if len(s) > maxDiagnosticBytes {
		return s[:maxDiagnosticBytes]
	}
	return s
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
	connection, err := (&net.Dialer{}).DialContext(operationCtx, "unix", filepath.Join(c.RuntimeDirectory, socketName))
	if err != nil {
		return response, err
	}
	defer connection.Close()
	deadline, _ := operationCtx.Deadline()
	if err := connection.SetDeadline(deadline); err != nil {
		return response, err
	}
	cancelIO := context.AfterFunc(operationCtx, func() { connection.SetDeadline(time.Now()) })
	defer cancelIO()
	data, err := json.Marshal(controlRequest{Version: 1, Action: action, Binding: binding})
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

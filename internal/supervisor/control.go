package supervisor

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type controlRequest struct {
	Version   int     `json:"version"`
	Action    string  `json:"action"`
	Binding   Binding `json:"binding"`
	Challenge string  `json:"challenge"`
	MAC       string  `json:"mac"`
}
type controlResponse struct {
	Version   int      `json:"version"`
	Binding   Binding  `json:"binding"`
	Challenge string   `json:"challenge"`
	Snapshot  Snapshot `json:"snapshot"`
	MAC       string   `json:"mac"`
}

func requestMAC(key []byte, request controlRequest) (string, error) {
	request.MAC = ""
	data, err := json.Marshal(request)
	if err != nil {
		return "", err
	}
	return base64.RawStdEncoding.EncodeToString(mac(key, data)), nil
}
func responseMAC(key []byte, response controlResponse) (string, error) {
	response.MAC = ""
	data, err := json.Marshal(response)
	if err != nil {
		return "", err
	}
	return base64.RawStdEncoding.EncodeToString(mac(key, data)), nil
}
func mac(key, data []byte) []byte {
	sum := hmac.New(sha256.New, key)
	_, _ = sum.Write(data)
	return sum.Sum(nil)
}

func listenSocket(path string) (*net.UnixListener, error) {
	if filepath.Base(path) != socketName || !privateDirectory(filepath.Dir(path)) {
		return nil, fmt.Errorf("unsafe control socket path")
	}
	if _, err := os.Lstat(path); err == nil {
		return nil, fmt.Errorf("control socket already exists")
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		listener.Close()
		return nil, err
	}
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSocket == 0 || info.Mode().Perm() != 0o600 || !ownedByCurrentUser(info) {
		listener.Close()
		return nil, fmt.Errorf("control socket is not owner-private")
	}
	return listener, nil
}
func serveControl(ctx context.Context, listener *net.UnixListener, manifest Manifest, key []byte, owner RuntimeOwner) error {
	defer listener.Close()
	for {
		listener.SetDeadline(time.Now().Add(100 * time.Millisecond))
		connection, err := listener.AcceptUnix()
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				if ctx.Err() != nil {
					return nil
				}
				continue
			}
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		go handleControl(connection, manifest, key, owner)
	}
}
func handleControl(connection *net.UnixConn, manifest Manifest, key []byte, owner RuntimeOwner) {
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(2 * time.Second))
	request, err := readControlRequest(connection)
	if err != nil {
		return
	}
	if request.Version != 1 || (request.Action != "snapshot" && request.Action != "stop") || request.Challenge == "" || request.Binding != manifest.Binding {
		return
	}
	want, err := requestMAC(key, request)
	if err != nil || !hmac.Equal([]byte(want), []byte(request.MAC)) {
		return
	}
	snapshot := owner.Snapshot()
	snapshot.Binding = manifest.Binding
	snapshot.ObservedAt = time.Now().UTC()
	snapshot.Diagnostic = boundedDiagnostic(snapshot.Diagnostic)
	if !snapshot.BrokerHealthy {
		snapshot.BackendRunning = false
		snapshot.PinPresent = false
		snapshot.CertificateCurrent = false
		snapshot.ProbeOK = false
		snapshot.ZoneMatches = false
	}
	response := controlResponse{Version: 1, Binding: manifest.Binding, Challenge: request.Challenge, Snapshot: snapshot}
	response.MAC, err = responseMAC(key, response)
	if err != nil {
		return
	}
	_ = writeControl(connection, response)
	if request.Action == "stop" {
		go owner.Stop(context.Background())
	}
}
func readControlRequest(connection *net.UnixConn) (controlRequest, error) {
	var request controlRequest
	data, err := readBounded(connection)
	if err != nil {
		return request, err
	}
	if err := decodeExact(data, &request); err != nil {
		return request, err
	}
	return request, nil
}
func writeControl(connection *net.UnixConn, value controlResponse) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if len(data) > maxControlBytes {
		return fmt.Errorf("control response exceeds bound")
	}
	return writeFrame(connection, data)
}
func readBounded(connection *net.UnixConn) ([]byte, error) {
	var size uint32
	if err := binary.Read(connection, binary.BigEndian, &size); err != nil {
		return nil, err
	}
	if size == 0 || size > maxControlBytes {
		return nil, fmt.Errorf("control message exceeds bound")
	}
	buffer := make([]byte, size)
	_, err := io.ReadFull(connection, buffer)
	return buffer, err
}
func writeFrame(writer io.Writer, data []byte) error {
	if len(data) == 0 || len(data) > maxControlBytes {
		return fmt.Errorf("control message exceeds bound")
	}
	if err := binary.Write(writer, binary.BigEndian, uint32(len(data))); err != nil {
		return err
	}
	for len(data) > 0 {
		count, err := writer.Write(data)
		if err != nil {
			return err
		}
		if count == 0 {
			return io.ErrShortWrite
		}
		data = data[count:]
	}
	return nil
}
func boundedDiagnostic(value string) string {
	if len(value) <= maxDiagnosticBytes {
		return value
	}
	return value[:maxDiagnosticBytes]
}

// Client reconnects only to a runtime it can reprove from its immutable
// manifest, current kernel identity, private socket, and a fresh MAC challenge.
type Client struct {
	RuntimeDirectory string
	Inspector        ProcessInspector
	MaxSnapshotAge   time.Duration
}

func (c *Client) Snapshot(ctx context.Context, binding Binding) (Snapshot, error) {
	response, err := c.authenticated(ctx, binding, "snapshot")
	if err != nil {
		return Snapshot{}, err
	}
	if c.MaxSnapshotAge > 0 && time.Since(response.Snapshot.ObservedAt) > c.MaxSnapshotAge {
		return Snapshot{}, fmt.Errorf("supervisor snapshot is stale")
	}
	return response.Snapshot, nil
}
func (c *Client) Stop(ctx context.Context, binding Binding) error {
	_, err := c.authenticated(ctx, binding, "stop")
	return err
}
func (c *Client) authenticated(ctx context.Context, binding Binding, action string) (controlResponse, error) {
	if c == nil || !binding.valid() || !privateDirectory(c.RuntimeDirectory) || c.Inspector == nil || !c.Inspector.Supported() {
		return controlResponse{}, fmt.Errorf("supervisor ownership unavailable")
	}
	manifest, err := readManifest(filepath.Join(c.RuntimeDirectory, manifestName))
	if err != nil {
		return controlResponse{}, err
	}
	if manifest.Binding != binding {
		return controlResponse{}, fmt.Errorf("supervisor binding mismatch")
	}
	current, err := c.Inspector.Observe(ctx, manifest.Supervisor.PID)
	if err != nil || !current.matches(manifest.Supervisor) {
		return controlResponse{}, fmt.Errorf("supervisor process identity no longer matches")
	}
	for _, child := range manifest.Children {
		observed, err := c.Inspector.Observe(ctx, child.PID)
		if err != nil || !observed.matches(child) {
			return controlResponse{}, fmt.Errorf("supervisor direct child identity no longer matches")
		}
	}
	key, err := decodeKey(manifest.ControlKey)
	if err != nil {
		return controlResponse{}, err
	}
	return c.call(ctx, binding, action, randomChallengeOrEmpty(), key)
}
func randomChallengeOrEmpty() string {
	value, err := newChallenge()
	if err != nil {
		return ""
	}
	return value
}
func (c *Client) call(ctx context.Context, binding Binding, action, challenge string, key []byte) (controlResponse, error) {
	if challenge == "" || len(key) != 32 {
		return controlResponse{}, fmt.Errorf("invalid control challenge or key")
	}
	request := controlRequest{Version: 1, Action: action, Binding: binding, Challenge: challenge}
	var err error
	request.MAC, err = requestMAC(key, request)
	if err != nil {
		return controlResponse{}, err
	}
	socket := filepath.Join(c.RuntimeDirectory, socketName)
	info, err := os.Lstat(socket)
	if err != nil || info.Mode()&os.ModeSocket == 0 || info.Mode().Perm() != 0o600 || !ownedByCurrentUser(info) {
		return controlResponse{}, fmt.Errorf("control socket is not owner-private: %v", err)
	}
	connection, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: socket, Net: "unix"})
	if err != nil {
		return controlResponse{}, err
	}
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(2 * time.Second))
	data, err := json.Marshal(request)
	if err != nil {
		return controlResponse{}, err
	}
	if len(data) > maxControlBytes {
		return controlResponse{}, fmt.Errorf("control request exceeds bound")
	}
	if err := writeFrame(connection, data); err != nil {
		return controlResponse{}, err
	}
	responseData, err := readBounded(connection)
	if err != nil {
		return controlResponse{}, err
	}
	var response controlResponse
	if err := decodeExact(responseData, &response); err != nil {
		return controlResponse{}, err
	}
	if response.Version != 1 || response.Binding != binding || response.Challenge != challenge {
		return controlResponse{}, fmt.Errorf("control response binding or challenge mismatch")
	}
	want, err := responseMAC(key, response)
	if err != nil || !hmac.Equal([]byte(want), []byte(response.MAC)) {
		return controlResponse{}, fmt.Errorf("control response MAC mismatch")
	}
	if len(response.Snapshot.Diagnostic) > maxDiagnosticBytes || response.Snapshot.Binding != binding {
		return controlResponse{}, fmt.Errorf("invalid bounded snapshot")
	}
	return response, nil
}

var _ Controller = (*Client)(nil)
var _ = strings.Builder{}

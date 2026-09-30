package guestproto

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"
	"unicode/utf8"
)

const (
	MaxClipboardTextBytes     = 1 << 20
	MaxClipboardMetadataBytes = 4096
	MaxClipboardRequestBytes  = MaxClipboardMetadataBytes + 1 + 4 + MaxClipboardTextBytes
	MaxClipboardResponseBytes = MaxClipboardMetadataBytes + 1 + MaxClipboardTextBytes
	ClipboardHelperPath       = "/usr/local/libexec/boxwarden-guest-clipboard.py"
	clipboardGenerationPath   = "run/boxwarden/clipboard-generation.json"
)

var errClipboardFrame = errors.New("invalid clipboard frame")

// ClipboardRequest is a fixed operation with an exact runtime binding. The
// length-prefixed text follows its canonical JSON line; it is never JSON/argv.
type ClipboardRequest struct {
	Version int `json:"version"`
	Association
	Generation string    `json:"generation"`
	ExpiresAt  time.Time `json:"expires_at"`
	Direction  string    `json:"direction"`
}
type ClipboardResponse struct {
	Version int `json:"version"`
	Association
	Generation string `json:"generation"`
	Status     string `json:"status"`
	Length     int    `json:"length"`
}

func (r ClipboardRequest) validateShape() error {
	if r.Version != Version || !r.Association.valid() || !validUUID(r.Generation) ||
		(r.Direction != "read" && r.Direction != "write") || r.ExpiresAt.IsZero() || r.ExpiresAt.Location() != time.UTC {
		return errClipboardFrame
	}
	return nil
}
func (r ClipboardRequest) Validate() error {
	now := time.Now()
	if r.validateShape() != nil || !r.ExpiresAt.After(now) || r.ExpiresAt.After(now.Add(30*time.Second)) {
		return errClipboardFrame
	}
	return nil
}

func ValidateClipboardText(value []byte) error {
	if len(value) > MaxClipboardTextBytes || !utf8.Valid(value) || bytes.IndexByte(value, 0) >= 0 {
		return errors.New("invalid clipboard text")
	}
	return nil
}
func EncodeClipboardRequest(request ClipboardRequest, payload []byte) ([]byte, error) {
	if request.Validate() != nil || ValidateClipboardText(payload) != nil || (request.Direction == "read" && len(payload) != 0) {
		return nil, errClipboardFrame
	}
	header, err := json.Marshal(request)
	if err != nil || len(header) > MaxClipboardMetadataBytes {
		return nil, errClipboardFrame
	}
	frame := make([]byte, len(header)+1+4+len(payload))
	copy(frame, header)
	frame[len(header)] = '\n'
	binary.BigEndian.PutUint32(frame[len(header)+1:], uint32(len(payload)))
	copy(frame[len(header)+5:], payload)
	return frame, nil
}
func DecodeClipboardRequest(reader io.Reader) (ClipboardRequest, []byte, error) {
	var request ClipboardRequest
	header, err := readCanonicalLine(reader, MaxClipboardMetadataBytes)
	if err != nil {
		return request, nil, errClipboardFrame
	}
	fields, err := exactObject(header, "version", "domain", "session_id", "backend_kind", "backend_object", "generation", "expires_at", "direction")
	if err != nil || decodeFields(fields, &request) != nil || request.Validate() != nil {
		return ClipboardRequest{}, nil, errClipboardFrame
	}
	canonical, _ := json.Marshal(request)
	if !bytes.Equal(header, canonical) {
		return ClipboardRequest{}, nil, errClipboardFrame
	}
	var length [4]byte
	if _, err := io.ReadFull(reader, length[:]); err != nil {
		return ClipboardRequest{}, nil, errClipboardFrame
	}
	size := binary.BigEndian.Uint32(length[:])
	if size > MaxClipboardTextBytes || (request.Direction == "read" && size != 0) {
		return ClipboardRequest{}, nil, errClipboardFrame
	}
	payload := make([]byte, int(size))
	if _, err := io.ReadFull(reader, payload); err != nil || ValidateClipboardText(payload) != nil || clipboardEOF(reader) != nil {
		return ClipboardRequest{}, nil, errClipboardFrame
	}
	return request, payload, nil
}
func clipboardEOF(reader io.Reader) error {
	var extra [1]byte
	n, err := reader.Read(extra[:])
	if n != 0 || err != io.EOF {
		return errClipboardFrame
	}
	return nil
}
func (response ClipboardResponse) validate(request ClipboardRequest, payload []byte) error {
	if request.validateShape() != nil || response.Version != Version || response.Association != request.Association || response.Generation != request.Generation || response.Length < 0 || response.Length > MaxClipboardTextBytes {
		return errClipboardFrame
	}
	switch response.Status {
	case "ok":
		if request.Direction == "read" {
			if response.Length != len(payload) || ValidateClipboardText(payload) != nil {
				return errClipboardFrame
			}
		} else if len(payload) != 0 {
			return errClipboardFrame
		}
	case "error", "unknown":
		if response.Length != 0 || len(payload) != 0 || (response.Status == "unknown" && request.Direction != "write") {
			return errClipboardFrame
		}
	default:
		return errClipboardFrame
	}
	return nil
}
func EncodeClipboardResponse(request ClipboardRequest, response ClipboardResponse, payload []byte) ([]byte, error) {
	if response.validate(request, payload) != nil {
		return nil, errClipboardFrame
	}
	header, err := json.Marshal(response)
	if err != nil || len(header) > MaxClipboardMetadataBytes {
		return nil, errClipboardFrame
	}
	raw := make([]byte, len(header)+1+len(payload))
	copy(raw, header)
	raw[len(header)] = '\n'
	copy(raw[len(header)+1:], payload)
	return raw, nil
}
func DecodeClipboardResponse(request ClipboardRequest, reader io.Reader) (ClipboardResponse, []byte, error) {
	var response ClipboardResponse
	header, err := readCanonicalLine(reader, MaxClipboardMetadataBytes)
	if err != nil {
		return response, nil, errClipboardFrame
	}
	fields, err := exactObject(header, "version", "domain", "session_id", "backend_kind", "backend_object", "generation", "status", "length")
	if err != nil || decodeFields(fields, &response) != nil || response.Length < 0 || response.Length > MaxClipboardTextBytes {
		return ClipboardResponse{}, nil, errClipboardFrame
	}
	canonical, _ := json.Marshal(response)
	if !bytes.Equal(header, canonical) {
		return ClipboardResponse{}, nil, errClipboardFrame
	}
	size := 0
	if request.Direction == "read" && response.Status == "ok" {
		size = response.Length
	}
	payload := make([]byte, size)
	if _, err := io.ReadFull(reader, payload); err != nil || response.validate(request, payload) != nil || clipboardEOF(reader) != nil {
		return ClipboardResponse{}, nil, errClipboardFrame
	}
	return response, payload, nil
}

type clipboardGeneration struct {
	Version int `json:"version"`
	Association
	Generation string `json:"generation"`
}

func (b *Bootstrapper) clipboardRuntimeDirectory(create bool) (string, error) {
	if b == nil || b.Root == "" {
		return "", errors.New("clipboard binding unavailable")
	}
	run, _ := b.path("run")
	if create {
		if err := os.Mkdir(run, 0755); err != nil && !errors.Is(err, os.ErrExist) {
			return "", err
		}
	}
	if err := safeDirectory(run, 0755); err != nil {
		return "", err
	}
	dir := filepath.Join(run, "boxwarden")
	if create {
		if err := os.Mkdir(dir, 0700); err != nil && !errors.Is(err, os.ErrExist) {
			return "", err
		}
	}
	if err := safeDirectory(dir, 0700); err != nil {
		return "", err
	}
	return dir, nil
}
func readClipboardGeneration(filename string) (clipboardGeneration, error) {
	var record clipboardGeneration
	raw, err := readSafeFile(filename, 0600)
	if err != nil || len(raw) > MaxClipboardMetadataBytes {
		return record, errors.New("clipboard binding unavailable")
	}
	fields, err := exactObject(raw, "version", "domain", "session_id", "backend_kind", "backend_object", "generation")
	if err != nil || decodeFields(fields, &record) != nil || record.Version != Version || !record.Association.valid() || !validUUID(record.Generation) {
		return clipboardGeneration{}, errors.New("clipboard binding unavailable")
	}
	canonical, _ := json.Marshal(record)
	if !bytes.Equal(raw, append(canonical, '\n')) {
		return clipboardGeneration{}, errors.New("clipboard binding unavailable")
	}
	return record, nil
}

// publishClipboardGeneration follows successful Serial verification. This is
// ephemeral runtime metadata, separate from the durable SSH trust association.
func (b *Bootstrapper) publishClipboardGeneration(request SerialRequest) error {
	dir, err := b.clipboardRuntimeDirectory(true)
	if err != nil {
		return errors.New("clipboard binding unavailable")
	}
	filename := filepath.Join(dir, "clipboard-generation.json")
	if _, err := os.Lstat(filename); err == nil {
		old, err := readClipboardGeneration(filename)
		if err != nil || old.Association != request.Association {
			return errors.New("clipboard binding unavailable")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return errors.New("clipboard binding unavailable")
	}
	raw, _ := json.Marshal(clipboardGeneration{Version: Version, Association: request.Association, Generation: request.StartGeneration})
	temporary, err := os.CreateTemp(dir, ".clipboard-generation-")
	if err != nil {
		return errors.New("clipboard binding unavailable")
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err := temporary.Chmod(0600); err != nil {
		temporary.Close()
		return errors.New("clipboard binding unavailable")
	}
	if _, err := temporary.Write(append(raw, '\n')); err != nil {
		temporary.Close()
		return errors.New("clipboard binding unavailable")
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return errors.New("clipboard binding unavailable")
	}
	if err := temporary.Close(); err != nil {
		return errors.New("clipboard binding unavailable")
	}
	if err := os.Rename(name, filename); err != nil {
		return errors.New("clipboard binding unavailable")
	}
	if err := syncDir(dir); err != nil {
		return errors.New("clipboard binding unavailable")
	}
	return nil
}
func (b *Bootstrapper) checkClipboardBinding(request ClipboardRequest) error {
	if b == nil || b.validateBootstrapAncestors() != nil {
		return errors.New("clipboard binding unavailable")
	}
	active, _ := b.path(filepath.Join(boxwardenRelative, activeName))
	if verifyActiveAssociation(active, request.Association) != nil {
		return errors.New("clipboard binding unavailable")
	}
	dir, err := b.clipboardRuntimeDirectory(false)
	if err != nil {
		return errors.New("clipboard binding unavailable")
	}
	record, err := readClipboardGeneration(filepath.Join(dir, "clipboard-generation.json"))
	if err != nil || record.Association != request.Association || record.Generation != request.Generation {
		return errors.New("clipboard binding unavailable")
	}
	return nil
}

// ClipboardExecutor runs only the fixed desktop adapter. Its output includes
// a small receipt line and optional raw read bytes. No ordinary runner bound
// is raised, and no payload enters command arguments, stderr or persistent state.
type ClipboardExecutor interface {
	Run(context.Context, string, []byte) ([]byte, error)
}
type ExecClipboardExecutor struct{}

func (ExecClipboardExecutor) Run(ctx context.Context, direction string, payload []byte) ([]byte, error) {
	if (direction != "read" && direction != "write") || ValidateClipboardText(payload) != nil || (direction == "read" && len(payload) != 0) {
		return nil, errors.New("clipboard execution refused")
	}
	// The adapter is a generic installed root-owned artifact, never session code.
	for _, dir := range []string{"/usr", "/usr/local", "/usr/local/libexec"} {
		if safeDirectory(dir, 0755) != nil {
			return nil, errors.New("clipboard adapter unavailable")
		}
	}
	if _, err := readSafeFile(ClipboardHelperPath, 0755); err != nil {
		return nil, errors.New("clipboard adapter unavailable")
	}
	deadline, ok := ctx.Deadline()
	if !ok || ctx.Err() != nil {
		return nil, errors.New("clipboard deadline unavailable")
	}
	command := exec.CommandContext(ctx, "/usr/bin/python3", ClipboardHelperPath, direction, "--deadline-unix-ns", strconv.FormatInt(deadline.UnixNano(), 10))
	command.Env = []string{"HOME=/root", "USER=root", "LOGNAME=root", "PATH=/usr/bin:/bin", "LANG=C.UTF-8"}
	command.Dir = "/"
	command.Stdin = bytes.NewReader(payload)
	command.Stderr = io.Discard
	command.WaitDelay = 2 * time.Second
	var output clipboardOutputBuffer
	command.Stdout = &output
	err := command.Run()
	if output.overflow {
		return nil, errors.New("clipboard adapter output exceeds bound")
	}
	return output.Bytes(), err
}

type clipboardOutputBuffer struct {
	bytes.Buffer
	overflow bool
}

func (b *clipboardOutputBuffer) Write(data []byte) (int, error) {
	if b.Len()+len(data) > MaxClipboardResponseBytes {
		b.overflow = true
		return 0, errors.New("clipboard output exceeds bound")
	}
	return b.Buffer.Write(data)
}
func decodeDesktopReceipt(request ClipboardRequest, raw []byte) (ClipboardResponse, []byte, error) {
	reader := bytes.NewReader(raw)
	header, err := readCanonicalLine(reader, MaxClipboardMetadataBytes)
	if err != nil {
		return ClipboardResponse{}, nil, errClipboardFrame
	}
	fields, err := exactObject(header, "version", "status", "length")
	if err != nil {
		return ClipboardResponse{}, nil, errClipboardFrame
	}
	var receipt struct {
		Version int    `json:"version"`
		Status  string `json:"status"`
		Length  int    `json:"length"`
	}
	if decodeFields(fields, &receipt) != nil || receipt.Length < 0 || receipt.Length > MaxClipboardTextBytes {
		return ClipboardResponse{}, nil, errClipboardFrame
	}
	canonical, _ := json.Marshal(receipt)
	if !bytes.Equal(header, canonical) {
		return ClipboardResponse{}, nil, errClipboardFrame
	}
	response := ClipboardResponse{Version: receipt.Version, Association: request.Association, Generation: request.Generation, Status: receipt.Status, Length: receipt.Length}
	payload := []byte(nil)
	if request.Direction == "read" && response.Status == "ok" {
		payload = make([]byte, response.Length)
		if _, err := io.ReadFull(reader, payload); err != nil {
			return ClipboardResponse{}, nil, errClipboardFrame
		}
	}
	if response.validate(request, payload) != nil || clipboardEOF(reader) != nil {
		return ClipboardResponse{}, nil, errClipboardFrame
	}
	return response, payload, nil
}

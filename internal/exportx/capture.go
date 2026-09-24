package exportx

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/weshofmann/boxwarden/internal/diskreserve"
	"github.com/weshofmann/boxwarden/internal/privateacl"
)

const (
	maxInspectorStreamBytes = 320 << 20
	maxInspectorLogBytes    = 16 << 10
	inspectorDeadline       = 80 * time.Second
)

// InspectorEvidence is the host helper's final observation, emitted only
// after its VM has stopped and both serial pipes reached EOF.
type InspectorEvidence struct {
	VMState               string
	RuntimeNetworkDevices int64
	ConsoleBytes          int64
	ExportBytes           int64
	Mode                  string
}

// CapturedInspectorStream is a complete private spool. The caller owns Stream
// and must still verify the snapshot after helper reap, before calling Receive.
type CapturedInspectorStream struct {
	Stream   *os.File
	Evidence InspectorEvidence
	parent   string
	parentID os.FileInfo
	streamID os.FileInfo
}

// AdmitForPublication rechecks the exact private spool and its bound parent
// immediately before a journal owner hands Stream to the receiver. The
// receiver closes Stream, so call this before ReceiveSelectedExport.
func (captured CapturedInspectorStream) AdmitForPublication(expectedParent string) error {
	if captured.Stream == nil || captured.parentID == nil || captured.streamID == nil || expectedParent != captured.parent ||
		captured.Evidence.Mode != "export" || captured.Evidence.VMState != "stopped" || captured.Evidence.RuntimeNetworkDevices != 0 {
		return fmt.Errorf("captured export spool lacks exact publication binding")
	}
	parentFile, parentRoot, parentInfo, err := openPrivateParent(expectedParent)
	if err != nil {
		return err
	}
	defer parentFile.Close()
	defer parentRoot.Close()
	if !os.SameFile(parentInfo, captured.parentID) {
		return fmt.Errorf("captured spool parent changed")
	}
	if err := privateacl.Check(expectedParent, parentInfo, captureACLInspector); err != nil {
		return err
	}
	pathInfo, err := parentRoot.Lstat("stream.bin")
	if err != nil || !os.SameFile(pathInfo, captured.streamID) || !privateCapturedFile(pathInfo) {
		return fmt.Errorf("captured spool path changed: %v", err)
	}
	opened, err := captured.Stream.Stat()
	if err != nil || !os.SameFile(opened, pathInfo) || !privateCapturedFile(opened) || opened.Size() != captured.Evidence.ExportBytes {
		return fmt.Errorf("captured spool descriptor changed: %v", err)
	}
	if err := privateacl.Check(filepath.Join(expectedParent, "stream.bin"), pathInfo, captureACLInspector); err != nil {
		return err
	}
	_, err = captured.Stream.Seek(0, io.SeekStart)
	return err
}

var captureACLInspector privateacl.Inspector = privateacl.OSInspector{}

// Remove closes the captured descriptor if it is still open and removes only
// the originally captured private inode. Call it after receiver publication or
// failure has been reconciled; a changed path is left intact for inspection.
func (captured CapturedInspectorStream) Remove() error {
	if captured.Stream == nil || captured.parentID == nil || captured.streamID == nil {
		return fmt.Errorf("no captured inspector stream to remove")
	}
	if err := captured.Stream.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
		return err
	}
	parentFile, parentRoot, parentInfo, err := openPrivateParent(captured.parent)
	if err != nil {
		return err
	}
	defer parentFile.Close()
	defer parentRoot.Close()
	if !os.SameFile(parentInfo, captured.parentID) {
		return fmt.Errorf("inspector spool parent changed")
	}
	if err := privateacl.Check(captured.parent, parentInfo, captureACLInspector); err != nil {
		return err
	}
	info, err := parentRoot.Lstat("stream.bin")
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || !os.SameFile(info, captured.streamID) || !privateCapturedFile(info) {
		return fmt.Errorf("inspector spool path changed: %v", err)
	}
	if err := privateacl.Check(filepath.Join(captured.parent, "stream.bin"), info, captureACLInspector); err != nil {
		return err
	}
	if err := parentRoot.Remove("stream.bin"); err != nil {
		return err
	}
	return parentFile.Sync()
}

// CaptureInspector runs an already-admitted inspector helper with exact argv.
// The caller must pin the executable, kernel, initramfs, and snapshot and must
// verify the snapshot again before passing Stream to the receiver. This gate
// only exposes the stream after bounded capture, process reap, and exact host
// stopped/zero-NIC evidence. PrivateParent must be an existing 0700 directory;
// stream.bin must not already exist.
func CaptureInspector(ctx context.Context, executable string, args []string, privateParent string) (CapturedInspectorStream, error) {
	var result CapturedInspectorStream
	err := diskreserve.Run(ctx, []string{privateParent}, func(guarded context.Context) error {
		var captureErr error
		result, captureErr = captureInspector(guarded, executable, args, privateParent, maxInspectorStreamBytes, maxInspectorLogBytes)
		return captureErr
	})
	if err != nil {
		if result.Stream != nil {
			err = errors.Join(err, result.Remove())
		}
		return CapturedInspectorStream{}, err
	}
	return result, nil
}

// CaptureExportInspector refuses a synthetic helper even when it produced a
// well-formed typed stream under the same transaction. Production callers
// must still admit the executable and boot artifacts before invoking it.
func CaptureExportInspector(ctx context.Context, executable string, args []string, privateParent string) (CapturedInspectorStream, error) {
	captured, err := CaptureInspector(ctx, executable, args, privateParent)
	if err != nil {
		return CapturedInspectorStream{}, err
	}
	if captured.Evidence.Mode != "export" {
		return CapturedInspectorStream{}, errors.Join(
			fmt.Errorf("inspector helper did not prove export mode"), captured.Remove(),
		)
	}
	if captured.Evidence.ExportBytes < 22 {
		return CapturedInspectorStream{}, errors.Join(
			fmt.Errorf("inspector guest produced no complete export header"), captured.Remove(),
		)
	}
	return captured, nil
}

func captureInspector(ctx context.Context, executable string, args []string, privateParent string, streamLimit, logLimit int64) (captured CapturedInspectorStream, err error) {
	if executable == "" || !filepath.IsAbs(executable) || filepath.Clean(executable) != executable || streamLimit <= 0 || logLimit <= 0 {
		return CapturedInspectorStream{}, fmt.Errorf("invalid inspector capture inputs")
	}
	if err := ctx.Err(); err != nil {
		return CapturedInspectorStream{}, err
	}
	parentFile, parentRoot, parentInfo, err := openPrivateParent(privateParent)
	if err != nil {
		return CapturedInspectorStream{}, err
	}
	defer parentFile.Close()
	defer parentRoot.Close()
	if err := privateacl.Check(privateParent, parentInfo, captureACLInspector); err != nil {
		return CapturedInspectorStream{}, fmt.Errorf("capture parent ACL before launch: %w", err)
	}
	stream, err := parentRoot.OpenFile("stream.bin", os.O_RDWR|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return CapturedInspectorStream{}, err
	}
	keep := false
	defer func() {
		if !keep {
			err = errors.Join(err, stream.Close(), parentRoot.Remove("stream.bin"))
		}
	}()
	bootContext, cancel := context.WithTimeout(ctx, inspectorDeadline)
	defer cancel()
	command := exec.CommandContext(bootContext, executable, args...)
	command.Dir = privateParent
	command.Env = []string{"PATH=/usr/bin:/bin", "LANG=C"}
	command.Stdin = nil
	command.WaitDelay = 5 * time.Second
	stdout := &boundedInspectorWriter{output: stream, limit: streamLimit}
	var log bytes.Buffer
	stderr := &boundedInspectorWriter{output: &log, limit: logLimit}
	command.Stdout, command.Stderr = stdout, stderr
	if err := command.Run(); err != nil {
		return CapturedInspectorStream{}, fmt.Errorf("inspector helper did not complete and reap: %w", err)
	}
	if err := bootContext.Err(); err != nil {
		return CapturedInspectorStream{}, err
	}
	if stdout.overflow || stderr.overflow {
		return CapturedInspectorStream{}, fmt.Errorf("inspector output exceeded capture limit")
	}
	if err := stream.Sync(); err != nil {
		return CapturedInspectorStream{}, err
	}
	info, err := stream.Stat()
	if err != nil || info.Size() != stdout.count || !privateCapturedFile(info) {
		return CapturedInspectorStream{}, fmt.Errorf("captured stream metadata changed: %v", err)
	}
	pathInfo, err := parentRoot.Lstat("stream.bin")
	if err != nil || !os.SameFile(info, pathInfo) || !privateCapturedFile(pathInfo) {
		return CapturedInspectorStream{}, fmt.Errorf("captured stream path changed: %v", err)
	}
	if err := samePrivateParent(privateParent, parentInfo, parentFile); err != nil {
		return CapturedInspectorStream{}, err
	}
	finalParentInfo, err := parentFile.Stat()
	if err != nil || !os.SameFile(parentInfo, finalParentInfo) {
		return CapturedInspectorStream{}, fmt.Errorf("capture parent changed after helper reap: %v", err)
	}
	if err := privateacl.Check(privateParent, finalParentInfo, captureACLInspector); err != nil {
		return CapturedInspectorStream{}, fmt.Errorf("capture parent ACL after helper reap: %w", err)
	}
	if err := privateacl.Check(filepath.Join(privateParent, "stream.bin"), pathInfo, captureACLInspector); err != nil {
		return CapturedInspectorStream{}, fmt.Errorf("captured stream ACL after helper reap: %w", err)
	}
	evidence, err := parseInspectorEvidence(log.Bytes(), info.Size())
	if err != nil {
		return CapturedInspectorStream{}, err
	}
	if _, err := stream.Seek(0, io.SeekStart); err != nil {
		return CapturedInspectorStream{}, err
	}
	keep = true
	return CapturedInspectorStream{Stream: stream, Evidence: evidence, parent: privateParent, parentID: finalParentInfo, streamID: pathInfo}, nil
}

type boundedInspectorWriter struct {
	output   io.Writer
	limit    int64
	count    int64
	overflow bool
}

func (w *boundedInspectorWriter) Write(data []byte) (int, error) {
	length := len(data)
	remaining := w.limit - w.count
	if remaining <= 0 {
		w.overflow = true
		return length, nil // keep draining until the helper stops
	}
	if int64(length) > remaining {
		data = data[:remaining]
		w.overflow = true
	}
	n, err := w.output.Write(data)
	w.count += int64(n)
	if err != nil {
		return n, err
	}
	if n != len(data) {
		return n, io.ErrShortWrite
	}
	return length, nil
}

func privateCapturedFile(info os.FileInfo) bool {
	if info == nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Nlink == 1 && stat.Uid == uint32(os.Geteuid())
}

func parseInspectorEvidence(log []byte, streamSize int64) (InspectorEvidence, error) {
	const prefix = "BOOT_EVIDENCE "
	var payload []byte
	for _, line := range bytes.Split(log, []byte{'\n'}) {
		if bytes.HasPrefix(line, []byte(prefix)) {
			if payload != nil {
				return InspectorEvidence{}, fmt.Errorf("duplicate inspector evidence")
			}
			payload = line[len(prefix):]
		}
	}
	if len(payload) == 0 {
		return InspectorEvidence{}, fmt.Errorf("missing inspector host evidence")
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return InspectorEvidence{}, fmt.Errorf("invalid inspector host evidence: %v", err)
	}
	fields := make(map[string]json.RawMessage, 5)
	for decoder.More() {
		keyToken, err := decoder.Token()
		key, ok := keyToken.(string)
		if err != nil || !ok {
			return InspectorEvidence{}, fmt.Errorf("invalid inspector evidence field: %v", err)
		}
		if _, exists := fields[key]; exists {
			return InspectorEvidence{}, fmt.Errorf("duplicate inspector evidence field")
		}
		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			return InspectorEvidence{}, err
		}
		fields[key] = raw
	}
	if token, err = decoder.Token(); err != nil || token != json.Delim('}') {
		return InspectorEvidence{}, fmt.Errorf("invalid inspector evidence ending: %v", err)
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return InspectorEvidence{}, fmt.Errorf("trailing inspector evidence")
	}
	if len(fields) != 4 && len(fields) != 5 {
		return InspectorEvidence{}, fmt.Errorf("incomplete inspector host evidence")
	}
	var result InspectorEvidence
	if len(fields) == 5 {
		if err := json.Unmarshal(fields["inspector_mode"], &result.Mode); err != nil || result.Mode != "export" {
			return InspectorEvidence{}, fmt.Errorf("inspector did not report export mode")
		}
	}
	if err := json.Unmarshal(fields["vm_state"], &result.VMState); err != nil || result.VMState != "stopped" {
		return InspectorEvidence{}, fmt.Errorf("inspector VM did not stop")
	}
	for name, target := range map[string]*int64{
		"runtime_network_devices": &result.RuntimeNetworkDevices,
		"console_bytes":           &result.ConsoleBytes,
		"export_bytes":            &result.ExportBytes,
	} {
		raw, ok := fields[name]
		if !ok || bytes.Equal(raw, []byte("null")) || json.Unmarshal(raw, target) != nil || *target < 0 {
			return InspectorEvidence{}, fmt.Errorf("invalid inspector evidence count %s", name)
		}
	}
	if result.RuntimeNetworkDevices != 0 || result.ConsoleBytes > 256<<10 || result.ExportBytes != streamSize {
		return InspectorEvidence{}, fmt.Errorf("inspector host observations do not match captured stream")
	}
	return result, nil
}

//go:build n1clipboarddiagnostic && !n1candidate

package sshx

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/netip"
	"regexp"
	"strings"
	"time"
)

// These programs are immutable compiled source. Callers supply data only.
//
//go:embed n1guest/stager.py
var n1StagerSource string

//go:embed n1guest/inspector.py
var n1InspectorSource string

//go:embed n1guest/controls.py
var n1ControlsSource string

//go:embed n1guest/connect.py
var n1ConnectSource string

var ErrN1Guest = errors.New("n1_guest_incomplete")

const n1MaxStageBytes = 40 << 20
const n1GenericSHA = "33b12b9e293bcbdf7d6c91f933b42003ca394e7a678ac83b8d80df714d27c57e"
const n1ProductionSHA = "e38530c45a2aab705be80aad3e9096f2fce9330cb6c0b5b6623a3d76403b4dbb"
const n1ObserverSHA = "1a52c58c2143f3f2876bdde829fba6dca35a99f23843a8bb0af61aa4a018c455"

var n1Names = [9]string{"boxwarden-guest-bootstrap", "boxwarden-n1-clipboard-diagnostic", "boxwarden-guest-clipboard.py", "boxwarden-guest-clipboard.production.py", "boxwarden-n1-guest-metadata-observer.py", "boxwarden-n1-stager.py", "boxwarden-n1-inspector.py", "boxwarden-n1-controls.py", "boxwarden-n1-tcp-probe.py"}
var n1BackendPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// N1GuestBinding is cooperative guest association evidence. The coordinator
// owns fresh exact-generation READY, current certificate, locks and one-use intent.
type N1GuestBinding struct {
	Version       int    `json:"version"`
	Domain        string `json:"domain"`
	SessionID     string `json:"session_id"`
	BackendKind   string `json:"backend_kind"`
	BackendObject string `json:"backend_object"`
	Generation    string `json:"generation"`
}

func (b N1GuestBinding) valid() bool {
	return b.Version == 1 && b.Domain == "n1qualification" && b.BackendKind == "tart" && validUUID(b.SessionID) && validUUID(b.Generation) && b.SessionID != b.Generation && n1BackendPattern.MatchString(b.BackendObject)
}

// The static-lock-admitted expected SHA is distinct from the received bytes.
// No destination, program, path or command can be chosen by the caller.
type N1GuestArtifact struct {
	Bytes          []byte
	ExpectedSHA256 string
}
type N1GuestArtifacts struct{ GenericHelper, TrialHelper, Overlay, Production, Observer N1GuestArtifact }
type N1GuestHashes struct{ GenericHelper, TrialHelper, Overlay, Production, Observer, Stager, Inspector, Controls, Connect string }
type n1ArtifactDescriptor struct {
	Name   string `json:"name"`
	Length int    `json:"length"`
	SHA256 string `json:"sha256"`
}
type n1StageHeader struct {
	Binding   N1GuestBinding         `json:"binding"`
	Artifacts []n1ArtifactDescriptor `json:"artifacts"`
}
type N1StageResult struct {
	Version   int            `json:"version"`
	Binding   N1GuestBinding `json:"binding"`
	Code      string         `json:"code"`
	Installed int            `json:"installed"`
}
type N1InspectRequest struct {
	Binding   N1GuestBinding
	Artifacts N1GuestArtifacts
	Hashes    N1GuestHashes
	Phase     N1GuestPhase
}
type N1GuestPhase string

const (
	N1GuestInitial N1GuestPhase = "initial"
	N1GuestFinal   N1GuestPhase = "final"
)

type N1InspectResult struct {
	Version             int            `json:"version"`
	Binding             N1GuestBinding `json:"binding"`
	Code                string         `json:"code"`
	Phase               N1GuestPhase   `json:"phase"`
	Installed           int            `json:"installed"`
	DiagnosticNamespace string         `json:"diagnostic_namespace"`
}
type N1ControlsResult struct {
	Version int            `json:"version"`
	Binding N1GuestBinding `json:"binding"`
	Code    string         `json:"code"`
	DNS     string         `json:"dns"`
	HTTPS   string         `json:"https"`
}
type N1ConnectRequest struct {
	Binding     N1GuestBinding `json:"binding"`
	ControlIPv4 string         `json:"control_ipv4"`
}
type N1ConnectResult struct {
	ControlIPv4 string         `json:"control_ipv4"`
	Version     int            `json:"version"`
	Binding     N1GuestBinding `json:"binding"`
	Code        string         `json:"code"`
	Errno       int            `json:"errno"`
	ElapsedUS   int64          `json:"elapsed_us"`
	Connected   bool           `json:"connected"`
	CloseOK     bool           `json:"close_ok"`
	TimingOK    bool           `json:"timing_ok"`
}

// N1GuestSourceHashes lets static-lock preparation bind exact embedded sources.
// It returns public metadata, never source or a runnable command.
func N1GuestSourceHashes() N1GuestHashes {
	return N1GuestHashes{GenericHelper: n1GenericSHA, Production: n1ProductionSHA, Observer: n1ObserverSHA, Stager: n1Hash([]byte(n1StagerSource)), Inspector: n1Hash([]byte(n1InspectorSource)), Controls: n1Hash([]byte(n1ControlsSource)), Connect: n1Hash([]byte(n1ConnectSource))}
}
func n1Hash(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func n1ValidHash(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func n1Bundle(a N1GuestArtifacts, h N1GuestHashes) ([]n1ArtifactDescriptor, [][]byte, error) {
	fixed := N1GuestSourceHashes()
	if h.GenericHelper != fixed.GenericHelper || h.Production != fixed.Production || h.Observer != fixed.Observer || h.Stager != fixed.Stager || h.Inspector != fixed.Inspector || h.Controls != fixed.Controls || h.Connect != fixed.Connect {
		return nil, nil, ErrN1Guest
	}
	input := []N1GuestArtifact{a.GenericHelper, a.TrialHelper, a.Overlay, a.Production, a.Observer, {[]byte(n1StagerSource), h.Stager}, {[]byte(n1InspectorSource), h.Inspector}, {[]byte(n1ControlsSource), h.Controls}, {[]byte(n1ConnectSource), h.Connect}}
	expected := []string{h.GenericHelper, h.TrialHelper, h.Overlay, h.Production, h.Observer, h.Stager, h.Inspector, h.Controls, h.Connect}
	ds := make([]n1ArtifactDescriptor, 9)
	values := make([][]byte, 9)
	total := 0
	for i, x := range input {
		limit := 1 << 20
		if i < 2 {
			limit = 16 << 20
		}
		if !n1ValidHash(expected[i]) || x.ExpectedSHA256 != expected[i] || len(x.Bytes) == 0 || len(x.Bytes) > limit || n1Hash(x.Bytes) != expected[i] {
			return nil, nil, ErrN1Guest
		}
		total += len(x.Bytes)
		ds[i] = n1ArtifactDescriptor{n1Names[i], len(x.Bytes), expected[i]}
		values[i] = x.Bytes
	}
	if total > n1MaxStageBytes {
		return nil, nil, ErrN1Guest
	}
	return ds, values, nil
}

func (c *Client) StageN1Guest(ctx context.Context, conn Connection, b N1GuestBinding, a N1GuestArtifacts, h N1GuestHashes) (N1StageResult, error) {
	if c.n1Admit(ctx, conn, b) != nil {
		return N1StageResult{}, ErrN1Guest
	}
	ds, values, err := n1Bundle(a, h)
	if err != nil {
		return N1StageResult{}, ErrN1Guest
	}
	frame, err := n1StageFrame(b, ds, values)
	if err != nil {
		return N1StageResult{}, ErrN1Guest
	}
	raw, runErr := c.n1Run(ctx, conn, n1StagerSource, true, frame, 4096, 45*time.Second)
	var result N1StageResult
	if n1Decode(raw, &result, 4096) != nil || result.Version != 1 || result.Binding != b || result.Code != "staged" || result.Installed != 9 {
		return N1StageResult{}, ErrN1Guest
	}
	return result, runErr
}
func n1StageFrame(b N1GuestBinding, ds []n1ArtifactDescriptor, values [][]byte) ([]byte, error) {
	if !b.valid() || len(ds) != 9 || len(values) != 9 {
		return nil, ErrN1Guest
	}
	size := 0
	for i, d := range ds {
		limit := 1 << 20
		if i < 2 {
			limit = 16 << 20
		}
		if d.Name != n1Names[i] || d.Length != len(values[i]) || d.Length <= 0 || d.Length > limit || !n1ValidHash(d.SHA256) || n1Hash(values[i]) != d.SHA256 {
			return nil, ErrN1Guest
		}
		size += d.Length
	}
	if size > n1MaxStageBytes {
		return nil, ErrN1Guest
	}
	header, err := json.Marshal(n1StageHeader{b, ds})
	if err != nil || len(header) > 65536 {
		return nil, ErrN1Guest
	}
	frame := make([]byte, 4, 4+len(header)+size)
	binary.BigEndian.PutUint32(frame, uint32(len(header)))
	frame = append(frame, header...)
	for _, value := range values {
		frame = append(frame, value...)
	}
	return frame, nil
}

func (c *Client) InspectN1Guest(ctx context.Context, conn Connection, r N1InspectRequest) (N1InspectResult, error) {
	if c.n1Admit(ctx, conn, r.Binding) != nil || (r.Phase != N1GuestInitial && r.Phase != N1GuestFinal) {
		return N1InspectResult{}, ErrN1Guest
	}
	ds, _, err := n1Bundle(r.Artifacts, r.Hashes)
	if err != nil {
		return N1InspectResult{}, ErrN1Guest
	}
	request := struct {
		Binding   N1GuestBinding         `json:"binding"`
		Artifacts []n1ArtifactDescriptor `json:"artifacts"`
		Phase     N1GuestPhase           `json:"phase"`
	}{r.Binding, ds, r.Phase}
	input, _ := json.Marshal(request)
	raw, runErr := c.n1Run(ctx, conn, n1InspectorSource, true, input, 4096, 20*time.Second)
	var result N1InspectResult
	expected := 0
	if r.Phase == N1GuestFinal {
		expected = 9
	}
	if n1Decode(raw, &result, 4096) != nil || result.Version != 1 || result.Binding != r.Binding || result.Code != "inspected" || result.Phase != r.Phase || result.Installed != expected || result.DiagnosticNamespace != "absent" {
		return N1InspectResult{}, ErrN1Guest
	}
	return result, runErr
}
func (c *Client) RunN1Controls(ctx context.Context, conn Connection, b N1GuestBinding) (N1ControlsResult, error) {
	if c.n1Admit(ctx, conn, b) != nil {
		return N1ControlsResult{}, ErrN1Guest
	}
	input, _ := json.Marshal(struct {
		Binding N1GuestBinding `json:"binding"`
	}{b})
	raw, runErr := c.n1Run(ctx, conn, n1ControlsSource, false, input, 4096, 20*time.Second)
	var result N1ControlsResult
	if n1Decode(raw, &result, 4096) != nil || result.Version != 1 || result.Binding != b || result.Code != "controls" || !n1ControlCode(result.DNS) || !n1ControlCode(result.HTTPS) {
		return N1ControlsResult{}, ErrN1Guest
	}
	return result, runErr
}
func n1ControlCode(s string) bool { return s == "ok" || s == "failed" }
func (c *Client) ConnectN1Peer(ctx context.Context, conn Connection, r N1ConnectRequest) (N1ConnectResult, error) {
	if c.n1Admit(ctx, conn, r.Binding) != nil || !n1PrivateIPv4(r.ControlIPv4) || r.ControlIPv4 == conn.Address {
		return N1ConnectResult{}, ErrN1Guest
	}
	input, _ := json.Marshal(r)
	var result N1ConnectResult
	decode := func(raw []byte) error {
		var received N1ConnectResult
		if n1Decode(raw, &received, 4096) != nil || received.ControlIPv4 != r.ControlIPv4 || received.Version != 1 || received.Binding != r.Binding || received.Errno < 0 || received.Errno > 4095 || received.ElapsedUS < 0 || received.ElapsedUS > 10000000 || !(received.Code == "connected" || received.Code == "timeout" || received.Code == "socket_error") || received.Connected != (received.Code == "connected") || (received.Code != "socket_error" && received.Errno != 0) {
			return ErrN1Guest
		}
		result = received
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := Command{Path: sshPath, Args: n1Arguments(conn, n1ConnectSource, false), Stdin: input}
	var runErr error
	switch c.runner.(type) {
	case ExecRunner, *ExecRunner:
		observation, err := n1Execute(ctx, cmd, 4096, func(reader io.Reader) error { return n1ReadSingleRecord(reader, 4096, decode) })
		if err != nil || observation.exit != 0 {
			runErr = ErrN1Guest
		}
	default:
		observation, err := c.runner.Run(ctx, cmd)
		if decode([]byte(observation.Stdout)) != nil || err != nil || ctx.Err() != nil || observation.Truncated || len(observation.Stderr) > 0 {
			runErr = ErrN1Guest
		}
	}
	if result.Version != 1 {
		return N1ConnectResult{}, ErrN1Guest
	}
	return result, runErr
}

func n1PrivateIPv4(s string) bool {
	a, e := netip.ParseAddr(s)
	return e == nil && a.Is4() && a.IsPrivate() && a.String() == s
}
func (c *Client) n1Admit(ctx context.Context, conn Connection, b N1GuestBinding) error {
	if c != nil {
		switch runner := c.runner.(type) {
		case ExecRunner:
			if runner.runner == nil {
				return ErrN1Guest
			}
		case *ExecRunner:
			if runner == nil || runner.runner == nil {
				return ErrN1Guest
			}
		}
	}
	if c == nil || c.runner == nil || ctx.Err() != nil || !b.valid() || b.Domain != string(conn.Binding.Domain) || b.SessionID != conn.Binding.SessionID || b.BackendKind != conn.Binding.BackendKind || b.BackendObject != conn.Binding.BackendObject || conn.Port != 22 || !n1PrivateIPv4(conn.Address) || validateConnection(conn) != nil || verifyKnownHostsPin(conn) != nil {
		return ErrN1Guest
	}
	return nil
}
func n1Arguments(conn Connection, source string, root bool) []string {
	args := strictOpenSSHArguments(conn)
	args = append(args, "-p", "22", "boxwarden@"+conn.Address)
	if root {
		args = append(args, "/usr/bin/sudo", "-n", "--")
	}
	return append(args, "/usr/bin/python3", "-I", "-S", "-c", n1ShellQuote(source))
}
func n1ShellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
func (c *Client) n1Run(ctx context.Context, conn Connection, source string, root bool, input []byte, limit int, budget time.Duration) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	cmd := Command{Path: sshPath, Args: n1Arguments(conn, source, root), Stdin: input}
	switch c.runner.(type) {
	case ExecRunner, *ExecRunner:
		result, err := n1Execute(ctx, cmd, limit, nil)
		if err != nil {
			return nil, ErrN1Guest
		}
		if result.exit != 0 {
			return result.output, ErrN1Guest
		}
		return result.output, nil
	default:
		result, err := c.runner.Run(ctx, cmd)
		if len(result.Stdout) > limit {
			return nil, ErrN1Guest
		}
		if err != nil || ctx.Err() != nil || result.Truncated || len(result.Stderr) > 0 {
			return []byte(result.Stdout), ErrN1Guest
		}
		return []byte(result.Stdout), nil
	}
}

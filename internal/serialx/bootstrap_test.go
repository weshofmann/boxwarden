package serialx

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/weshofmann/boxwarden/internal/guestproto"
)

const requestGeneration = "9b2d12d8-7014-4c5e-9d5c-627c2fcc1575"
const requestKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

func bootstrapRequest() guestproto.SerialRequest {
	raw, _ := base64.StdEncoding.DecodeString(strings.Fields(requestKey)[1])
	sum := sha256.Sum256(raw)
	return guestproto.SerialRequest{Version: 1, Nonce: "nonce-1", StartGeneration: requestGeneration, Association: guestproto.Association{Domain: "work", SessionID: "123e4567-e89b-42d3-a456-426614174000", BackendKind: "tart", BackendObject: "workstation"}, CAPublicKey: requestKey, CAFingerprint: "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:]), Principal: "boxwarden-session-123e4567-e89b-42d3-a456-426614174000"}
}

func bootstrapResult() guestproto.SerialResult {
	r := bootstrapRequest()
	return guestproto.SerialResult{Version: 1, StartGeneration: r.StartGeneration, Association: r.Association, CAFingerprint: r.CAFingerprint, Principal: r.Principal, HostPublicKey: requestKey, InstalledSHA256: map[string]string{"trusted-user-ca.pub": strings.Repeat("a", 64), "authorized_principals/boxwarden": strings.Repeat("b", 64), "management-binding.json": strings.Repeat("c", 64)}, SSHD: map[string]string{"trustedusercakeys": "/etc/ssh/boxwarden/active/trusted-user-ca.pub", "authorizedprincipalsfile": "/etc/ssh/boxwarden/active/authorized_principals/%u", "authorizedkeysfile": "none", "permituserenvironment": "no", "permituserrc": "no", "passwordauthentication": "no", "kbdinteractiveauthentication": "no", "permitrootlogin": "no", "allowagentforwarding": "no", "x11forwarding": "no", "allowtcpforwarding": "no", "allowstreamlocalforwarding": "no", "gatewayports": "no", "permittunnel": "no"}}
}

func runtimePipe(t *testing.T) (*Runtime, net.Conn) {
	t.Helper()
	host, guest := net.Pipe()
	r := newRuntime(host, requestGeneration)
	t.Cleanup(func() { guest.Close(); r.Close() })
	return r, guest
}

func readBootstrap(t *testing.T, guest net.Conn) {
	t.Helper()
	guest.SetDeadline(time.Now().Add(3 * time.Second))
	reader := bufio.NewReader(guest)
	command, err := reader.ReadString('\n')
	if err != nil || command != "/usr/bin/sudo -n -- /usr/local/libexec/boxwarden-guest-bootstrap serial-bootstrap\n" {
		t.Fatalf("fixed bootstrap command = %q, %v", command, err)
	}
	request, err := guestproto.DecodeSerialRequest(reader)
	if err != nil || request != bootstrapRequest() {
		t.Fatalf("canonical request = %#v, %v", request, err)
	}
}

func startBootstrap(r *Runtime, ctx context.Context) <-chan error {
	done := make(chan error, 1)
	go func() {
		got, err := r.Bootstrap(ctx, bootstrapRequest())
		if err == nil && (got.HostPublicKey != requestKey || got.StartGeneration != requestGeneration) {
			err = errors.New("lost validated host-key binding")
		}
		done <- err
	}()
	return done
}

func awaitBootstrap(t *testing.T, done <-chan error, wantError bool) {
	t.Helper()
	select {
	case err := <-done:
		if (err != nil) != wantError {
			t.Fatalf("Bootstrap error = %v, want error %v", err, wantError)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("bootstrap did not finish")
	}
}

func TestBootstrapOnceThenContinuouslyDrainsWithoutParsing(t *testing.T) {
	r, guest := runtimePipe(t)
	done := startBootstrap(r, context.Background())
	readBootstrap(t, guest)
	begin, end, _ := guestproto.EncodeSerialFrame(bootstrapRequest(), bootstrapResult())
	// Fragment every byte, exercising line framing independently of read chunks.
	for _, b := range []byte("boot output\r\n" + begin + "\r\n" + end + "\r\n") {
		if _, err := guest.Write([]byte{b}); err != nil {
			t.Fatal(err)
		}
	}
	awaitBootstrap(t, done, false)
	if _, err := r.Bootstrap(context.Background(), bootstrapRequest()); err == nil {
		t.Fatal("second bootstrap accepted")
	}
	// More than the exchange/line limits, including control-looking text, must
	// drain after success. There is no parser left to poison or stall this path.
	if _, err := guest.Write(append([]byte("BOXWARDEN-END invalid\n"), bytes.Repeat([]byte("x"), 2<<20)...)); err != nil {
		t.Fatal(err)
	}
	if err := r.Err(); err != nil {
		t.Fatalf("drain reparsed output: %v", err)
	}
}

func TestBootstrapRejectsHostileFramesAndPermanentlyPoisons(t *testing.T) {
	request := bootstrapRequest()
	begin, end, _ := guestproto.EncodeSerialFrame(request, bootstrapResult())
	wrong := bootstrapResult()
	wrong.StartGeneration = "80c64529-fcb5-4789-8460-a43517622238"
	raw, _ := json.Marshal(wrong)
	wrongEnd := "BOXWARDEN-END " + request.Nonce + " " + request.SessionID + " " + base64.StdEncoding.EncodeToString(raw)
	for name, wire := range map[string]string{
		"nonce":             strings.Replace(begin, request.Nonce, "different", 1) + "\n",
		"session":           strings.Replace(begin, request.SessionID, requestGeneration, 1) + "\n",
		"duplicate begin":   begin + "\n" + begin + "\n",
		"end without begin": end + "\n",
		"generation":        begin + "\n" + wrongEnd + "\n",
		"interleaved":       begin + "\nother output\n",
		"unknown control":   "BOXWARDEN-OTHER data\n",
		"malformed base64":  begin + "\nBOXWARDEN-END " + request.Nonce + " " + request.SessionID + " !\n",
		"oversized line":    strings.Repeat("x", MaxPhysicalLineBytes+1),
		"total flood":       strings.Repeat("x\n", MaxExchangeBytes/2+1),
		"ambiguous CR":      begin + "\r\r\n",
	} {
		t.Run(name, func(t *testing.T) {
			r, guest := runtimePipe(t)
			done := startBootstrap(r, context.Background())
			readBootstrap(t, guest)
			_, _ = io.WriteString(guest, wire)
			awaitBootstrap(t, done, true)
			if !errors.Is(r.Err(), ErrPoisoned) {
				t.Fatalf("missing poison: %v", r.Err())
			}
			if _, err := r.Bootstrap(context.Background(), request); err == nil {
				t.Fatal("poisoned runtime reused")
			}
		})
	}
}

func TestBootstrapCancellationAndClosureUnblockBothDirections(t *testing.T) {
	for _, phase := range []string{"blocked write", "blocked read", "close"} {
		t.Run(phase, func(t *testing.T) {
			r, guest := runtimePipe(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := startBootstrap(r, ctx)
			if phase != "blocked write" {
				readBootstrap(t, guest)
			} else {
				// Consume one byte, leaving the fixed request write blocked.
				guest.SetReadDeadline(time.Now().Add(time.Second))
				var b [1]byte
				if _, err := guest.Read(b[:]); err != nil {
					t.Fatal(err)
				}
			}
			if phase == "close" {
				if err := r.Close(); err != nil {
					t.Fatal(err)
				}
			} else {
				cancel()
			}
			awaitBootstrap(t, done, true)
			select {
			case <-r.pumpDone:
			case <-time.After(time.Second):
				t.Fatal("reader pump leaked")
			}
		})
	}
}

func TestBootstrapRejectsWrongGenerationBeforeWriting(t *testing.T) {
	r, guest := runtimePipe(t)
	request := bootstrapRequest()
	request.StartGeneration = "80c64529-fcb5-4789-8460-a43517622238"
	if _, err := r.Bootstrap(context.Background(), request); err == nil {
		t.Fatal("wrong generation accepted")
	}
	guest.SetReadDeadline(time.Now().Add(20 * time.Millisecond))
	var b [1]byte
	if n, _ := guest.Read(b[:]); n != 0 {
		t.Fatal("invalid request wrote command")
	}
}

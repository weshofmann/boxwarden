//go:build n1diagnostic && !n1candidate && darwin

package tart

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/weshofmann/boxwarden/internal/networkdiag"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Only this test binary's linker may select the separately preflighted native
// fixture. Production has no fixture/descriptor/path override.
var nativeFoundationFixture string

type nativeFDObservation struct {
	FD       int    `json:"fd"`
	Access   int    `json:"access"`
	Nonblock bool   `json:"nonblock"`
	Cloexec  bool   `json:"cloexec"`
	Kind     string `json:"kind"`
	Socktype int    `json:"socktype"`
	Socklen  int    `json:"socklen"`
	SunLen   int    `json:"sun_len"`
	ZeroPath bool   `json:"zero_path"`
}
type nativeObservation struct {
	Stage        string                `json:"stage"`
	Argv         []string              `json:"argv"`
	Environment  map[string]string     `json:"environment"`
	FDs          []nativeFDObservation `json:"fds"`
	ExtraSockets []int                 `json:"extra_sockets"`
}

func TestDiagnosticFoundationActualChildContract(t *testing.T) {
	if nativeFoundationFixture == "" {
		t.Skip("native fixture requires separately retained selected-tool/SDK preflight")
	}
	p, c, err := diagnosticSocketpair()
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	defer c.Close()
	p.SetDeadline(time.Now().Add(8 * time.Second))
	b := diagnosticBindingFixture()
	selector := "@boxwarden-n1-diagnostic:" + b.Generation + ":" + b.Nonce
	spec := processSpec{path: nativeFoundationFixture, args: []string{"run", "--net-softnet", "--net-softnet-block=" + selector, "--no-audio", "--no-clipboard", "--serial-path", "/dev/synthetic", "synthetic"}, dir: t.TempDir(), env: []string{"PATH=/synthetic/qualified/softnet", "HOME=/synthetic/operator", "USER=synthetic", "LOGNAME=synthetic", "TART_HOME=/synthetic/tart-home", "TMPDIR=/synthetic/private", "LANG=C", "LC_ALL=C"}}
	h, err := startDiagnosticProcess(t.Context(), spec, c)
	if h == nil || err != nil {
		t.Fatalf("actual spawn=%v %v", h, err)
	}
	defer func() { h.Stop(context.Background()); h.Wait(context.Background()) }()
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	expectedEnv := map[string]string{}
	for _, v := range spec.env {
		k, value, _ := strings.Cut(v, "=")
		expectedEnv[k] = value
	}
	expectedEnv["__CF_USER_TEXT_ENCODING"] = fmt.Sprintf("0x%X:0x0:0x0", os.Getuid())
	for i := 0; i < 2; i++ {
		raw, err := networkdiag.ReadFrame(p)
		if err != nil {
			t.Fatal("actual child observation", err)
		}
		var got nativeObservation
		if json.Unmarshal(raw, &got) != nil {
			t.Fatal("untyped fixture observation")
		}
		want := spec.args
		stage := "tart-stdio"
		stdin := "char"
		socketType := 0
		if i == 1 {
			want = []string{"--vm-fd", "0", "--vm-mac-address", "02:00:00:00:00:02", "--block", selector}
			stage = "foundation-child"
			stdin = "socket"
			socketType = 2
		}
		if got.Stage != stage || !reflect.DeepEqual(got.Argv, want) || !reflect.DeepEqual(got.Environment, expectedEnv) || len(got.FDs) != 3 || len(got.ExtraSockets) != 0 {
			t.Fatalf("child argv/environment/count/descriptor contract: %+v", got)
		}
		for fd, v := range got.FDs {
			if v.FD != fd || v.Cloexec {
				t.Fatalf("intended stdio missing: %+v", v)
			}
			if fd == 1 {
				if v.Kind != "socket" || v.Socktype != 1 || v.Access != 2 || !v.Nonblock || v.Socklen != 16 || v.SunLen != 16 || !v.ZeroPath {
					t.Fatalf("actual fd1 drift: %+v", v)
				}
			} else if fd == 0 {
				if v.Kind != stdin || v.Socktype != socketType {
					t.Fatalf("fd0 wrong role: %+v", v)
				}
			} else if v.Kind != "char" {
				t.Fatalf("fd2 not null: %+v", v)
			}
		}
		t.Logf("validated synthetic typed %s observation: argv_count=%d exact_argv_bytes=true spawn_environment=8_exact_keys observed_environment=9_exact_keys_including_CF_UID_encoding fd1=unnamed_Darwin16_RDWR_NONBLOCK_noCLOEXEC no_extra_socket_descriptors=true", stage, len(got.Argv))
	}
	if n, err := p.Write([]byte("PING")); err != nil || n != 4 {
		t.Fatal("full-duplex fixture write", err)
	}
	var reply [4]byte
	if _, err := io.ReadFull(p, reply[:]); err != nil || string(reply[:]) != "PONG" {
		t.Fatal("full-duplex fixture read", err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := h.Wait(ctx); err != nil {
		t.Fatal("owned native fixture reap", err)
	}
	_ = os.DevNull
}

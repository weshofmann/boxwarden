//go:build n1diagnostic && n1clipboarddiagnostic && !n1candidate

package coordinator

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/weshofmann/boxwarden/internal/config"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/contract"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/worker"
	"github.com/weshofmann/boxwarden/internal/supervisor"
	"io"
	"net"
	"os"
	"regexp"
	"strings"
	"time"
)

func admitRuntime(o worker.RuntimeObservation, i int, id worker.Identity) error {
	if i < 0 || i > 1 {
		return ErrRefused
	}
	now := uint64(time.Now().Unix())
	hex := regexp.MustCompile(`^[0-9a-f]{64}$`)
	b := o.Ready.Binding
	n := o.Network
	if o.Version != 1 || o.Role != role(i) || o.Name != roleName(i) || o.Identity.Session != id.Session || o.Identity.Backend != id.Backend || id.Generation != "" && o.Identity.Generation != id.Generation || !contract.UUID(o.Identity.Generation) || o.Identity.Generation == o.Identity.Session || !n.Valid() || b != (supervisor.Binding{Domain: config.N1Domain, SessionID: o.Identity.Session, BackendKind: "tart", BackendObject: o.Identity.Backend, Generation: o.Identity.Generation}) || !o.Ready.BackendRunning || !o.Ready.SerialHealthy || !o.Ready.PinPresent || !o.Ready.CertificateCurrent || !o.Ready.ProbeOK || !o.Ready.ZoneMatches || n.Binding.SessionID != o.Identity.Session || n.Binding.Generation != o.Identity.Generation || n.Binding.BackendObject != o.Identity.Backend || n.ConfigSHA256 != [2]string{contract.StockConfigSHA, contract.CandidateConfigSHA}[i] || !hex.MatchString(o.HostPinSHA) || !hex.MatchString(o.CASHA) || !hex.MatchString(o.Certificate.SHA256) || o.Connection.Port != 22 || o.Connection.Binding.SessionID != o.Identity.Session || o.Connection.Binding.BackendObject != o.Identity.Backend || o.Certificate.Principal != o.Connection.Binding.Principal() || o.Certificate.Identity != o.Connection.Binding.CertificateIdentity() || o.Certificate.NotBefore > now || o.Certificate.NotAfter <= now || o.Certificate.NotAfter-o.Certificate.NotBefore != 1200 || o.Certificate.CAFingerprint == "" || o.Connection.Binding.Domain != config.N1Domain || o.Connection.Binding.BackendKind != "tart" || o.Connection.Address != net.IP(n.Binding.Address[:]).String() || o.Identity.Backend != "boxwarden-n1qualification-"+strings.ReplaceAll(o.Identity.Session, "-", "") {
		return ErrRefused
	}
	runtime := contract.StateRoot + "/runtime/n1qualification/" + o.Identity.Session + "/" + o.Identity.Generation
	if o.Connection.RuntimeDirectory != runtime || o.Connection.IdentityFile != runtime+"/client" || o.Connection.CertificateFile != runtime+"/client-cert.pub" || o.Connection.KnownHostsFile != runtime+"/known_hosts" || o.PrivateKey.Path != runtime+"/client" || o.PrivateKey.UID != 501 || o.PrivateKey.Mode != 0100600 || o.PrivateKey.Links != 1 || o.PrivateKey.Size <= 0 || o.PrivateKey.Size > 4096 {
		return ErrRefused
	}
	if i == 1 && o.Launch != nil {
		if !contract.UUID(o.Launch.Nonce) || o.Launch.Owner.PID == 0 || o.Launch.Child.PID == 0 || o.Launch.Owner.BirthUS == 0 || o.Launch.Child.BirthUS == 0 || o.Launch.Owner.UniqueID == 0 || o.Launch.Child.UniqueID == 0 {
			return ErrRefused
		}
	}
	return nil
}
func distinct(p [2]worker.RuntimeObservation) bool {
	return p[0].Identity.Session != p[1].Identity.Session && p[0].Identity.Generation != p[1].Identity.Generation && p[0].Identity.Backend != p[1].Identity.Backend && p[0].Network.Binding.Address != p[1].Network.Binding.Address && p[0].Network.Binding.MAC != p[1].Network.Binding.MAC && p[0].CASHA == p[1].CASHA && p[1].Launch != nil
}

type runtimeReview struct {
	Version             int                          `json:"version"`
	Window              contract.Window              `json:"window"`
	Phase               string                       `json:"phase"`
	CatalogueSHA        string                       `json:"catalogue_sha"`
	CommandCatalogueSHA string                       `json:"command_catalogue_sha"`
	ProcedureSHA        string                       `json:"procedure_sha"`
	Pair                [2]worker.RuntimeObservation `json:"pair"`
}
type reviewAcceptance struct {
	Version          int    `json:"version"`
	Phase            string `json:"phase"`
	SnapshotSHA      string `json:"snapshot_sha"`
	IndependentHuman bool   `json:"independent_human"`
}

func (e *engine) review(phase string) error {
	return e.phase(phase+"-review", true, func(ctx context.Context, id string) (any, int, error) {
		for i := 0; i < 2; i++ {
			var current worker.RuntimeObservation
			if e.worker(ctx, i, "inspect", e.pair[i].Identity, nil, &current) != nil || admitRuntime(current, i, e.pair[i].Identity) != nil {
				return nil, 2, ErrRefused
			}
			e.pair[i] = current
		}
		if !distinct(e.pair) {
			return nil, 2, ErrRefused
		}
		snapshot := runtimeReview{1, e.window, phase, e.static.Lock.CatalogueSHA, e.static.Lock.Files[30].SHA, e.static.Lock.ProcedureSHA, e.pair}
		raw, err := json.Marshal(snapshot)
		if err != nil || len(raw) > contract.MaxReceiptBytes || e.archive.write("runtime-"+phase+".json", raw) != nil {
			return nil, 2, ErrRefused
		}
		sha := contract.SHA(raw)
		err = reviewTerminal(ctx, phase, sha, raw)
		return reviewAcceptance{1, phase, sha, err == nil}, 2, err
	})
}
func reviewTerminal(ctx context.Context, phase, sha string, snapshot []byte) error {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return ErrRefused
	}
	timer := time.AfterFunc(time.Until(deadlineOf(ctx)), func() { tty.Close() })
	defer timer.Stop()
	prompt := fmt.Sprintf("\nN1 %s runtime review within the original approved window. An independent human reviewer must inspect both fixed fresh quarantine sessions, distinct MAC/IP/session/backend/generation, exact READY/public pins/current no-extension certificates, retained process birth/nonce, current candidate HELLO unarmed, fixed command catalogue and approved static lock. Unknown images or current application consumers are refusal conditions; do not suppress them. This acceptance records review, not a new owner authorization. C cannot review its own observation. The approved single-window premise requires no concurrent lifecycle operation, fixed-name replacement, config replacement or state-root replacement through teardown and H.\nSnapshot SHA256 %s\n", phase, sha)
	_, e := tty.WriteString(prompt)
	if e == nil {
		_, e = tty.Write(append(snapshot, '\n'))
	}
	if e == nil {
		_, e = tty.WriteString("After independent review type exactly ACCEPT " + sha + " INDEPENDENT followed by Enter, or refuse.\n")
	}
	var line []byte
	if e == nil {
		line, e = readLine(tty, 128)
	}
	ce := tty.Close()
	if e != nil || ce != nil || ctx.Err() != nil || string(line) != "ACCEPT "+sha+" INDEPENDENT\n" {
		return ErrRefused
	}
	return nil
}
func deadlineOf(ctx context.Context) time.Time {
	d, ok := ctx.Deadline()
	if !ok {
		return time.Now()
	}
	return d
}
func readLine(r io.Reader, limit int) ([]byte, error) {
	b := make([]byte, 0, 128)
	for len(b) < limit {
		var x [1]byte
		n, e := r.Read(x[:])
		if n != 1 || e != nil {
			return nil, ErrRefused
		}
		b = append(b, x[0])
		if x[0] == '\n' {
			return b, nil
		}
	}
	return nil, ErrRefused
}

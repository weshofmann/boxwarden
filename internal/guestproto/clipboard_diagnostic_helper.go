//go:build n1clipboarddiagnostic

package guestproto

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/weshofmann/boxwarden/internal/clipboarddiag"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

// EncodeDiagnosticClipboardRequest wraps an unchanged production request frame.
func EncodeDiagnosticClipboardRequest(o clipboarddiag.Operation, r ClipboardRequest, payload []byte) ([]byte, error) {
	if o.Validate(true) != nil || !diagnosticRequestMatches(o, r) {
		return nil, clipboarddiag.ErrMetadata
	}
	header, err := clipboarddiag.EncodeOperation(o)
	if err != nil {
		return nil, clipboarddiag.ErrMetadata
	}
	frame, err := EncodeClipboardRequest(r, payload)
	if err != nil {
		return nil, clipboarddiag.ErrMetadata
	}
	return append(append(header, '\n'), frame...), nil
}
func DecodeDiagnosticClipboardRequest(input io.Reader) (clipboarddiag.Operation, ClipboardRequest, []byte, error) {
	raw, err := clipboarddiag.ReadHeader(input)
	if err != nil {
		return clipboarddiag.Operation{}, ClipboardRequest{}, nil, clipboarddiag.ErrMetadata
	}
	o, err := clipboarddiag.DecodeOperation(raw, true)
	if err != nil {
		return o, ClipboardRequest{}, nil, clipboarddiag.ErrMetadata
	}
	request, data, err := DecodeClipboardRequest(input)
	if err != nil || !diagnosticRequestMatches(o, request) {
		return o, ClipboardRequest{}, nil, clipboarddiag.ErrMetadata
	}
	return o, request, data, nil
}

// RunClipboardDiagnostic has exactly invoke/collect. Its production root and
// artifact paths are fixed by the distinct command, never caller-selected.
func RunClipboardDiagnostic(ctx context.Context, args []string, input io.Reader, output io.Writer) error {
	if len(args) != 1 || (args[0] != "invoke" && args[0] != "collect") {
		return clipboarddiag.ErrMetadata
	}
	b := NewBootstrapper("/", ExecRunner{})
	if args[0] == "collect" {
		return b.collectDiagnosticClipboard(ctx, input, output)
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	type decoded struct {
		o    clipboarddiag.Operation
		r    ClipboardRequest
		text []byte
		err  error
	}
	ready := make(chan decoded, 1)
	go func() { o, r, text, e := DecodeDiagnosticClipboardRequest(input); ready <- decoded{o, r, text, e} }()
	var message decoded
	select {
	case message = <-ready:
	case <-ctx.Done():
		return clipboarddiag.ErrMetadata
	}
	if message.err != nil {
		return clipboarddiag.ErrMetadata
	}
	o, request := message.o, message.r
	dir, created, err := b.admitDiagnosticNamespacePublication(o, true)
	if err != nil {
		return clipboarddiag.ErrMetadata
	}
	// A direction is a one-use operation, even if transfer later refuses. Reserve
	// its fixed bootstrap pending file before dispatch; never adopt failed residue.
	for _, suffix := range []string{".binding", ".trace", ".closure", ".collect", ".bootstrap"} {
		if _, e := os.Lstat(filepath.Join(dir, o.Direction+suffix)); !os.IsNotExist(e) {
			return clipboarddiag.ErrMetadata
		}
	}
	pending := filepath.Join(dir, o.Direction+".bootstrap.pending")
	stageFile, err := os.OpenFile(pending, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0600)
	if err != nil {
		return clipboarddiag.ErrMetadata
	}
	recorder, err := clipboarddiag.NewRecorder(o, "guest", time.Now)
	if err != nil {
		stageFile.Close()
		return clipboarddiag.ErrMetadata
	}
	diagnosticCtx, err := clipboarddiag.WithContext(ctx, o, recorder)
	if err != nil {
		stageFile.Close()
		return clipboarddiag.ErrMetadata
	}
	clipboarddiag.Record(clipboarddiag.WithSource(diagnosticCtx, "helper"), "helper_namespace", "ok")
	publication := &diagnosticInvokePublication{operation: o, created: created, namespaceReturned: true}
	executor := newDiagnosticClipboardExecutor(o, dir)
	executor.publication = publication
	b.ClipboardExecutor = executor
	response, text, operationErr := b.Clipboard(diagnosticCtx, request, message.text)
	var encoded []byte
	if operationErr == nil {
		encoded, err = EncodeClipboardResponse(request, response, text)
		if err != nil {
			operationErr = clipboarddiag.ErrMetadata
		}
	}
	if operationErr == nil {
		clipboarddiag.Record(clipboarddiag.WithSource(diagnosticCtx, "helper"), "helper_response", "ok")
	} else {
		recorder.Invalidate()
		clipboarddiag.Record(clipboarddiag.WithSource(diagnosticCtx, "helper"), "helper_response", "incomplete")
	}
	// Persist only typed stage metadata. Publication failure is ancillary after
	// dispatch; it cannot replace or replay the original acknowledgement/outcome.
	fragment := recorder.Fragment()
	stageRaw, stageErr := json.Marshal(fragment)
	if stageErr == nil && len(stageRaw)+1 <= clipboarddiag.MaxFragmentBytes {
		// Failure leaves the existing pending guard/one-use reservation, while the
		// original acknowledgement remains authoritative and is never replayed.
		stageRaw = append(stageRaw, '\n')
		publicationErr := finalizeDiagnosticPending(dir, o.Direction+".bootstrap", stageFile, stageRaw, os.Link)
		if publicationErr != nil {
			recorder.Invalidate()
		} else {
			publication.bootstrapRaw = stageRaw
			publication.bootstrapReturned = true
		}
		publication.emit(os.Stderr)
	} else {
		stageFile.Close()
		recorder.Invalidate()
	}

	if operationErr != nil {
		return clipboarddiag.ErrMetadata
	}
	return writeDiagnosticExact(output, encoded)
}
func writeDiagnosticExact(output io.Writer, raw []byte) error {
	n, err := output.Write(raw)
	if err != nil || n != len(raw) {
		return clipboarddiag.ErrMetadata
	}
	return nil
}
func (b *Bootstrapper) collectDiagnosticClipboard(parent context.Context, input io.Reader, output io.Writer) error {
	ctx, cancel := context.WithTimeout(parent, 250*time.Millisecond)
	defer cancel()
	type intake struct {
		raw []byte
		err error
	}
	ready := make(chan intake, 1)
	go func() {
		raw, err := io.ReadAll(io.LimitReader(input, clipboarddiag.MaxHeaderBytes+1))
		ready <- intake{raw, err}
	}()
	var raw []byte
	select {
	case message := <-ready:
		if message.err != nil {
			return clipboarddiag.ErrMetadata
		}
		raw = message.raw
	case <-ctx.Done():
		return clipboarddiag.ErrMetadata
	}
	o, err := clipboarddiag.DecodeOperation(raw, false)
	if err != nil {
		return clipboarddiag.ErrMetadata
	}
	dir, err := b.admitDiagnosticNamespace(o, false)
	if err != nil {
		return clipboarddiag.ErrMetadata
	}
	headerRaw, err := readDiagnosticFile(filepath.Join(dir, o.Direction+".binding"), clipboarddiag.MaxHeaderBytes)
	if err != nil || !bytes.Equal(headerRaw, raw) {
		return clipboarddiag.ErrMetadata
	}
	// Collection is independently one-use, bound to the existing exact header.
	marker := []byte(clipboarddiag.HeaderDigest(o) + "\n")
	if publishDiagnosticFile(dir, o.Direction+".collect", marker, os.Link) != nil {
		return clipboarddiag.ErrMetadata
	}
	result := clipboarddiag.GuestCollection{Version: 1, Binding: o}
	bootstrapRaw, err := readDiagnosticFile(filepath.Join(dir, o.Direction+".bootstrap"), clipboarddiag.MaxFragmentBytes)
	if err == nil {
		var fragment clipboarddiag.Fragment
		if clipboarddiag.StrictDecode(bootstrapRaw, &fragment, clipboarddiag.MaxFragmentBytes) == nil && fragment.Validate() == nil && fragment.Binding == o && fragment.Origin == "guest" {
			result.Bootstrap = &fragment
		}
	}
	for _, dir := range []string{"/usr", "/usr/local", "/usr/local/libexec"} {
		if safeDirectory(dir, 0755) != nil {
			return clipboarddiag.ErrMetadata
		}
	}
	if _, err := readSafeFile(ClipboardHelperPath, 0755); err != nil {
		return clipboarddiag.ErrMetadata
	}
	mr, mw, err := os.Pipe()
	if err != nil {
		return clipboarddiag.ErrMetadata
	}
	defer mr.Close()
	n, e := mw.Write(raw)
	ce := mw.Close()
	if e != nil || n != len(raw) || ce != nil {
		return clipboarddiag.ErrMetadata
	}
	command := exec.CommandContext(ctx, "/usr/bin/python3", ClipboardHelperPath, "collect", o.Direction)
	command.ExtraFiles = []*os.File{mr}
	command.Env = []string{"HOME=/root", "USER=root", "LOGNAME=root", "PATH=/usr/bin:/bin", "LANG=C.UTF-8"}
	command.Dir = "/"
	command.Stdin = bytes.NewReader(nil)
	command.Stderr = nil
	command.WaitDelay = 250 * time.Millisecond
	var collector boundedDiagnosticOutput
	command.Stdout = &collector
	runErr := command.Run()
	// Native collector uses its own fixed250ms budget; outer timeout includes its
	// actual launch/read/wait. It never lends the transfer a new expiry.
	if runErr == nil && !collector.overflow && ctx.Err() == nil {
		overlay, decodeErr := clipboarddiag.DecodeOverlay(collector.Bytes(), o)
		if decodeErr == nil && (overlay.Closure == nil || diagnosticPublicationCommitted(filepath.Join(dir, o.Direction+".closure"))) {
			result.Overlay = &overlay
		}
	}
	result.Complete = result.Bootstrap != nil && result.Bootstrap.Complete && result.Overlay != nil && result.Overlay.Complete
	if result.Validate(o) != nil {
		return clipboarddiag.ErrMetadata
	}
	encoded, err := json.Marshal(result)
	if err != nil || len(encoded)+1 > clipboarddiag.MaxCollectionBytes {
		return clipboarddiag.ErrMetadata
	}
	emitDiagnosticCollectionPublication(os.Stderr, o, nil)
	return writeDiagnosticExact(output, append(encoded, '\n'))
}

type boundedDiagnosticOutput struct {
	bytes.Buffer
	overflow bool
}

func (b *boundedDiagnosticOutput) Write(raw []byte) (int, error) {
	if b.Len()+len(raw) > 16384 {
		b.overflow = true
		return 0, clipboarddiag.ErrMetadata
	}
	return b.Buffer.Write(raw)
}

// Metadata publication return state is private and never determines ACK bytes.
type diagnosticInvokePublication struct {
	operation                                                      clipboarddiag.Operation
	created, namespaceReturned, closureReturned, bootstrapReturned bool
	closureRaw, bootstrapRaw                                       []byte
}

func (p *diagnosticInvokePublication) emit(output io.Writer) {
	if p == nil || !p.namespaceReturned || !p.closureReturned || !p.bootstrapReturned {
		return
	}
	bootstrap := clipboarddiag.MetadataDigest(p.bootstrapRaw)
	closure := clipboarddiag.MetadataDigest(p.closureRaw)
	emitDiagnosticPublication(output, clipboarddiag.PublicationWitness{V: 1, Phase: "invoke", Header: clipboarddiag.HeaderDigest(p.operation), Generation: clipboarddiag.GenerationDigest(p.operation), Created: p.created, Bootstrap: &bootstrap, Closure: &closure})
}
func emitDiagnosticPublication(output io.Writer, w clipboarddiag.PublicationWitness) {
	raw, e := clipboarddiag.EncodePublication(w)
	if e == nil {
		_ = writeDiagnosticExact(output, raw)
	}
}

func emitDiagnosticCollectionPublication(output io.Writer, o clipboarddiag.Operation, publicationErr error) {
	if publicationErr != nil {
		return
	}
	digest := clipboarddiag.MetadataDigest([]byte(clipboarddiag.HeaderDigest(o) + "\n"))
	emitDiagnosticPublication(output, clipboarddiag.PublicationWitness{V: 1, Phase: "collect", Header: clipboarddiag.HeaderDigest(o), Generation: clipboarddiag.GenerationDigest(o), Collect: &digest})
}

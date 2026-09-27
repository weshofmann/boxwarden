package serialx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

const (
	installerReadyMarker    = "generic golden clone-ready; power off without another boot"
	installerPreparedMarker = "boxwarden recipe prepare complete"
	installerFailureMarker  = "boxwarden recipe prepare failed:"
	installerPrepare        = "sudo -n -- /usr/bin/python3 /usr/local/libexec/boxwarden-recipe-prepare --run"
	installerFinalizer      = "sudo -n -- /usr/local/libexec/boxwarden-finalize-golden --acknowledge-generic-golden-finalization"
	installerPoweroff       = "sudo -n -- /usr/bin/systemctl poweroff"
	installerTailBytes      = 256
)

type installerExchange struct {
	tail                                               []byte
	expectedPrompt                                     string
	seenPrompt, seenPrepared, prepareFailed, seenReady bool
	prepareSent, finalizerSent, poweroffSent           bool
	wake                                               chan struct{}
	writeMu                                            sync.Mutex
}

func newInstallerExchange(runID string) *installerExchange {
	return &installerExchange{wake: make(chan struct{}), expectedPrompt: "boxwarden@boxwarden-task0-" + runID + ":"}
}

func validInstallerRunID(value string) bool {
	if value == "run-1" || value == "run-2" {
		return true
	}
	if len(value) != 16 || value[:4] != "run-" {
		return false
	}
	for _, c := range value[4:] {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

// feed is called only by Runtime's sole PTY reader while holding Runtime.mu.
func (e *installerExchange) feed(chunk []byte) {
	joined := make([]byte, 0, len(e.tail)+len(chunk))
	joined = append(joined, e.tail...)
	joined = append(joined, chunk...)
	changed := false
	if !e.seenPrompt && bytes.Contains(joined, []byte(e.expectedPrompt)) {
		e.seenPrompt, changed = true, true
	}
	if e.prepareSent && !e.seenPrepared && bytes.Contains(joined, []byte(installerPreparedMarker)) {
		e.seenPrepared, changed = true, true
	}
	if e.prepareSent && !e.prepareFailed && bytes.Contains(joined, []byte(installerFailureMarker)) {
		e.prepareFailed, changed = true, true
	}
	if e.finalizerSent && !e.seenReady && bytes.Contains(joined, []byte(installerReadyMarker)) {
		e.seenReady, changed = true, true
	}
	if len(joined) > installerTailBytes {
		joined = joined[len(joined)-installerTailBytes:]
	}
	e.tail = joined
	if changed {
		e.notify()
	}
}

func (e *installerExchange) notify() {
	close(e.wake)
	e.wake = make(chan struct{})
}

// InstallerWaitFor recognizes only the bound prompt and fixed preparation and
// clone-ready markers. The builder owns each phase's finite deadline; this
// transport must not shorten it with an independent timeout. It never returns
// console text to a caller.
func (r *Runtime) InstallerWaitFor(ctx context.Context, marker string) error {
	if r == nil || r.installer == nil {
		return errors.New("installer serial runtime is required")
	}
	switch marker {
	case r.installer.expectedPrompt:
	case installerPreparedMarker:
	case installerReadyMarker:
	default:
		return errors.New("unrecognized installer marker")
	}
	if _, bounded := ctx.Deadline(); !bounded {
		return errors.New("installer marker wait requires caller deadline")
	}
	for {
		r.mu.Lock()
		e := r.installer
		seen := marker == e.expectedPrompt && e.seenPrompt || marker == installerPreparedMarker && e.seenPrepared || marker == installerReadyMarker && e.seenReady
		failed := marker == installerPreparedMarker && e.prepareFailed
		err := r.err
		wake := e.wake
		r.mu.Unlock()
		if err != nil {
			return err
		}
		if failed {
			return errors.New("guest preparation reported failure")
		}
		if seen {
			return nil
		}
		select {
		case <-wake:
		case <-ctx.Done():
			r.fail(fmt.Errorf("wait for installer marker: %w", ctx.Err()))
			return r.Err()
		}
	}
}

// InstallerSendLine permits only the tracked prepare, finalizer, and poweroff
// commands in order, once each. A timeout poisons the sole serial transport.
func (r *Runtime) InstallerSendLine(ctx context.Context, line string) error {
	if r == nil || r.installer == nil {
		return errors.New("installer serial runtime is required")
	}
	e := r.installer
	e.writeMu.Lock()
	defer e.writeMu.Unlock()
	r.mu.Lock()
	if r.err != nil {
		err := r.err
		r.mu.Unlock()
		return err
	}
	switch line {
	case installerPrepare:
		if !e.seenPrompt || e.prepareSent {
			r.mu.Unlock()
			return errors.New("installer preparation is out of order")
		}
		e.prepareSent = true
		e.tail = nil
	case installerFinalizer:
		if !e.seenPrepared || e.prepareFailed || e.finalizerSent {
			r.mu.Unlock()
			return errors.New("installer finalizer is out of order")
		}
		e.finalizerSent = true
		e.tail = nil // A pre-finalizer marker cannot satisfy the later wait.
	case installerPoweroff:
		if !e.seenReady || e.poweroffSent {
			r.mu.Unlock()
			return errors.New("installer poweroff is out of order")
		}
		e.poweroffSent = true
	default:
		r.mu.Unlock()
		return errors.New("unrecognized installer command")
	}
	r.mu.Unlock()
	writeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	written := make(chan error, 1)
	go func() { written <- writeAll(r.stream, []byte(line+"\n")) }()
	select {
	case err := <-written:
		if err != nil {
			r.fail(fmt.Errorf("write installer command: %w", err))
			return r.Err()
		}
		return nil
	case <-writeCtx.Done():
		r.fail(fmt.Errorf("write installer command: %w", writeCtx.Err()))
		<-written
		return r.Err()
	}
}

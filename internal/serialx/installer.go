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
	installerReadyMarker = "generic golden clone-ready; power off without another boot"
	installerFinalizer   = "sudo -n -- /usr/local/libexec/boxwarden-finalize-golden --acknowledge-generic-golden-finalization"
	installerPoweroff    = "sudo -n -- /usr/bin/systemctl poweroff"
	installerTailBytes   = 256
)

type installerExchange struct {
	tail                        []byte
	expectedPrompt              string
	seenPrompt, seenReady       bool
	finalizerSent, poweroffSent bool
	wake                        chan struct{}
	writeMu                     sync.Mutex
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

// InstallerWaitFor recognizes only the bound run prompt and the fixed
// clone-ready marker. It never returns console text to a caller.
func (r *Runtime) InstallerWaitFor(ctx context.Context, marker string) error {
	if r == nil || r.installer == nil {
		return errors.New("installer serial runtime is required")
	}
	var limit time.Duration
	switch marker {
	case r.installer.expectedPrompt:
		limit = 90 * time.Minute
	case installerReadyMarker:
		limit = 10 * time.Minute
	default:
		return errors.New("unrecognized installer marker")
	}
	waitCtx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	for {
		r.mu.Lock()
		e := r.installer
		seen := marker == e.expectedPrompt && e.seenPrompt || marker == installerReadyMarker && e.seenReady
		err := r.err
		wake := e.wake
		r.mu.Unlock()
		if err != nil {
			return err
		}
		if seen {
			return nil
		}
		select {
		case <-wake:
		case <-waitCtx.Done():
			r.fail(fmt.Errorf("wait for installer marker: %w", waitCtx.Err()))
			return r.Err()
		}
	}
}

// InstallerSendLine permits only the tracked finalizer and poweroff commands
// in order, once each. A timeout poisons the sole serial transport.
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
	case installerFinalizer:
		if !e.seenPrompt || e.finalizerSent {
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

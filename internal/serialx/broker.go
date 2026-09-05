// Package serialx owns the deliberately small, bounded host-local serial path.
package serialx

import (
	"errors"
	"fmt"
	"io"
	"sync"
	"time"
)

var ErrPoisoned = errors.New("serial transport is poisoned")

const (
	MaxScreenQueueBytes = 256 << 10
	// The Task 2 end frame carries up to a 64 KiB result as base64, so an 8 KiB
	// physical line bound would reject the canonical protocol itself.
	MaxPhysicalLineBytes = 128 << 10
	MaxFrameBytes        = 128 << 10
	MaxExchangeBytes     = 256 << 10
	ExchangeDeadline     = 30 * time.Second
)

type State string

const (
	StateIdle       State = "idle"
	StateConsole    State = "console"
	StateAutomation State = "automation"
	StateFailed     State = "failed"
)

type BrokerConfig struct {
	Tart, Screen io.Writer
	Generation   string
	Clock        Clock
}
type Broker struct {
	mu                     sync.Mutex
	tart, screen           io.Writer
	generation             string
	clock                  Clock
	state                  State
	discardedInput         uint64
	screenQueue            []byte
	draining               bool
	exchange               *activeExchange
	leaseID, activeLeaseID uint64
	changed                chan struct{}
	poisonCause            error
}

func NewBroker(config BrokerConfig) *Broker {
	clock := config.Clock
	if clock == nil {
		clock = systemClock{}
	}
	return &Broker{tart: config.Tart, screen: config.Screen, generation: config.Generation, clock: clock, state: StateIdle, changed: make(chan struct{})}
}
func (b *Broker) State() State           { b.mu.Lock(); defer b.mu.Unlock(); return b.state }
func (b *Broker) InputDiscarded() uint64 { b.mu.Lock(); defer b.mu.Unlock(); return b.discardedInput }
func (b *Broker) Poisoned() bool         { return b.State() == StateFailed }
func (b *Broker) OperatorInput(input []byte) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.state != StateConsole || b.tart == nil {
		b.discardedInput += uint64(len(input))
		return
	}
	if err := writeAll(b.tart, input); err != nil {
		b.poisonLocked(fmt.Errorf("forward console input: %w", err))
	}
}
func (b *Broker) TartOutput(output []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.state == StateFailed {
		return ErrPoisoned
	}
	if err := b.enqueueScreenLocked(output); err != nil {
		b.failExchangeLocked(err)
		b.poisonLocked(err)
		return ErrPoisoned
	}
	if b.state == StateAutomation && b.exchange != nil {
		if err := b.feedParserLocked(b.exchange, output); err != nil {
			b.failExchangeLocked(err)
			b.poisonLocked(err)
			return ErrPoisoned
		}
	}
	return nil
}

// ChildLost is called only for the direct owned Screen child. There is no
// process discovery fallback: an unavailable direct child poisons this generation.
func (b *Broker) ChildLost(err error) {
	if err == nil {
		err = errors.New("screen child exited")
	}
	b.poison(fmt.Errorf("screen child lost: %w", err))
}
func (b *Broker) poison(err error) { b.mu.Lock(); defer b.mu.Unlock(); b.poisonLocked(err) }
func (b *Broker) poisonLocked(err error) {
	if b.state != StateFailed {
		b.state = StateFailed
		b.poisonCause = err
		b.notifyLocked()
	}
}
func (b *Broker) notifyLocked() { close(b.changed); b.changed = make(chan struct{}) }
func (b *Broker) failExchangeLocked(err error) {
	if b.exchange == nil || b.exchange.finished {
		return
	}
	b.exchange.finished = true
	select {
	case b.exchange.result <- exchangeResult{err: err}:
	default:
	}
}
func (b *Broker) enqueueScreenLocked(output []byte) error {
	if b.screen == nil {
		return fmt.Errorf("screen writer is required")
	}
	if len(output) > MaxScreenQueueBytes-len(b.screenQueue) {
		return fmt.Errorf("screen output queue exceeds %d bytes", MaxScreenQueueBytes)
	}
	b.screenQueue = append(b.screenQueue, output...)
	if !b.draining {
		b.draining = true
		go b.drainScreen()
	}
	return nil
}

// drainScreen executes outside Broker.mu. A stopped Screen writer can consume
// only the fixed queue; it must never wedge the Tart-master reader.
func (b *Broker) drainScreen() {
	for {
		b.mu.Lock()
		if b.state == StateFailed || len(b.screenQueue) == 0 {
			b.draining = false
			b.mu.Unlock()
			return
		}
		queued, screen := b.screenQueue, b.screen
		b.mu.Unlock()

		n, err := screen.Write(queued)
		b.mu.Lock()
		if n < 0 || n > len(b.screenQueue) {
			b.poisonLocked(fmt.Errorf("invalid screen write count %d", n))
			b.mu.Unlock()
			return
		}
		b.screenQueue = b.screenQueue[n:]
		if err != nil {
			b.poisonLocked(fmt.Errorf("write screen output: %w", err))
			b.mu.Unlock()
			return
		}
		if n == 0 {
			b.poisonLocked(io.ErrShortWrite)
			b.mu.Unlock()
			return
		}
		b.mu.Unlock()
	}
}
func writeAll(writer io.Writer, input []byte) error {
	for len(input) != 0 {
		n, err := writer.Write(input)
		if n < 0 || n > len(input) {
			return fmt.Errorf("invalid write count %d", n)
		}
		input = input[n:]
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}

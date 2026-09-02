// Package serialx provides the bounded, host-local serial transport used by a
// supervisor. It deliberately has no dependency on guest protocol packages.
package serialx

import (
	"errors"
	"fmt"
	"io"
	"sync"
	"time"
)

var (
	ErrPoisoned = errors.New("serial transport is poisoned")
)

const (
	// MaxScreenQueueBytes is the fixed amount of guest output that may await
	// delivery to the retained Screen child.
	MaxScreenQueueBytes  = 256 << 10
	MaxPhysicalLineBytes = 8 << 10
	MaxDecodedFrameBytes = 64 << 10
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

// BrokerConfig contains only capability-limited host I/O endpoints. Tart is
// written with console input and bootstrap bytes; Screen receives queued guest
// output. Readers are driven through ReadTart and ReadOperator so tests never
// need a live PTY.
type BrokerConfig struct {
	Tart       io.Writer
	Screen     io.Writer
	Generation string
	Clock      Clock
}

// Broker serializes all state transitions and never retains operator input.
type Broker struct {
	mu             sync.Mutex
	tart           io.Writer
	screen         io.Writer
	state          State
	discardedInput uint64
	poisonCause    error
	screenQueue    []byte
	generation     string
	clock          Clock
	exchange       *activeExchange
	changed        chan struct{}
	leaseID        uint64
	activeLeaseID  uint64
}

func NewBroker(config BrokerConfig) *Broker {
	clock := config.Clock
	if clock == nil {
		clock = systemClock{}
	}
	return &Broker{tart: config.Tart, screen: config.Screen, generation: config.Generation, clock: clock, state: StateIdle, changed: make(chan struct{})}
}

func (b *Broker) State() State {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.state
}

func (b *Broker) InputDiscarded() uint64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.discardedInput
}

func (b *Broker) Poisoned() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.state == StateFailed
}

// OperatorInput forwards bytes only during an active console lease. All
// other modes count and forget the bytes immediately: a later lease cannot
// replay them into a guest.
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

func (b *Broker) poisonLocked(err error) {
	if b.state != StateFailed {
		b.state = StateFailed
		b.poisonCause = err
		b.notifyLocked()
	}
}

func (b *Broker) notifyLocked() {
	close(b.changed)
	b.changed = make(chan struct{})
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

package serialx

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/weshofmann/boxwarden/internal/guestproto"
)

const bootstrapCommand = "/usr/bin/sudo -n -- /usr/local/libexec/boxwarden-guest-bootstrap serial-bootstrap\n"

type ExchangeRequest struct{ Request guestproto.SerialRequest }
type activeExchange struct {
	request           guestproto.SerialRequest
	partial           []byte
	total             int
	started, finished bool
	result            chan exchangeResult
}
type exchangeResult struct {
	payload json.RawMessage
	err     error
}

func (b *Broker) Exchange(ctx context.Context, request ExchangeRequest) (json.RawMessage, error) {
	if err := b.validateRequest(request); err != nil {
		return nil, err
	}
	lease, err := b.acquire(ctx, StateAutomation)
	if err != nil {
		return nil, err
	}
	b.mu.Lock()
	if b.exchange != nil || b.state != StateAutomation {
		b.poisonLocked(fmt.Errorf("automation parser state conflict"))
		b.mu.Unlock()
		return nil, ErrPoisoned
	}
	exchange := &activeExchange{request: request.Request, result: make(chan exchangeResult, 1)}
	b.exchange = exchange
	b.mu.Unlock()
	defer func() { b.finishExchange(exchange, lease) }()
	encoded, err := json.Marshal(request.Request)
	if err != nil || len(encoded) > guestproto.MaxRequestBytes {
		b.poison(fmt.Errorf("canonical request is invalid or exceeds frame bound"))
		return nil, ErrPoisoned
	}
	if err := writeAll(b.tart, []byte(bootstrapCommand)); err != nil {
		b.poison(fmt.Errorf("write bootstrap command: %w", err))
		return nil, ErrPoisoned
	}
	if err := writeAll(b.tart, append(encoded, '\n')); err != nil {
		b.poison(fmt.Errorf("write bootstrap request: %w", err))
		return nil, ErrPoisoned
	}
	select {
	case result := <-exchange.result:
		if result.err != nil {
			b.poison(result.err)
			return nil, ErrPoisoned
		}
		return result.payload, nil
	case <-ctx.Done():
		b.poison(fmt.Errorf("automation canceled: %w", ctx.Err()))
		return nil, ErrPoisoned
	case <-b.clock.After(ExchangeDeadline):
		b.poison(fmt.Errorf("automation deadline exceeded"))
		return nil, ErrPoisoned
	}
}
func (b *Broker) finishExchange(exchange *activeExchange, lease Lease) {
	b.mu.Lock()
	if b.exchange == exchange {
		b.exchange = nil
		if b.state == StateAutomation {
			b.state = StateIdle
			b.activeLeaseID = 0
			b.notifyLocked()
		}
	}
	b.mu.Unlock()
	_ = lease.Close()
}
func (b *Broker) validateRequest(request ExchangeRequest) error {
	if b.tart == nil {
		return fmt.Errorf("tart writer is required")
	}
	if err := request.Request.Validate(); err != nil {
		return fmt.Errorf("invalid serial request: %w", err)
	}
	if request.Request.StartGeneration != b.generation {
		return fmt.Errorf("exchange generation does not match current broker generation")
	}
	return nil
}
func (b *Broker) feedParserLocked(exchange *activeExchange, output []byte) error {
	if len(output) > MaxExchangeBytes-exchange.total {
		return fmt.Errorf("automation response exceeds %d bytes", MaxExchangeBytes)
	}
	exchange.total += len(output)
	for len(output) != 0 {
		index := bytes.IndexByte(output, '\n')
		if index < 0 {
			if len(exchange.partial)+len(output) > MaxPhysicalLineBytes {
				return fmt.Errorf("serial line exceeds %d bytes", MaxPhysicalLineBytes)
			}
			exchange.partial = append(exchange.partial, output...)
			return nil
		}
		if len(exchange.partial)+index > MaxPhysicalLineBytes {
			return fmt.Errorf("serial line exceeds %d bytes", MaxPhysicalLineBytes)
		}
		line := append(exchange.partial, output[:index]...)
		exchange.partial = exchange.partial[:0]
		output = output[index+1:]
		if err := b.parseLineLocked(exchange, line); err != nil {
			return err
		}
	}
	return nil
}
func (b *Broker) parseLineLocked(exchange *activeExchange, line []byte) error {
	text := string(line)
	if !strings.HasPrefix(text, "BOXWARDEN-") {
		if exchange.started && !exchange.finished && len(line) != 0 {
			return fmt.Errorf("serial frame was interleaved")
		}
		return nil
	}
	fields := strings.Split(text, " ")
	switch fields[0] {
	case "BOXWARDEN-BEGIN":
		// A PTY may translate the helper's newline to CRLF. Normalize exactly
		// that terminal CR at this frame boundary; no payload bytes are touched.
		if strings.HasSuffix(text, "\r") {
			text = strings.TrimSuffix(text, "\r")
			if strings.ContainsAny(text, "\r\n") {
				return fmt.Errorf("invalid serial begin frame line ending")
			}
			fields = strings.Split(text, " ")
		}
		if len(fields) != 3 || fields[1] != exchange.request.Nonce || fields[2] != exchange.request.SessionID || exchange.started {
			return fmt.Errorf("ambiguous or mismatched begin frame")
		}
		exchange.started = true
		return nil
	case "BOXWARDEN-END":
		if !exchange.started || exchange.finished {
			return fmt.Errorf("ambiguous or mismatched end frame")
		}
		if len(line) > MaxFrameBytes {
			return fmt.Errorf("serial frame exceeds %d bytes", MaxFrameBytes)
		}
		result, err := guestproto.DecodeSerialEndLine(exchange.request, text)
		if err != nil {
			return fmt.Errorf("invalid serial end frame: %w", err)
		}
		payload, err := json.Marshal(result)
		if err != nil {
			return fmt.Errorf("encode serial result: %w", err)
		}
		exchange.finished = true
		exchange.result <- exchangeResult{payload: payload}
		return nil
	default:
		return fmt.Errorf("unexpected serial control frame")
	}
}

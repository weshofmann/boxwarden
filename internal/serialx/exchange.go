package serialx

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

const bootstrapCommand = "/usr/bin/sudo -n -- /usr/local/libexec/boxwarden-guest-bootstrap serial-bootstrap\n"

// Association is the durable identity an exchange must echo. Serialx checks
// only its exact bounded equality; callers own any stronger field semantics.
type Association struct {
	Domain  string `json:"domain"`
	Session string `json:"session"`
	Backend string `json:"backend"`
}

// ExchangeRequest is serialized locally as one canonical JSON line. Nonce and
// Generation are correlation data only; serialx never persists them.
type ExchangeRequest struct {
	Nonce       string
	Generation  string
	Association Association
	Payload     json.RawMessage
}

type exchangeRequestEnvelope struct {
	Association Association     `json:"association"`
	Generation  string          `json:"generation"`
	Nonce       string          `json:"nonce"`
	Payload     json.RawMessage `json:"payload"`
}

type exchangeResultEnvelope struct {
	Association Association     `json:"association"`
	Generation  string          `json:"generation"`
	Nonce       string          `json:"nonce"`
	Payload     json.RawMessage `json:"payload"`
}

type activeExchange struct {
	request  ExchangeRequest
	partial  []byte
	total    int
	started  bool
	finished bool
	result   chan exchangeResult
}

type exchangeResult struct {
	payload json.RawMessage
	err     error
}

// Exchange takes the exclusive automation lease, arms the parser before any
// write, and accepts precisely one associated begin/end response pair.
func (b *Broker) Exchange(ctx context.Context, request ExchangeRequest) (json.RawMessage, error) {
	if err := b.validateRequest(request); err != nil {
		return nil, err
	}
	lease, err := b.acquire(ctx, StateAutomation)
	if err != nil {
		return nil, err
	}
	defer lease.Close()

	b.mu.Lock()
	if b.exchange != nil || b.state != StateAutomation {
		b.poisonLocked(fmt.Errorf("automation parser state conflict"))
		b.mu.Unlock()
		return nil, ErrPoisoned
	}
	exchange := &activeExchange{request: request, result: make(chan exchangeResult, 1)}
	b.exchange = exchange
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		if b.exchange == exchange {
			b.exchange = nil
		}
		b.mu.Unlock()
	}()

	encoded, err := canonicalJSON(exchangeRequestEnvelope{
		Association: request.Association,
		Generation:  request.Generation,
		Nonce:       request.Nonce,
		Payload:     request.Payload,
	})
	if err != nil || len(encoded)+1 > MaxDecodedFrameBytes {
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

func (b *Broker) validateRequest(request ExchangeRequest) error {
	if b.tart == nil {
		return fmt.Errorf("tart writer is required")
	}
	if request.Generation == "" || request.Generation != b.generation {
		return fmt.Errorf("exchange generation does not match current broker generation")
	}
	if !validToken(request.Nonce) || !validAssociation(request.Association) {
		return fmt.Errorf("exchange has invalid association or nonce")
	}
	payload, err := canonicalJSON(request.Payload)
	if err != nil || len(payload) > MaxDecodedFrameBytes {
		return fmt.Errorf("exchange payload is invalid or exceeds frame bound")
	}
	return nil
}

func validToken(value string) bool {
	return value != "" && len(value) <= 512 && !strings.ContainsAny(value, " \t\r\n")
}

func validAssociation(value Association) bool {
	return validToken(value.Domain) && validToken(value.Session) && validToken(value.Backend)
}

func (b *Broker) poison(err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.poisonLocked(err)
}

// TartOutput is called only by the supervisor-owned Tart-master reader. It
// feeds Screen's bounded queue and, while armed, the separate raw parser.
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
	if len(output) > MaxScreenQueueBytes-len(b.screenQueue) {
		return fmt.Errorf("screen output queue exceeds %d bytes", MaxScreenQueueBytes)
	}
	b.screenQueue = append(b.screenQueue, output...)
	if b.screen == nil {
		return nil
	}
	for len(b.screenQueue) > 0 {
		n, err := b.screen.Write(b.screenQueue)
		if n < 0 || n > len(b.screenQueue) {
			return fmt.Errorf("invalid screen write count %d", n)
		}
		b.screenQueue = b.screenQueue[n:]
		if err != nil {
			return fmt.Errorf("write screen output: %w", err)
		}
		if n == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}

func (b *Broker) feedParserLocked(exchange *activeExchange, output []byte) error {
	if len(output) > MaxExchangeBytes-exchange.total {
		return fmt.Errorf("automation response exceeds %d bytes", MaxExchangeBytes)
	}
	exchange.total += len(output)
	for len(output) > 0 {
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
		if exchange.started || len(fields) != 3 || fields[1] != exchange.request.Nonce || fields[2] != exchange.request.Association.Session {
			return fmt.Errorf("ambiguous or mismatched begin frame")
		}
		exchange.started = true
		return nil
	case "BOXWARDEN-END":
		if !exchange.started || exchange.finished || len(fields) != 4 || fields[1] != exchange.request.Nonce || fields[2] != exchange.request.Association.Session {
			return fmt.Errorf("ambiguous or mismatched end frame")
		}
		decoded, err := base64.StdEncoding.DecodeString(fields[3])
		if err != nil || len(decoded) > MaxDecodedFrameBytes {
			return fmt.Errorf("invalid or oversized end frame")
		}
		payload, err := validateResult(decoded, exchange.request)
		if err != nil {
			return err
		}
		exchange.finished = true
		exchange.result <- exchangeResult{payload: payload}
		return nil
	default:
		return fmt.Errorf("unexpected serial control frame")
	}
}

func validateResult(decoded []byte, request ExchangeRequest) (json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(decoded))
	decoder.DisallowUnknownFields()
	var result exchangeResultEnvelope
	if err := decoder.Decode(&result); err != nil {
		return nil, fmt.Errorf("invalid result JSON: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, fmt.Errorf("result has trailing JSON")
	}
	if result.Nonce != request.Nonce || result.Generation != request.Generation || result.Association != request.Association {
		return nil, fmt.Errorf("result association does not match request")
	}
	payload, err := canonicalJSON(result.Payload)
	if err != nil || len(payload) > MaxDecodedFrameBytes {
		return nil, fmt.Errorf("result payload is invalid or oversized")
	}
	return payload, nil
}

func canonicalJSON(input any) ([]byte, error) {
	raw, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, fmt.Errorf("more than one JSON value")
	}
	return json.Marshal(value)
}

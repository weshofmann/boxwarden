package serialx

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/weshofmann/boxwarden/internal/guestproto"
)

const (
	bootstrapCommand     = "/usr/bin/sudo -n -- /usr/local/libexec/boxwarden-guest-bootstrap serial-bootstrap\n"
	MaxPhysicalLineBytes = 128 << 10
	MaxExchangeBytes     = 256 << 10
	ExchangeDeadline     = 30 * time.Second
)

// Bootstrap performs the runtime's only exchange, using a fixed helper and one
// canonical request line. The pump alone reads and validates the response.
func (r *Runtime) Bootstrap(ctx context.Context, request guestproto.SerialRequest) (guestproto.SerialResult, error) {
	if err := ctx.Err(); err != nil {
		return guestproto.SerialResult{}, err
	}
	if err := request.Validate(); err != nil {
		return guestproto.SerialResult{}, err
	}
	if request.StartGeneration != r.generation {
		return guestproto.SerialResult{}, fmt.Errorf("serial request generation mismatch")
	}
	encoded, err := json.Marshal(request)
	if err != nil || len(encoded) > guestproto.MaxRequestBytes {
		return guestproto.SerialResult{}, fmt.Errorf("serial request exceeds bound")
	}
	r.mu.Lock()
	if r.attempted || r.err != nil {
		r.mu.Unlock()
		return guestproto.SerialResult{}, fmt.Errorf("serial bootstrap is unavailable or already attempted")
	}
	r.attempted = true
	r.parser = &bootstrapParser{request: request}
	r.mu.Unlock()

	ctx, cancel := context.WithTimeout(ctx, ExchangeDeadline)
	defer cancel()
	written := make(chan error, 1)
	go func() { written <- writeAll(r.stream, append(append([]byte(bootstrapCommand), encoded...), '\n')) }()
	select {
	case err := <-written:
		if err != nil {
			r.fail(fmt.Errorf("write bootstrap: %w", err))
			return guestproto.SerialResult{}, r.Err()
		}
	case <-ctx.Done():
		r.fail(ctx.Err())
		<-written // Closing the owned stream interrupts a blocked writer.
		return guestproto.SerialResult{}, r.Err()
	}
	select {
	case <-r.ready:
		r.mu.Lock()
		defer r.mu.Unlock()
		return r.result, r.err
	case <-ctx.Done():
		r.fail(ctx.Err())
		return guestproto.SerialResult{}, r.Err()
	}
}

type bootstrapParser struct {
	request guestproto.SerialRequest
	partial []byte
	total   int
	started bool
}

func (p *bootstrapParser) feed(output []byte) (guestproto.SerialResult, bool, error) {
	for _, b := range output {
		p.total++
		if p.total > MaxExchangeBytes {
			return guestproto.SerialResult{}, false, fmt.Errorf("serial exchange exceeds bound")
		}
		if b != '\n' {
			if len(p.partial) == MaxPhysicalLineBytes {
				return guestproto.SerialResult{}, false, fmt.Errorf("serial line exceeds bound")
			}
			p.partial = append(p.partial, b)
			continue
		}
		result, complete, err := p.line(string(p.partial))
		p.partial = p.partial[:0]
		if err != nil || complete {
			return result, complete, err
		}
	}
	return guestproto.SerialResult{}, false, nil
}

func (p *bootstrapParser) line(line string) (guestproto.SerialResult, bool, error) {
	if !strings.HasPrefix(line, "BOXWARDEN-") {
		if p.started && line != "" {
			return guestproto.SerialResult{}, false, fmt.Errorf("interleaved serial response")
		}
		return guestproto.SerialResult{}, false, nil
	}
	// Only normalize the optional terminal CR. No other whitespace changes
	// are allowed in control framing or the base64-encoded response.
	line = strings.TrimSuffix(line, "\r")
	if strings.ContainsAny(line, "\r\n") {
		return guestproto.SerialResult{}, false, fmt.Errorf("invalid control line ending")
	}
	fields := strings.Split(line, " ")
	switch fields[0] {
	case "BOXWARDEN-BEGIN":
		if p.started || len(fields) != 3 || fields[1] != p.request.Nonce || fields[2] != p.request.SessionID {
			return guestproto.SerialResult{}, false, fmt.Errorf("ambiguous or mismatched begin frame")
		}
		p.started = true
		return guestproto.SerialResult{}, false, nil
	case "BOXWARDEN-END":
		if !p.started {
			return guestproto.SerialResult{}, false, fmt.Errorf("end frame without begin")
		}
		result, err := guestproto.DecodeSerialEndLine(p.request, line)
		return result, err == nil, err
	default:
		return guestproto.SerialResult{}, false, fmt.Errorf("unknown serial control frame")
	}
}

func writeAll(writer io.Writer, input []byte) error {
	for len(input) != 0 {
		n, err := writer.Write(input)
		if n < 0 || n > len(input) {
			return fmt.Errorf("invalid serial write count")
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

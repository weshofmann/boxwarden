package clipboardx

import (
	"context"
	"errors"
	"io"
	"time"
)

// Target is a value binding to one admitted generation. Endpoint verifies all
// fields against retained live readiness, without fallback or retargeting.
type Target struct{ Domain, SessionID, BackendKind, BackendObject, Generation string }
type Mode string

const (
	Push  Mode = "push"
	Pull  Mode = "pull"
	Copy  Mode = "copy"
	Paste Mode = "paste"
)

type Direction string

const (
	ToGuest   Direction = "to-guest"
	FromGuest Direction = "from-guest"
)

type Outcome string

const (
	Unchanged Outcome = "unchanged"
	Committed Outcome = "committed"
	Unknown   Outcome = "unknown"
)
const TransferTimeout = 30 * time.Second

var (
	ErrRequest     = errors.New("invalid clipboard request")
	ErrTerminal    = errors.New("clipboard output to terminal requires raw mode")
	ErrAdmission   = errors.New("clipboard target unavailable")
	ErrCancelled   = errors.New("clipboard transfer cancelled")
	ErrUnavailable = errors.New("clipboard text unavailable")
	ErrUnknown     = errors.New("clipboard destination outcome unknown")
)

type Request struct {
	Target        Target
	Mode          Mode
	Raw, Terminal bool
}

// Endpoint admits exactly one target before source capture. Begin must return a
// ready channel or fail immediately for busy/stale targets; it must not queue.
type Endpoint interface {
	Begin(context.Context, Target, Direction) (Transfer, error)
}

// Transfer belongs to one Execute call. Write returns Unknown if any error may
// follow destination mutation, including a lost acknowledgement. Read returns
// complete text, never a streaming partial value. Methods must honor ctx.
type Transfer interface {
	Write(context.Context, []byte) (Outcome, error)
	Read(context.Context) ([]byte, error)
	Close() error
}

// Pasteboard distinguishes absent/unsupported text (ErrUnavailable) from valid
// empty text (zero bytes, nil error). WriteText prepares the entire item before
// mutation and returns Unknown for failure after a possible commit.
type Pasteboard interface {
	ReadText(context.Context) ([]byte, error)
	WriteText(context.Context, []byte) (Outcome, error)
}
type Service struct{ Endpoint Endpoint }

// Execute captures req by value and admits its exact target before reading a source.
// Endpoint and Pasteboard implementations must honor ctx, including its deadline.
// Caller-owned stream I/O is synchronous: a blocking reader/writer must provide
// its own cancellation mechanism. Readers implementing ReadContext(context.Context,
// []byte) receive the derived deadline. Execute never leaves background source reads.
func (s Service) Execute(ctx context.Context, req Request, input io.Reader, output io.Writer, board Pasteboard) (Outcome, error) {
	if req.Mode == Paste && req.Terminal && !req.Raw {
		return Unchanged, ErrTerminal
	}
	direction := ToGuest
	switch req.Mode {
	case Push, Pull:
		if board == nil {
			return Unchanged, ErrRequest
		}
	case Copy:
		if input == nil {
			return Unchanged, ErrRequest
		}
	case Paste:
		if output == nil {
			return Unchanged, ErrRequest
		}
	default:
		return Unchanged, ErrRequest
	}
	if req.Mode == Pull || req.Mode == Paste {
		direction = FromGuest
	}
	if s.Endpoint == nil || req.Target.Domain == "" || req.Target.SessionID == "" || req.Target.BackendKind == "" || req.Target.BackendObject == "" || req.Target.Generation == "" {
		return Unchanged, ErrRequest
	}
	ctx, cancel := context.WithTimeout(ctx, TransferTimeout)
	defer cancel()
	if ctx.Err() != nil {
		return Unchanged, ErrCancelled
	}
	transfer, err := s.Endpoint.Begin(ctx, req.Target, direction)
	if err != nil {
		return Unchanged, sanitize(err, ErrAdmission)
	}
	if transfer == nil {
		return Unchanged, ErrAdmission
	}
	defer transfer.Close()
	if ctx.Err() != nil {
		return Unchanged, ErrCancelled
	}
	var data []byte
	switch req.Mode {
	case Push:
		data, err = board.ReadText(ctx)
	case Copy:
		data, err = ReadText(contextReader{ctx: ctx, r: input})
	case Pull, Paste:
		data, err = transfer.Read(ctx)
	}
	if ctx.Err() != nil {
		return Unchanged, ErrCancelled
	}
	if err != nil {
		return Unchanged, sanitize(err, ErrRead)
	}
	if err = Validate(data); err != nil {
		return Unchanged, err
	}
	// Own a snapshot so implementations cannot retain and replace the source's
	// backing buffer while a destination prepares its commit.
	data = append([]byte{}, data...)
	if ctx.Err() != nil {
		return Unchanged, ErrCancelled
	}
	switch req.Mode {
	case Push, Copy:
		outcome, writeErr := transfer.Write(ctx, data)
		return destinationResult(outcome, writeErr)
	case Pull:
		outcome, writeErr := board.WriteText(ctx, data)
		return destinationResult(outcome, writeErr)
	case Paste:
		var n int
		var writeErr error
		if contextual, ok := output.(interface {
			WriteContext(context.Context, []byte) (int, error)
		}); ok {
			n, writeErr = contextual.WriteContext(ctx, data)
		} else {
			n, writeErr = output.Write(data)
		}
		if writeErr != nil || n != len(data) {
			return Unknown, ErrUnknown
		}
		return Committed, nil
	}
	return Unchanged, ErrRequest
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if r.ctx.Err() != nil {
		return 0, ErrCancelled
	}
	var n int
	var err error
	if reader, ok := r.r.(interface {
		ReadContext(context.Context, []byte) (int, error)
	}); ok {
		n, err = reader.ReadContext(r.ctx, p)
	} else {
		n, err = r.r.Read(p)
	}
	if r.ctx.Err() != nil {
		return n, ErrCancelled
	}
	return n, err
}

// A destination must return Unknown whenever an error might follow mutation.
// Committed may accompany an ancillary failure after a confirmed commit.
func destinationResult(outcome Outcome, err error) (Outcome, error) {
	if errors.Is(err, ErrUnknown) {
		return Unknown, ErrUnknown
	}
	switch outcome {
	case Unknown:
		return Unknown, ErrUnknown
	case Committed:
		if err != nil {
			return Committed, sanitize(err, ErrWrite)
		}
		return Committed, nil
	case Unchanged:
		if err != nil {
			return Unchanged, sanitize(err, ErrWrite)
		}
		return Unchanged, ErrWrite
	default:
		return Unknown, ErrUnknown
	}
}
func sanitize(err, fallback error) error {
	for _, known := range []error{ErrTooLarge, ErrInvalidText, ErrRead, ErrWrite, ErrFrame, ErrRequest, ErrTerminal, ErrAdmission, ErrCancelled, ErrUnavailable, ErrUnknown} {
		if errors.Is(err, known) {
			return known
		}
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return ErrCancelled
	}
	return fallback
}

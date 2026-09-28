package clipboardx

import (
	"bytes"
	"context"
	"errors"
	"io"
	"reflect"
	"testing"
	"time"
)

var boundTarget = Target{"personal", "uuid", "tart", "sandbox", "generation"}

type endpointFake struct {
	events    *[]string
	tr        *transferFake
	err       error
	target    Target
	direction Direction
	calls     int
	deadline  time.Time
}

func (e *endpointFake) Begin(ctx context.Context, t Target, d Direction) (Transfer, error) {
	*e.events = append(*e.events, "begin")
	e.calls++
	e.target = t
	e.direction = d
	e.deadline, _ = ctx.Deadline()
	return e.tr, e.err
}

type transferFake struct {
	events  *[]string
	data    []byte
	outcome Outcome
	err     error
	writes  int
	closed  int
}

func (t *transferFake) Write(_ context.Context, data []byte) (Outcome, error) {
	*t.events = append(*t.events, "guest-write")
	t.data = bytes.Clone(data)
	t.writes++
	return t.outcome, t.err
}
func (t *transferFake) Read(context.Context) ([]byte, error) {
	*t.events = append(*t.events, "guest-read")
	return bytes.Clone(t.data), t.err
}
func (t *transferFake) Close() error { t.closed++; return nil }

type boardFake struct {
	events        *[]string
	data          []byte
	err           error
	outcome       Outcome
	reads, writes int
	afterRead     func()
}

func (b *boardFake) ReadText(context.Context) ([]byte, error) {
	*b.events = append(*b.events, "host-read")
	b.reads++
	if b.afterRead != nil {
		b.afterRead()
	}
	return bytes.Clone(b.data), b.err
}
func (b *boardFake) WriteText(_ context.Context, data []byte) (Outcome, error) {
	*b.events = append(*b.events, "host-write")
	b.writes++
	b.data = bytes.Clone(data)
	return b.outcome, b.err
}
func setup() (*Service, *endpointFake, *transferFake, *boardFake, *[]string) {
	events := []string{}
	tr := &transferFake{events: &events, outcome: Committed}
	e := &endpointFake{events: &events, tr: tr}
	b := &boardFake{events: &events, outcome: Committed}
	return &Service{e}, e, tr, b, &events
}

type sourceFake struct {
	events *[]string
	r      io.Reader
	hook   func()
}

func (s sourceFake) Read(p []byte) (int, error) {
	*s.events = append(*s.events, "stdin")
	if s.hook != nil {
		s.hook()
	}
	return s.r.Read(p)
}

func TestExecuteAdmitsBeforeCapturingSourceAndKeepsTarget(t *testing.T) {
	s, e, tr, b, events := setup()
	b.data = []byte("雪\n\n")
	req := Request{Target: boundTarget, Mode: Push}
	b.afterRead = func() { req.Target.Generation = "replacement" }
	outcome, err := s.Execute(context.Background(), req, nil, nil, b)
	if err != nil || outcome != Committed || !bytes.Equal(tr.data, []byte("雪\n\n")) {
		t.Fatalf("push: %s %v", outcome, err)
	}
	if !reflect.DeepEqual(*events, []string{"begin", "host-read", "guest-write"}) {
		t.Fatalf("ordering: %v", *events)
	}
	if e.target != boundTarget || e.direction != ToGuest || e.calls != 1 || tr.closed != 1 {
		t.Fatal("binding, retry, direction or cleanup changed")
	}
	if delta := time.Until(e.deadline); delta <= 0 || delta > 30*time.Second {
		t.Fatal("transfer deadline missing or unbounded")
	}
}
func TestExecuteAdmissionFailureDoesNotReadSource(t *testing.T) {
	for _, mode := range []Mode{Push, Copy, Pull, Paste} {
		s, e, tr, b, events := setup()
		e.err = errors.New("synthetic private payload")
		_, err := s.Execute(context.Background(), Request{Target: boundTarget, Mode: mode}, sourceFake{events: events, r: bytes.NewReader([]byte("secret"))}, io.Discard, b)
		if !errors.Is(err, ErrAdmission) || len(*events) != 1 || b.reads != 0 || tr.writes != 0 {
			t.Fatalf("admission read source: %v %v", *events, err)
		}
	}
}
func TestExecuteAbsentVersusEmpty(t *testing.T) {
	s, _, tr, b, _ := setup()
	b.err = ErrUnavailable
	outcome, err := s.Execute(context.Background(), Request{Target: boundTarget, Mode: Push}, nil, nil, b)
	if outcome != Unchanged || !errors.Is(err, ErrUnavailable) || tr.writes != 0 {
		t.Fatalf("absent: %s %v", outcome, err)
	}
	b.err = nil
	b.data = []byte{}
	outcome, err = s.Execute(context.Background(), Request{Target: boundTarget, Mode: Push}, nil, nil, b)
	if err != nil || outcome != Committed || tr.writes != 1 || len(tr.data) != 0 {
		t.Fatalf("empty: %s %v", outcome, err)
	}
}
func TestExecuteRejectsInvalidBeforeDestinationWrite(t *testing.T) {
	for _, mode := range []Mode{Push, Copy, Pull, Paste} {
		s, _, tr, b, _ := setup()
		b.data = []byte{0xff}
		tr.data = []byte{0xff}
		var out bytes.Buffer
		out.WriteString("initial")
		outcome, err := s.Execute(context.Background(), Request{Target: boundTarget, Mode: mode}, bytes.NewReader([]byte{0xff}), &out, b)
		if !errors.Is(err, ErrInvalidText) || outcome != Unchanged || b.writes != 0 || tr.writes != 0 || out.String() != "initial" {
			t.Fatalf("invalid %s: %s %v", mode, outcome, err)
		}
	}
}
func TestExecuteCancellationBeforeCommitPreservesDestination(t *testing.T) {
	s, _, tr, b, _ := setup()
	ctx, cancel := context.WithCancel(context.Background())
	b.afterRead = cancel
	b.data = []byte("new")
	outcome, err := s.Execute(ctx, Request{Target: boundTarget, Mode: Push}, nil, nil, b)
	if outcome != Unchanged || !errors.Is(err, ErrCancelled) || tr.writes != 0 {
		t.Fatalf("cancel: %s %v", outcome, err)
	}
}
func TestExecuteUnknownNeverRetriesOrClaimsRollback(t *testing.T) {
	for _, mode := range []Mode{Push, Pull} {
		s, e, tr, b, _ := setup()
		tr.data = []byte("value")
		b.data = []byte("value")
		if mode == Push {
			tr.outcome = Unknown
			tr.err = errors.New("synthetic private payload")
		} else {
			b.outcome = Unknown
			b.err = errors.New("synthetic private payload")
		}
		outcome, err := s.Execute(context.Background(), Request{Target: boundTarget, Mode: mode}, nil, nil, b)
		if outcome != Unknown || !errors.Is(err, ErrUnknown) || e.calls != 1 {
			t.Fatalf("unknown %s: %s %v", mode, outcome, err)
		}
	}
}
func TestExecutePasteTerminalRequiresRawBeforeAdmission(t *testing.T) {
	s, e, tr, b, _ := setup()
	tr.data = []byte("雪\n\n")
	var out bytes.Buffer
	outcome, err := s.Execute(context.Background(), Request{Target: boundTarget, Mode: Paste, Terminal: true}, nil, &out, b)
	if outcome != Unchanged || !errors.Is(err, ErrTerminal) || e.calls != 0 || out.Len() != 0 {
		t.Fatalf("TTY: %s %v", outcome, err)
	}
	outcome, err = s.Execute(context.Background(), Request{Target: boundTarget, Mode: Paste, Terminal: true, Raw: true}, nil, &out, b)
	if err != nil || outcome != Committed || out.String() != "雪\n\n" || b.reads != 0 || b.writes != 0 {
		t.Fatalf("raw: %s %v", outcome, err)
	}
}
func TestExecuteCopyUsesOnlyStdin(t *testing.T) {
	s, _, tr, b, events := setup()
	outcome, err := s.Execute(context.Background(), Request{Target: boundTarget, Mode: Copy}, sourceFake{events: events, r: bytes.NewReader([]byte("\n雪\n"))}, nil, b)
	if err != nil || outcome != Committed || !bytes.Equal(tr.data, []byte("\n雪\n")) || b.reads != 0 || b.writes != 0 || (*events)[0] != "begin" {
		t.Fatalf("copy: %s %v", outcome, err)
	}
}
func TestExecutePullReadsGuestBeforeHostMutation(t *testing.T) {
	s, _, tr, b, events := setup()
	tr.data = []byte("雪\n")
	outcome, err := s.Execute(context.Background(), Request{Target: boundTarget, Mode: Pull}, nil, nil, b)
	if err != nil || outcome != Committed || !bytes.Equal(b.data, []byte("雪\n")) || !reflect.DeepEqual(*events, []string{"begin", "guest-read", "host-write"}) {
		t.Fatalf("pull: %s %v", outcome, err)
	}
}

func TestExecuteUnknownErrorCannotClaimDestinationUnchanged(t *testing.T) {
	s, _, tr, b, _ := setup()
	tr.outcome = Unchanged
	tr.err = ErrUnknown
	b.data = []byte("value")
	outcome, err := s.Execute(context.Background(), Request{Target: boundTarget, Mode: Push}, nil, nil, b)
	if outcome != Unknown || !errors.Is(err, ErrUnknown) {
		t.Fatalf("ambiguous failure reported %s %v", outcome, err)
	}
}

func TestExecuteCancelledBeforeAdmissionDoesNotTouchSource(t *testing.T) {
	s, e, _, b, _ := setup()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	outcome, err := s.Execute(ctx, Request{Target: boundTarget, Mode: Push}, nil, nil, b)
	if outcome != Unchanged || !errors.Is(err, ErrCancelled) || e.calls != 0 || b.reads != 0 {
		t.Fatalf("cancel before admission: %s %v", outcome, err)
	}
}

func TestExecuteCopyCancellationAfterReadDoesNotCommit(t *testing.T) {
	s, _, tr, b, events := setup()
	ctx, cancel := context.WithCancel(context.Background())
	outcome, err := s.Execute(ctx, Request{Target: boundTarget, Mode: Copy}, sourceFake{events: events, r: bytes.NewReader([]byte("value")), hook: cancel}, nil, b)
	if outcome != Unchanged || !errors.Is(err, ErrCancelled) || tr.writes != 0 {
		t.Fatalf("cancel stdin: %s %v", outcome, err)
	}
}

func TestExecuteCancelledWritePreservesUnknown(t *testing.T) {
	s, _, tr, b, _ := setup()
	b.data = []byte("value")
	tr.outcome = Unknown
	tr.err = context.Canceled
	outcome, err := s.Execute(context.Background(), Request{Target: boundTarget, Mode: Push}, nil, nil, b)
	if outcome != Unknown || !errors.Is(err, ErrUnknown) {
		t.Fatalf("possible commit cancellation: %s %v", outcome, err)
	}
}

func TestExecutePasteShortWriteIsUnknown(t *testing.T) {
	s, _, tr, b, _ := setup()
	tr.data = []byte("value")
	outcome, err := s.Execute(context.Background(), Request{Target: boundTarget, Mode: Paste}, nil, shortWriter{}, b)
	if outcome != Unknown || !errors.Is(err, ErrUnknown) {
		t.Fatalf("short stdout write: %s %v", outcome, err)
	}
}

func TestExecuteInvalidRequestCannotAdmitOrRead(t *testing.T) {
	for _, req := range []Request{{Target: boundTarget, Mode: "invalid"}, {Target: Target{}, Mode: Push}} {
		s, e, _, b, _ := setup()
		outcome, err := s.Execute(context.Background(), req, nil, nil, b)
		if outcome != Unchanged || !errors.Is(err, ErrRequest) || e.calls != 0 || b.reads != 0 {
			t.Fatalf("invalid request: %s %v", outcome, err)
		}
	}
}

func TestExecuteSourceFailureIsSanitized(t *testing.T) {
	for _, mode := range []Mode{Copy, Push, Pull} {
		s, _, tr, b, _ := setup()
		b.err = errors.New("synthetic private payload")
		tr.err = b.err
		outcome, err := s.Execute(context.Background(), Request{Target: boundTarget, Mode: mode}, errorReader{}, nil, b)
		if outcome != Unchanged || err != ErrRead || b.writes != 0 || tr.writes != 0 {
			t.Fatalf("source failure %s: %s %v", mode, outcome, err)
		}
	}
}

type cancellableInput struct {
	called bool
	ctx    context.Context
}

func (c *cancellableInput) Read([]byte) (int, error) {
	return 0, errors.New("plain reader cannot honor cancellation")
}
func (c *cancellableInput) ReadContext(ctx context.Context, p []byte) (int, error) {
	c.called = true
	c.ctx = ctx
	return copy(p, []byte("value")), io.EOF
}
func TestExecuteCopyPassesDerivedDeadlineToCancellableInput(t *testing.T) {
	s, _, tr, b, _ := setup()
	input := &cancellableInput{}
	outcome, err := s.Execute(context.Background(), Request{Target: boundTarget, Mode: Copy}, input, nil, b)
	if err != nil || outcome != Committed || !input.called || !bytes.Equal(tr.data, []byte("value")) {
		t.Fatalf("context read unused: %s %v", outcome, err)
	}
	deadline, ok := input.ctx.Deadline()
	if !ok || time.Until(deadline) > 30*time.Second {
		t.Fatal("context source has no bounded deadline")
	}
}

type cancellationWriter struct{ used bool }

func (w *cancellationWriter) Write([]byte) (int, error) {
	return 0, errors.New("uncancellable output used")
}
func (w *cancellationWriter) WriteContext(ctx context.Context, p []byte) (int, error) {
	w.used = true
	return 1, context.DeadlineExceeded
}
func TestPasteUsesContextWriterAndPartialOutputIsUnknown(t *testing.T) {
	s, _, tr, _, _ := setup()
	tr.data = []byte("two")
	out := &cancellationWriter{}
	result, err := s.Execute(context.Background(), Request{Target: boundTarget, Mode: Paste}, nil, out, nil)
	if !out.used || result != Unknown || err != ErrUnknown {
		t.Fatalf("context output=%v outcome=%s err=%v", out.used, result, err)
	}
}

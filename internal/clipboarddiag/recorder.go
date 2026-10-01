//go:build n1clipboarddiagnostic

// Package clipboarddiag contains only finite clipboard diagnostic metadata.
// It confers no runtime admission, transfer, path or retry authority.
package clipboarddiag

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"time"
)

const MaxHeaderBytes = 4096
const MaxRecords = 32
const MaxFragmentBytes = 16384
const MaxCollectionBytes = 32768

var ErrMetadata = errors.New("clipboard diagnostic metadata unavailable")
var uuidPattern = regexp.MustCompile(`^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$`)
var objectPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)

type Operation struct {
	Version       int       `json:"version"`
	OperationID   string    `json:"operation_id"`
	Direction     string    `json:"direction"`
	Domain        string    `json:"domain"`
	SessionID     string    `json:"session_id"`
	BackendKind   string    `json:"backend_kind"`
	BackendObject string    `json:"backend_object"`
	Generation    string    `json:"generation"`
	ExpiresAt     time.Time `json:"expires_at"`
}

func ValidUUID(s string) bool {
	return uuidPattern.MatchString(s) && s != "00000000-0000-0000-0000-000000000000"
}
func (o Operation) Validate(future bool) error {
	if o.Version != 1 || !ValidUUID(o.OperationID) || !ValidUUID(o.SessionID) || !ValidUUID(o.Generation) || o.Domain != "n1qualification" || o.BackendKind != "tart" || !objectPattern.MatchString(o.BackendObject) || (o.Direction != "read" && o.Direction != "write") || o.ExpiresAt.IsZero() || o.ExpiresAt.Location() != time.UTC || o.ExpiresAt.UnixNano() <= 0 || o.ExpiresAt.After(time.Unix(0, 1<<63-1)) {
		return ErrMetadata
	}
	if future {
		now := time.Now()
		if !o.ExpiresAt.After(now) || o.ExpiresAt.After(now.Add(30*time.Second)) {
			return ErrMetadata
		}
	}
	return nil
}
func EncodeOperation(o Operation) ([]byte, error) {
	if o.Validate(false) != nil {
		return nil, ErrMetadata
	}
	return json.Marshal(map[string]any{"version": o.Version, "operation_id": o.OperationID, "direction": o.Direction, "domain": o.Domain, "session_id": o.SessionID, "backend_kind": o.BackendKind, "backend_object": o.BackendObject, "generation": o.Generation, "expires_at": o.ExpiresAt.Format(time.RFC3339Nano)})
}
func DecodeOperation(raw []byte, future bool) (Operation, error) {
	var o Operation
	if StrictDecode(raw, &o, MaxHeaderBytes) != nil || o.Validate(future) != nil {
		return Operation{}, ErrMetadata
	}
	want, _ := EncodeOperation(o)
	if !bytes.Equal(raw, want) {
		return Operation{}, ErrMetadata
	}
	return o, nil
}
func HeaderDigest(o Operation) string {
	raw, err := EncodeOperation(o)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// StrictDecode rejects duplicates recursively before typed exact-field decoding.
func StrictDecode(raw []byte, destination any, limit int) error {
	if len(raw) == 0 || len(raw) > limit {
		return ErrMetadata
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	if scanValue(d, 0) != nil {
		return ErrMetadata
	}
	if _, err := d.Token(); err != io.EOF {
		return ErrMetadata
	}
	if shapeJSON(raw, reflect.TypeOf(destination).Elem()) != nil {
		return ErrMetadata
	}
	d = json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(destination) != nil {
		return ErrMetadata
	}
	if d.Decode(new(any)) != io.EOF {
		return ErrMetadata
	}
	return nil
}
func scanValue(d *json.Decoder, depth int) error {
	if depth > 16 {
		return ErrMetadata
	}
	t, err := d.Token()
	if err != nil {
		return ErrMetadata
	}
	delimiter, ok := t.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		seen := map[string]bool{}
		for d.More() {
			k, e := d.Token()
			key, ok := k.(string)
			if e != nil || !ok || seen[key] {
				return ErrMetadata
			}
			seen[key] = true
			if scanValue(d, depth+1) != nil {
				return ErrMetadata
			}
		}
		t, err = d.Token()
		if err != nil || t != json.Delim('}') {
			return ErrMetadata
		}
	case '[':
		for d.More() {
			if scanValue(d, depth+1) != nil {
				return ErrMetadata
			}
		}
		t, err = d.Token()
		if err != nil || t != json.Delim(']') {
			return ErrMetadata
		}
	default:
		return ErrMetadata
	}
	return nil
}
func ReadHeader(r io.Reader) ([]byte, error) {
	raw := make([]byte, 0, 512)
	for len(raw) <= MaxHeaderBytes {
		var b [1]byte
		n, e := r.Read(b[:])
		if n == 1 {
			if b[0] == '\n' {
				if len(raw) == 0 {
					return nil, ErrMetadata
				}
				return raw, nil
			}
			raw = append(raw, b[0])
			continue
		}
		if e != nil || n == 0 {
			return nil, ErrMetadata
		}
	}
	return nil, ErrMetadata
}

type RecordEntry struct {
	Source string `json:"source"`
	Stage  string `json:"stage"`
	Status string `json:"status"`
	AtMS   int64  `json:"at_ms"`
}
type Fragment struct {
	Version  int           `json:"version"`
	Binding  Operation     `json:"binding"`
	Origin   string        `json:"origin"`
	Complete bool          `json:"complete"`
	Records  []RecordEntry `json:"records"`
}

var stages = map[string]bool{}

// Explicit producer catalogue: helper_metadata is also the guest method's
// own pre-dispatch immutable-context admission seam. No prefix grants admission.
var producerStages = map[string][]string{
	"cli":        {"cli_binding", "cli_factory", "cli_admission", "cli_source", "cli_outcome", "cli_merge"},
	"supervisor": {"control_expiry_missing", "control_expiry_elapsed", "control_expiry_bound", "control_expiry", "control_deadline", "control_capability", "control_context", "control_binding", "control_ready", "control_ready_frame", "control_length", "control_source", "control_terminator", "control_redispatch_context", "control_redispatch_binding", "control_redispatch_ready", "control_dispatch", "control_outcome", "control_read_validation", "control_post_context", "control_post_binding", "control_post_ready", "control_final_error_frame", "control_final_ok_frame", "control_complete"},
	"runtime":    {"runtime_client_missing", "runtime_final_context", "runtime_context", "runtime_owner", "runtime_client", "runtime_observation_context", "runtime_ready", "runtime_connection", "runtime_binding", "runtime_record", "runtime_admitted", "runtime_request", "runtime_dispatch", "runtime_transport", "runtime_response", "runtime_length", "runtime_post_context", "runtime_post_binding", "runtime_post_ready", "runtime_post_connection", "runtime_post_record", "runtime_status", "runtime_text", "runtime_complete"},
	"ssh":        {"ssh_runner", "ssh_diagnostic", "ssh_client", "ssh_connection", "ssh_association", "ssh_context", "ssh_encoding", "ssh_encoding_size", "ssh_pre_pin_context", "ssh_pin", "ssh_pre_dispatch_context", "ssh_dispatch", "ssh_transport", "ssh_post_context", "ssh_truncated", "ssh_stdout_bound", "ssh_stderr_bound", "ssh_ack_missing", "ssh_ack_malformed", "ssh_ack_length", "ssh_ack_status", "ssh_complete", "ssh_collection"},
	"guest":      {"guest_request", "guest_text", "guest_read_payload", "guest_context", "guest_receiver", "guest_executor", "guest_prebinding", "guest_admitted", "guest_dispatch", "guest_decode_missing", "guest_decode_malformed", "guest_explicit_error", "guest_execution", "guest_length", "guest_post_context", "guest_postbinding", "guest_unknown", "guest_complete", "helper_metadata"},
	"helper":     {"helper_wrapper", "helper_namespace", "helper_metadata", "helper_pipe", "helper_start", "helper_write_close", "helper_proof", "helper_proof_read_close", "helper_publish", "helper_bootstrap", "helper_response", "helper_collection"},
}

func init() {
	for _, catalogue := range producerStages {
		for _, stage := range catalogue {
			stages[stage] = true
		}
	}
}
func validProducerStage(source, stage string) bool {
	for _, admitted := range producerStages[source] {
		if admitted == stage {
			return true
		}
	}
	return false
}
func validSource(s string) bool {
	return s == "cli" || s == "supervisor" || s == "runtime" || s == "ssh" || s == "guest" || s == "helper"
}
func originSource(origin, source string) bool {
	switch origin {
	case "cli":
		return source == "cli"
	case "guest":
		return source == "guest" || source == "helper"
	case "supervisor":
		return source == "supervisor" || source == "runtime" || source == "ssh"
	}
	return false
}
func validStatus(s string) bool {
	return s == "ok" || s == "refused" || s == "cancelled" || s == "unknown" || s == "unavailable" || s == "incomplete" || s == "error"
}

type Recorder struct {
	mu          sync.Mutex
	operation   Operation
	origin      string
	now         func() time.Time
	start, last time.Time
	complete    bool
	records     []RecordEntry
}

func NewRecorder(o Operation, origin string, now func() time.Time) (*Recorder, error) {
	if o.Validate(false) != nil || (origin != "supervisor" && origin != "cli" && origin != "guest") || now == nil {
		return nil, ErrMetadata
	}
	t := now()
	return &Recorder{operation: o, origin: origin, now: now, start: t, last: t, complete: true, records: make([]RecordEntry, 0, MaxRecords)}, nil
}
func (r *Recorder) append(source, stage, status string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	if !validSource(source) || !originSource(r.origin, source) || !validProducerStage(source, stage) || !validStatus(status) || len(r.records) >= MaxRecords || now.Before(r.last) || now.Before(r.start) || now.Sub(r.start) >= 60*time.Second {
		r.complete = false
		return
	}
	for _, record := range r.records {
		if (record.Source == source && record.Stage == stage) || (source == "supervisor" && record.Source == source && finalControlStage(record.Stage) && finalControlStage(stage)) {
			r.complete = false
			return
		}
	}
	r.last = now
	r.records = append(r.records, RecordEntry{source, stage, status, now.Sub(r.start).Milliseconds()})
	raw, err := json.Marshal(r.fragmentLocked())
	if err != nil || len(raw)+1 > MaxFragmentBytes {
		r.records = r.records[:len(r.records)-1]
		r.complete = false
	}
}
func (r *Recorder) Invalidate() {
	if r != nil {
		r.mu.Lock()
		r.complete = false
		r.mu.Unlock()
	}
}
func (r *Recorder) fragmentLocked() Fragment {
	return Fragment{1, r.operation, r.origin, r.complete && terminalCoverage(r.origin, r.records), append([]RecordEntry{}, r.records...)}
}
func (r *Recorder) Fragment() Fragment {
	if r == nil {
		return Fragment{}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.fragmentLocked()
}
func (f Fragment) Validate() error {
	if f.Version != 1 || f.Binding.Validate(false) != nil || (f.Origin != "supervisor" && f.Origin != "cli" && f.Origin != "guest") || len(f.Records) > MaxRecords || f.Records == nil {
		return ErrMetadata
	}
	if f.Complete && !terminalCoverage(f.Origin, f.Records) {
		return ErrMetadata
	}
	var prev int64
	finals := 0
	seen := map[[2]string]bool{}
	for _, r := range f.Records {
		if r.Source == "supervisor" && finalControlStage(r.Stage) {
			finals++
			if finals > 1 {
				return ErrMetadata
			}
		}
		key := [2]string{r.Source, r.Stage}
		if seen[key] {
			return ErrMetadata
		}
		seen[key] = true
		if !validSource(r.Source) || !originSource(f.Origin, r.Source) || !validProducerStage(r.Source, r.Stage) || !validStatus(r.Status) || r.AtMS < prev || r.AtMS >= 60000 {
			return ErrMetadata
		}
		prev = r.AtMS
	}
	raw, err := json.Marshal(f)
	if err != nil || len(raw)+1 > MaxFragmentBytes {
		return ErrMetadata
	}
	return nil
}

type contextKey struct{}
type diagnosticContext struct {
	operation Operation
	recorder  *Recorder
	source    string
}

func WithContext(ctx context.Context, o Operation, r *Recorder) (context.Context, error) {
	if ctx == nil || r == nil || o.Validate(false) != nil || r.operation != o {
		return nil, ErrMetadata
	}
	source := r.origin
	if source == "guest" {
		source = "guest"
	}
	return context.WithValue(ctx, contextKey{}, diagnosticContext{o, r, source}), nil
}
func WithSource(ctx context.Context, source string) context.Context {
	if ctx == nil {
		return ctx
	}
	v, ok := ctx.Value(contextKey{}).(diagnosticContext)
	if !ok {
		return ctx
	}
	if !validSource(source) {
		v.recorder.Invalidate()
		return ctx
	}
	v.source = source
	return context.WithValue(ctx, contextKey{}, v)
}
func Get(ctx context.Context) (Operation, *Recorder, bool) {
	if ctx == nil {
		return Operation{}, nil, false
	}
	v, ok := ctx.Value(contextKey{}).(diagnosticContext)
	return v.operation, v.recorder, ok
}
func Record(ctx context.Context, stage, status string) {
	if ctx == nil {
		return
	}
	if v, ok := ctx.Value(contextKey{}).(diagnosticContext); ok {
		v.recorder.append(v.source, stage, status)
	}
}

// Available is intake/recording health, independent of terminal publication.
func Available(ctx context.Context) bool {
	_, r, ok := Get(ctx)
	if !ok {
		return true
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.complete
}

func finalControlStage(stage string) bool {
	return stage == "control_final_ok_frame" || stage == "control_final_error_frame"
}

// Terminal coverage classifies the actual producer end, including an error
// response. It imposes no successful-transfer or fixture-equality condition.
func terminalCoverage(origin string, records []RecordEntry) bool {
	if len(records) == 0 {
		return false
	}
	first, last := records[0], records[len(records)-1]
	switch origin {
	case "cli":
		return first.Source == "cli" && first.Stage == "cli_binding" && first.Status == "ok" && last.Source == "cli" && last.Stage == "cli_outcome"
	case "supervisor":
		return last.Source == "supervisor" && finalControlStage(last.Stage) && last.Status == "ok"
	case "guest":
		return first.Source == "helper" && first.Stage == "helper_namespace" && first.Status == "ok" && last.Source == "helper" && last.Stage == "helper_response" && last.Status == "ok"
	}
	return false
}

// Overflow/foreign/duplicate catalogues are omitted, never truncated or relabeled.
// Missing fragments and admitted partial progression remain incomplete.
func MergeHost(binding Operation, fragments ...Fragment) ([]Fragment, bool) {
	total, bytesTotal := 0, 0
	complete := len(fragments) == 2
	seen := map[string]bool{}
	for _, f := range fragments {
		if f.Validate() != nil || f.Binding != binding || f.Origin == "guest" || seen[f.Origin] {
			return []Fragment{}, false
		}
		raw, _ := json.Marshal(f)
		bytesTotal += len(raw) + 1
		total += len(f.Records)
		seen[f.Origin] = true
		complete = complete && f.Complete
	}
	if total > MaxRecords || bytesTotal > MaxFragmentBytes {
		return []Fragment{}, false
	}
	return append([]Fragment{}, fragments...), complete && seen["supervisor"] && seen["cli"]
}

func shapeJSON(raw json.RawMessage, typ reflect.Type) error {
	raw = bytes.TrimSpace(raw)
	if typ.Kind() == reflect.Pointer {
		if bytes.Equal(raw, []byte("null")) {
			return nil
		}
		return shapeJSON(raw, typ.Elem())
	}
	if typ == reflect.TypeFor[time.Time]() {
		if len(raw) == 0 || raw[0] != '"' {
			return ErrMetadata
		}
		return nil
	}
	switch typ.Kind() {
	case reflect.Struct:
		var values map[string]json.RawMessage
		if json.Unmarshal(raw, &values) != nil || values == nil {
			return ErrMetadata
		}
		allowed := map[string]bool{}
		for i := 0; i < typ.NumField(); i++ {
			f := typ.Field(i)
			if !f.IsExported() {
				continue
			}
			tag := strings.Split(f.Tag.Get("json"), ",")
			name := tag[0]
			if name == "-" {
				continue
			}
			if name == "" {
				name = f.Name
			}
			allowed[name] = true
			v, ok := values[name]
			if !ok {
				if len(tag) > 1 && tag[1] == "omitempty" {
					continue
				}
				return ErrMetadata
			}
			if len(tag) > 1 && tag[1] == "omitempty" && bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
				return ErrMetadata
			}
			if shapeJSON(v, f.Type) != nil {
				return ErrMetadata
			}
		}
		for key := range values {
			if !allowed[key] {
				return ErrMetadata
			}
		}
	case reflect.Slice:
		var values []json.RawMessage
		if json.Unmarshal(raw, &values) != nil || values == nil {
			return ErrMetadata
		}
		for _, v := range values {
			if shapeJSON(v, typ.Elem()) != nil {
				return ErrMetadata
			}
		}
	case reflect.Bool:
		if !bytes.Equal(raw, []byte("true")) && !bytes.Equal(raw, []byte("false")) {
			return ErrMetadata
		}
	case reflect.String:
		if len(raw) == 0 || raw[0] != '"' {
			return ErrMetadata
		}
	case reflect.Int, reflect.Int64, reflect.Int32, reflect.Uint, reflect.Uint32, reflect.Uint64:
		if len(raw) == 0 || raw[0] == 'n' || bytes.ContainsAny(raw, ".eE") {
			return ErrMetadata
		}
	default:
		return ErrMetadata
	}
	return nil
}

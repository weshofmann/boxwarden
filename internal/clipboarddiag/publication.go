//go:build n1clipboarddiagnostic

package clipboarddiag

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

const MaxPublicationBytes = 512

// PublicationWitness attests preceding publication calls returned nil. It does
// not attest sender write/close, clipboard outcome, or caller acknowledgement.
// Field order and all eight mandatory fields are the fixed private wire schema.
type PublicationWitness struct {
	V          int     `json:"v"`
	Phase      string  `json:"phase"`
	Header     string  `json:"header"`
	Generation string  `json:"generation"`
	Created    bool    `json:"created"`
	Bootstrap  *string `json:"bootstrap"`
	Closure    *string `json:"closure"`
	Collect    *string `json:"collect"`
}

func MetadataDigest(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }

// GenerationMetadata has the exact existing generation.binding field order.
func GenerationMetadata(o Operation) []byte {
	value := struct {
		Version       int    `json:"version"`
		Domain        string `json:"domain"`
		SessionID     string `json:"session_id"`
		BackendKind   string `json:"backend_kind"`
		BackendObject string `json:"backend_object"`
		Generation    string `json:"generation"`
	}{1, o.Domain, o.SessionID, o.BackendKind, o.BackendObject, o.Generation}
	raw, _ := json.Marshal(value)
	return append(raw, '\n')
}
func GenerationDigest(o Operation) string { return MetadataDigest(GenerationMetadata(o)) }
func (w PublicationWitness) valid() bool {
	validHash := func(p *string) bool { return p != nil && hexPattern.MatchString(*p) }
	if w.V != 1 || !hexPattern.MatchString(w.Header) || !hexPattern.MatchString(w.Generation) {
		return false
	}
	switch w.Phase {
	case "invoke":
		return validHash(w.Bootstrap) && validHash(w.Closure) && w.Collect == nil
	case "collect":
		return !w.Created && w.Bootstrap == nil && w.Closure == nil && validHash(w.Collect)
	}
	return false
}
func EncodePublication(w PublicationWitness) ([]byte, error) {
	if !w.valid() {
		return nil, ErrMetadata
	}
	raw, e := json.Marshal(w)
	raw = append(raw, '\n')
	if e != nil || len(raw) > MaxPublicationBytes {
		return nil, ErrMetadata
	}
	return raw, nil
}
func DecodePublication(raw []byte, o Operation, phase string) (PublicationWitness, error) {
	var w PublicationWitness
	if o.Validate(false) != nil || StrictDecode(raw, &w, MaxPublicationBytes) != nil || !w.valid() || w.Phase != phase || w.Header != HeaderDigest(o) || w.Generation != GenerationDigest(o) {
		return PublicationWitness{}, ErrMetadata
	}
	want, e := EncodePublication(w)
	if e != nil || !bytes.Equal(want, raw) {
		return PublicationWitness{}, ErrMetadata
	}
	return w, nil
}

type publicationKey struct{}

// The observer belongs to the admitted host generation scope, never a request.
func WithPublicationObserver(ctx context.Context, observer func(PublicationWitness)) context.Context {
	return context.WithValue(ctx, publicationKey{}, observer)
}
func ObservePublication(ctx context.Context, w PublicationWitness) {
	if ctx == nil {
		return
	}
	if f, ok := ctx.Value(publicationKey{}).(func(PublicationWitness)); ok && f != nil {
		f(w)
	}
}
func (w PublicationWitness) MatchesGuest(o Operation, g GuestCollection) bool {
	if !w.valid() || w.Phase != "invoke" || w.Header != HeaderDigest(o) || w.Generation != GenerationDigest(o) || g.Validate(o) != nil || g.Bootstrap == nil || g.Overlay == nil || g.Overlay.Closure == nil {
		return false
	}
	bootstrap, _ := json.Marshal(g.Bootstrap)
	bootstrap = append(bootstrap, '\n')
	closure, e := EncodeClosure(*g.Overlay.Closure)
	return e == nil && *w.Bootstrap == MetadataDigest(bootstrap) && *w.Closure == MetadataDigest(closure)
}

package clipboardx

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestTextValidationPreservesBytes(t *testing.T) {
	for _, data := range [][]byte{{}, []byte("é雪\r\n\n\t "), bytes.Repeat([]byte("a"), 1048576)} {
		got, err := ReadText(bytes.NewReader(data))
		if err != nil || !bytes.Equal(got, data) {
			t.Fatalf("valid text altered or rejected: %v", err)
		}
	}
	for _, data := range [][]byte{{0xff}, {'x', 0}, bytes.Repeat([]byte("a"), 1048577)} {
		if err := Validate(data); err == nil {
			t.Fatal("invalid text accepted")
		}
		if _, err := ReadText(bytes.NewReader(data)); err == nil {
			t.Fatal("invalid stream accepted")
		}
	}
}
func TestReadTextBoundedAndSanitized(t *testing.T) {
	r := &countReader{}
	if _, err := ReadText(r); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("oversize error: %v", err)
	}
	if r.n > 1048577 {
		t.Fatalf("read %d bytes", r.n)
	}
	if _, err := ReadText(errorReader{}); !errors.Is(err, ErrRead) {
		t.Fatalf("unsanitized error: %v", err)
	}
}

type countReader struct{ n int }

func (r *countReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'a'
	}
	r.n += len(p)
	return len(p), nil
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) { return 0, errors.New("synthetic private payload") }
func TestTextFrameExactAndStrict(t *testing.T) {
	for _, data := range [][]byte{{}, []byte("雪\n\n"), bytes.Repeat([]byte("a"), 1048576)} {
		var b bytes.Buffer
		if err := WriteTextFrame(&b, data); err != nil {
			t.Fatal(err)
		}
		if b.Len() < 4 {
			t.Fatal("frame header absent")
		}
		if int(binary.BigEndian.Uint32(b.Bytes()[:4])) != len(data) {
			t.Fatal("wrong length")
		}
		got, err := ReadTextFrame(&b)
		if err != nil || !bytes.Equal(got, data) {
			t.Fatalf("frame: %v", err)
		}
	}
	for _, data := range [][]byte{{}, {0, 0, 0}, {0, 0, 0, 1}, {0, 0, 0, 0, 1}, {0, 16, 0, 1}, {0, 0, 0, 1, 0xff}, {0, 0, 0, 1, 0}} {
		if _, err := ReadTextFrame(bytes.NewReader(data)); err == nil {
			t.Fatal("malformed frame accepted")
		}
	}
	var b bytes.Buffer
	if err := WriteTextFrame(&b, []byte{0xff}); err == nil || b.Len() != 0 {
		t.Fatal("invalid write mutated destination")
	}
	if err := WriteTextFrame(shortWriter{}, []byte("a")); !errors.Is(err, ErrWrite) {
		t.Fatalf("short write: %v", err)
	}
	if _, err := ReadTextFrame(io.MultiReader(strings.NewReader("\x00\x00\x00\x00"), errorReader{})); !errors.Is(err, ErrFrame) {
		t.Fatalf("EOF error: %v", err)
	}
}

type shortWriter struct{}

func (shortWriter) Write([]byte) (int, error) { return 0, nil }

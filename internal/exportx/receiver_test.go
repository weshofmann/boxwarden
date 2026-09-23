package exportx

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var fixtureTransaction = [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}

func fixtureHeader() []byte {
	return append([]byte{'B', 'W', 'E', 'X', 0, 1}, fixtureTransaction[:]...)
}

// Each frame uses a fixed 47-byte header: kind, path length, chunk length,
// declared file/stream bytes, SHA-256; then path and chunk bytes.
func fixtureRecord(kind byte, path string, chunk []byte, declared uint64, digest [32]byte) []byte {
	var record bytes.Buffer
	record.WriteByte(kind)
	_ = binary.Write(&record, binary.BigEndian, uint16(len(path)))
	_ = binary.Write(&record, binary.BigEndian, uint32(len(chunk)))
	_ = binary.Write(&record, binary.BigEndian, declared)
	record.Write(digest[:])
	record.WriteString(path)
	record.Write(chunk)
	return record.Bytes()
}

func validStream() []byte {
	var b bytes.Buffer
	b.Write(fixtureHeader())
	b.Write(fixtureRecord(1, "project", nil, 0, [32]byte{}))
	b.Write(fixtureRecord(2, "project/README.txt", nil, 5, [32]byte{}))
	b.Write(fixtureRecord(3, "", []byte("he"), 0, [32]byte{}))
	b.Write(fixtureRecord(3, "", []byte("llo"), 0, [32]byte{}))
	b.Write(fixtureRecord(4, "", nil, 0, sha256.Sum256([]byte("hello"))))
	b.Write(fixtureRecord(5, "", nil, 5, [32]byte{}))
	return b.Bytes()
}

func fixtureOptions(t *testing.T) Options {
	t.Helper()
	parent := filepath.Join(t.TempDir(), "exports")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	return Options{
		Parent:         parent,
		TransactionID:  fixtureTransaction,
		MaxChunkBytes:  4,
		MaxFileBytes:   16,
		MaxTotalBytes:  32,
		MaxFiles:       4,
		MaxDirectories: 4,
		MinFreeBytes:   1,
	}
}

func receiveFixture(t *testing.T, stream []byte, options Options) (string, error) {
	t.Helper()
	return Receive(context.Background(), io.NopCloser(bytes.NewReader(stream)), options)
}

func assertNoPublication(t *testing.T, parent string) {
	t.Helper()
	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("rejected stream left entries: %v", entries)
	}
}

func TestReceivePublishesVerifiedTree(t *testing.T) {
	opts := fixtureOptions(t)
	path, err := receiveFixture(t, validStream(), opts)
	if err != nil {
		t.Fatal(err)
	}
	wantPath := filepath.Join(opts.Parent, hex.EncodeToString(fixtureTransaction[:]))
	if path != wantPath {
		t.Fatalf("path = %q, want %q", path, wantPath)
	}
	data, err := os.ReadFile(filepath.Join(path, "project", "README.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello" {
		t.Fatalf("content = %q", data)
	}
	for _, check := range []struct {
		path string
		mode os.FileMode
	}{{path, 0o700}, {filepath.Join(path, "project"), 0o700}, {filepath.Join(path, "project", "README.txt"), 0o600}} {
		info, err := os.Lstat(check.path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != check.mode {
			t.Errorf("%s mode = %04o, want %04o", check.path, info.Mode().Perm(), check.mode)
		}
	}
	entries, err := os.ReadDir(opts.Parent)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("published parent has %d entries, want 1", len(entries))
	}
}

func TestReceiveRejectsMalformedOrUnverifiedStreamWithoutPublication(t *testing.T) {
	base := validStream()
	badHash := append([]byte(nil), base...)
	badHash[len(badHash)-47-32] ^= 1 // Alter the file-end digest while keeping framing intact.
	badID := append([]byte(nil), base...)
	badID[6] ^= 1
	badVersion := append([]byte(nil), base...)
	badVersion[5] = 2
	badMagic := append([]byte(nil), base...)
	badMagic[0] = 'X'
	truncatedChunk := append(fixtureHeader(), fixtureRecord(2, "cut", nil, 4, [32]byte{})...)
	chunkRecord := fixtureRecord(3, "", []byte("four"), 0, [32]byte{})
	truncatedChunk = append(truncatedChunk, chunkRecord[:len(chunkRecord)-2]...)
	wrongLength := append(fixtureHeader(), fixtureRecord(2, "short", nil, 6, [32]byte{})...)
	wrongLength = append(wrongLength, fixtureRecord(3, "", []byte("hello"), 0, [32]byte{})...)
	wrongLength = append(wrongLength, fixtureRecord(4, "", nil, 0, sha256.Sum256([]byte("hello")))...)
	wrongLength = append(wrongLength, fixtureRecord(5, "", nil, 5, [32]byte{})...)
	for _, tc := range []struct {
		name string
		data []byte
	}{
		{"truncated chunk", truncatedChunk},
		{"missing terminal", base[:len(base)-47]},
		{"declared length mismatch", wrongLength},
		{"trailing bytes", append(append([]byte(nil), base...), 'x')},
		{"wrong digest", badHash},
		{"wrong transaction", badID},
		{"wrong version", badVersion},
		{"wrong magic", badMagic},
		{"unknown record type", append(append([]byte(nil), fixtureHeader()...), fixtureRecord(9, "", nil, 0, [32]byte{})...)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts := fixtureOptions(t)
			if _, err := receiveFixture(t, tc.data, opts); err == nil {
				t.Fatal("expected rejection")
			}
			assertNoPublication(t, opts.Parent)
		})
	}
}

func TestReceiveRejectsUnsafePathsAndCollisions(t *testing.T) {
	for _, tc := range []struct {
		name  string
		paths []string
	}{
		{"parent traversal", []string{"../escape"}},
		{"absolute", []string{"/escape"}},
		{"space", []string{"a b"}},
		{"backslash", []string{`a\b`}},
		{"empty component", []string{"a//b"}},
		{"case fold collision", []string{"Report", "report"}},
		{"parent case mismatch", []string{"Folder", "folder/child"}},
		{"parent file conflict", []string{"file", "file/child"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stream bytes.Buffer
			stream.Write(fixtureHeader())
			for i, path := range tc.paths {
				if tc.name == "parent file conflict" && i == 0 {
					stream.Write(fixtureRecord(2, path, nil, 0, [32]byte{}))
					stream.Write(fixtureRecord(4, "", nil, 0, sha256.Sum256(nil)))
				} else {
					stream.Write(fixtureRecord(1, path, nil, 0, [32]byte{}))
				}
			}
			stream.Write(fixtureRecord(5, "", nil, 0, [32]byte{}))
			opts := fixtureOptions(t)
			if _, err := receiveFixture(t, stream.Bytes(), opts); err == nil {
				t.Fatal("expected rejection")
			}
			assertNoPublication(t, opts.Parent)
		})
	}
}

func TestReceiveEnforcesChunkAndTotalLimits(t *testing.T) {
	for _, tc := range []struct {
		name     string
		maxChunk uint32
		maxTotal uint64
	}{
		{"chunk", 1, 32},
		{"total", 4, 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts := fixtureOptions(t)
			opts.MaxChunkBytes = tc.maxChunk
			opts.MaxTotalBytes = tc.maxTotal
			if _, err := receiveFixture(t, validStream(), opts); err == nil {
				t.Fatal("expected limit rejection")
			}
			assertNoPublication(t, opts.Parent)
		})
	}
}

func TestReceiveNoOverwriteAndPrivateParent(t *testing.T) {
	opts := fixtureOptions(t)
	dest := filepath.Join(opts.Parent, hex.EncodeToString(fixtureTransaction[:]))
	if err := os.Mkdir(dest, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dest, "sentinel"), []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := receiveFixture(t, validStream(), opts); err == nil {
		t.Fatal("expected existing destination rejection")
	}
	data, err := os.ReadFile(filepath.Join(dest, "sentinel"))
	if err != nil || string(data) != "keep" {
		t.Fatalf("existing destination changed: %q, %v", data, err)
	}
	entries, err := os.ReadDir(opts.Parent)
	if err != nil || len(entries) != 1 {
		t.Fatalf("staging leaked: %v, %v", entries, err)
	}

	other := fixtureOptions(t)
	if err := os.Chmod(other.Parent, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := receiveFixture(t, validStream(), other); err == nil {
		t.Fatal("expected nonprivate parent rejection")
	}
	if err := os.Chmod(other.Parent, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(other.Parent, link); err != nil {
		t.Fatal(err)
	}
	other.Parent = link
	if _, err := receiveFixture(t, validStream(), other); err == nil {
		t.Fatal("expected symlink parent rejection")
	}
}

func TestReceiveRejectsUnavailableSpaceAndCanceledContext(t *testing.T) {
	opts := fixtureOptions(t)
	opts.MinFreeBytes = ^uint64(0)
	if _, err := receiveFixture(t, validStream(), opts); err == nil {
		t.Fatal("expected free-space rejection")
	}
	assertNoPublication(t, opts.Parent)

	opts = fixtureOptions(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Receive(ctx, io.NopCloser(strings.NewReader(string(validStream()))), opts); err == nil {
		t.Fatal("expected canceled-context rejection")
	}
	assertNoPublication(t, opts.Parent)
}

type emptyReadOnce struct {
	reader *bytes.Reader
	empty  bool
}

func (r *emptyReadOnce) Read(p []byte) (int, error) {
	if !r.empty && r.reader.Len() == 0 {
		r.empty = true
		return 0, nil
	}
	return r.reader.Read(p)
}

func TestReceiveAllowsLegalZeroLengthReadBeforeEOF(t *testing.T) {
	opts := fixtureOptions(t)
	stream := io.NopCloser(&emptyReadOnce{reader: bytes.NewReader(validStream())})
	if _, err := Receive(context.Background(), stream, opts); err != nil {
		t.Fatal(err)
	}
}

type replaceParentAtTerminal struct {
	reader *bytes.Reader
	parent string
	moved  bool
}

func (r *replaceParentAtTerminal) Read(p []byte) (int, error) {
	if !r.moved && r.reader.Len() == 47 {
		r.moved = true
		if err := os.Rename(r.parent, r.parent+"-old"); err != nil {
			return 0, err
		}
		if err := os.Mkdir(r.parent, 0o700); err != nil {
			return 0, err
		}
	}
	return r.reader.Read(p)
}

func TestReceiveRejectsParentSubstitutionBeforePublication(t *testing.T) {
	opts := fixtureOptions(t)
	stream := io.NopCloser(&replaceParentAtTerminal{reader: bytes.NewReader(validStream()), parent: opts.Parent})
	if _, err := Receive(context.Background(), stream, opts); err == nil {
		t.Fatal("expected parent substitution rejection")
	}
	assertNoPublication(t, opts.Parent)
	assertNoPublication(t, opts.Parent+"-old")
}

type cancelAtEOF struct {
	reader *bytes.Reader
	cancel context.CancelFunc
}

func (r *cancelAtEOF) Read(p []byte) (int, error) {
	if r.reader.Len() == 0 {
		r.cancel()
	}
	return r.reader.Read(p)
}

func TestReceiveDoesNotPublishAfterCancellationAtTerminal(t *testing.T) {
	opts := fixtureOptions(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream := io.NopCloser(&cancelAtEOF{reader: bytes.NewReader(validStream()), cancel: cancel})
	if _, err := Receive(ctx, stream, opts); err == nil {
		t.Fatal("expected cancellation to prevent publication")
	}
	assertNoPublication(t, opts.Parent)
}

func TestReceiveRejectsOversizedRecordBeforeReadingPayload(t *testing.T) {
	for _, tc := range []struct {
		name   string
		prefix []byte
		record []byte
	}{
		{"chunk", fixtureRecord(2, "small", nil, 5, [32]byte{}), fixtureRecord(3, "", make([]byte, 5), 0, [32]byte{})},
		{"file declaration", nil, fixtureRecord(2, "large", nil, 17, [32]byte{})},
		{"missing parent", nil, fixtureRecord(1, "a/b", nil, 0, [32]byte{})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stream := append(fixtureHeader(), tc.prefix...)
			stream = append(stream, tc.record...)
			opts := fixtureOptions(t)
			if _, err := receiveFixture(t, stream, opts); err == nil {
				t.Fatal("expected rejection")
			}
			assertNoPublication(t, opts.Parent)
		})
	}
}

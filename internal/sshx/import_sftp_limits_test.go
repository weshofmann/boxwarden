package sshx

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/weshofmann/boxwarden/internal/importx"
)

type importAdmissionWriter struct{ calls int }

var errImportAdmissionReached = errors.New("admitted SFTP request")

func (w *importAdmissionWriter) Write([]byte) (int, error) {
	w.calls++
	return 0, errImportAdmissionReached
}

func TestSFTPReadbackLargerImportDeclarationAdmission(t *testing.T) {
	fileEntries := func(n int, size int64) []importx.Entry {
		entries := make([]importx.Entry, n)
		for i := range entries {
			entries[i] = importx.Entry{Path: fmt.Sprintf("f%04d", i), Kind: "file", Size: size}
		}
		return entries
	}
	directoryEntries := func(n int) []importx.Entry {
		entries := make([]importx.Entry, n)
		for i := range entries {
			entries[i] = importx.Entry{Path: fmt.Sprintf("d%04d", i), Kind: "directory"}
		}
		return entries
	}
	for _, tc := range []struct {
		name    string
		entries []importx.Entry
		pass    bool
	}{
		{"64 MiB file", fileEntries(1, 64<<20), true},
		{"256 MiB aggregate", fileEntries(4, 64<<20), true},
		{"4096 files and 2048 directories", append(fileEntries(4096, 0), directoryEntries(2048)...), true},
		{"file too large", fileEntries(1, (64<<20)+1), false},
		{"aggregate too large", append(fileEntries(4, 64<<20), fileEntries(1, 1)...), false},
		{"too many files", fileEntries(4097, 0), false},
		{"too many directories", directoryEntries(2049), false},
		{"negative file length", fileEntries(1, -1), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			writer := &importAdmissionWriter{}
			err := readbackSFTP(context.Background(), bytes.NewReader(nil), writer, tc.entries, "/remote", privateRoot(t))
			if tc.pass {
				if !errors.Is(err, errImportAdmissionReached) || writer.calls != 1 {
					t.Fatalf("bounded manifest did not reach protocol: %v, writes=%d", err, writer.calls)
				}
			} else if err == nil || errors.Is(err, errImportAdmissionReached) || writer.calls != 0 {
				t.Fatalf("hostile declaration reached protocol: %v, writes=%d", err, writer.calls)
			}
		})
	}
}

func TestSFTPReadback64MiBStreamsThroughBoundedRequests(t *testing.T) {
	const server = "/usr/libexec/sftp-server"
	if _, err := os.Stat(server); err != nil {
		t.Skipf("local OpenSSH SFTP server unavailable: %v", err)
	}
	remote, local := privateRoot(t), privateRoot(t)
	file, err := os.OpenFile(filepath.Join(remote, "large.bin"), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(64 << 20); err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteAt([]byte("last"), (64<<20)-4); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := runReadbackProcess(ctx, server, nil, func(reader io.Reader, writer io.Writer) error {
		return readbackSFTP(ctx, reader, writer, []importx.Entry{{Path: "large.bin", Kind: "file", Size: 64 << 20}}, remote, local)
	}); err != nil {
		t.Fatal(err)
	}
	got, err := os.Open(filepath.Join(local, "large.bin"))
	if err != nil {
		t.Fatal(err)
	}
	defer got.Close()
	info, err := got.Stat()
	if err != nil || info.Size() != 64<<20 {
		t.Fatalf("readback size = %v, %v", info, err)
	}
	tail := make([]byte, 4)
	if _, err := got.ReadAt(tail, (64<<20)-4); err != nil || string(tail) != "last" {
		t.Fatalf("readback tail = %q, %v", tail, err)
	}
}

func TestSFTPReplyRejectsLengthBeforeReadingBody(t *testing.T) {
	for _, length := range []uint32{65537, ^uint32(0)} {
		header := []byte{0, 0, 0, 0, sftpFXPData}
		binary.BigEndian.PutUint32(header[:4], length)
		reader := bytes.NewReader(append(header, []byte("unread body")...))
		protocol := sftpReadProtocol{reader: reader, writer: io.Discard}
		if _, _, err := protocol.receive(); err == nil || reader.Len() != len("unread body") {
			t.Fatalf("oversized reply read body: remaining=%d, err=%v", reader.Len(), err)
		}
	}
}

func TestSFTPUploadBatchAdmitsBoundedWorstPathMetadata(t *testing.T) {
	snapshot := importx.Snapshot{Directory: "/stage"}
	// Both local and remote names stay within their existing 4096-byte envelope.
	// The maximum 6144 entries produce about 49 MiB of command metadata.
	prefix := strings.Repeat("p", 4000)
	for i := 0; i < 6144; i++ {
		kind := "file"
		if i < 2048 {
			kind = "directory"
		}
		snapshot.Entries = append(snapshot.Entries, importx.Entry{Path: prefix + fmt.Sprintf("%04d", i), Kind: kind})
	}
	batch, err := importUploadBatch(snapshot, "/remote")
	if err != nil {
		t.Fatal(err)
	}
	if len(batch) <= 1<<20 || len(batch) > 64<<20 || bytes.Count(batch, []byte("put -f ")) != 4096 || bytes.Count(batch, []byte("-mkdir ")) != 2049 {
		t.Fatalf("wrong bounded command batch: bytes=%d", len(batch))
	}
	snapshot.Entries = append(snapshot.Entries, importx.Entry{Path: "extra", Kind: "file"})
	if _, err := importUploadBatch(snapshot, "/remote"); err == nil {
		t.Fatal("oversized metadata entry list admitted")
	}
}

func TestSFTPCommandRejectsAbove64MiBMetadataBeforeRunner(t *testing.T) {
	runner := &fakeRunner{onRun: func(Command) Result { return Result{} }}
	client := newSFTPClient(runner)
	if err := client.runSFTP(context.Background(), testConnection(t), make([]byte, (64<<20)+1)); err == nil || len(runner.commands) != 0 {
		t.Fatalf("oversized batch reached runner: %v, commands=%d", err, len(runner.commands))
	}
}

func TestSFTPReadbackRejectsChunkAbove32KiBBeforeWrite(t *testing.T) {
	responses := bytes.Join([][]byte{
		testSFTPPacket(sftpFXPVersion, []byte{0, 0, 0, 3}),
		testSFTPReply(sftpFXPHandle, 1, testSFTPString([]byte("handle"))),
		testSFTPReply(sftpFXPData, 2, testSFTPString(make([]byte, 32769))),
	}, nil)
	root := privateRoot(t)
	err := readbackSFTP(context.Background(), bytes.NewReader(responses), io.Discard, []importx.Entry{{Path: "file", Kind: "file", Size: 1 << 20}}, "/remote", root)
	if err == nil || !strings.Contains(err.Error(), "packet bound") {
		t.Fatalf("oversized data chunk accepted: %v", err)
	}
	info, statErr := os.Stat(filepath.Join(root, "file"))
	if statErr != nil || info.Size() != 0 {
		t.Fatalf("oversized chunk reached host file: %v, %v", info, statErr)
	}
}

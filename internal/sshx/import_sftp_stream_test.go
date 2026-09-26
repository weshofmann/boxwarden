package sshx

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/weshofmann/boxwarden/internal/importx"
)

func testSFTPPacket(kind byte, payload []byte) []byte {
	packet := make([]byte, 5+len(payload))
	binary.BigEndian.PutUint32(packet, uint32(len(payload)+1))
	packet[4] = kind
	copy(packet[5:], payload)
	return packet
}

func testSFTPString(value []byte) []byte {
	data := make([]byte, 4+len(value))
	binary.BigEndian.PutUint32(data, uint32(len(value)))
	copy(data[4:], value)
	return data
}

func testSFTPReply(kind byte, id uint32, payload []byte) []byte {
	data := make([]byte, 4+len(payload))
	binary.BigEndian.PutUint32(data, id)
	copy(data[4:], payload)
	return testSFTPPacket(kind, data)
}

func testSFTPStatus(id, code uint32) []byte {
	data := make([]byte, 4)
	binary.BigEndian.PutUint32(data, code)
	return testSFTPReply(sftpFXPStatus, id, data)
}

func TestSFTPReadbackRejectsOversizeBeforeHostWrite(t *testing.T) {
	for _, test := range []struct {
		name      string
		responses []byte
	}{
		{"extra byte", bytes.Join([][]byte{
			testSFTPPacket(sftpFXPVersion, []byte{0, 0, 0, 3}),
			testSFTPReply(sftpFXPHandle, 1, testSFTPString([]byte("handle"))),
			testSFTPReply(sftpFXPData, 2, testSFTPString([]byte("12345"))),
		}, nil)},
		{"oversize packet", bytes.Join([][]byte{
			testSFTPPacket(sftpFXPVersion, []byte{0, 0, 0, 3}),
			testSFTPReply(sftpFXPHandle, 1, testSFTPString([]byte("handle"))),
			{0xff, 0xff, 0xff, 0xff, sftpFXPData},
		}, nil)},
		{"truncated packet", bytes.Join([][]byte{
			testSFTPPacket(sftpFXPVersion, []byte{0, 0, 0, 3}),
			testSFTPReply(sftpFXPHandle, 1, testSFTPString([]byte("handle"))),
			{0, 0, 0, 10, sftpFXPData, 0, 0, 0, 2},
		}, nil)},
		{"early EOF", bytes.Join([][]byte{
			testSFTPPacket(sftpFXPVersion, []byte{0, 0, 0, 3}),
			testSFTPReply(sftpFXPHandle, 1, testSFTPString([]byte("handle"))),
			testSFTPStatus(2, sftpFXEOF),
		}, nil)},
		{"failure status", bytes.Join([][]byte{
			testSFTPPacket(sftpFXPVersion, []byte{0, 0, 0, 3}),
			testSFTPReply(sftpFXPHandle, 1, testSFTPString([]byte("handle"))),
			testSFTPStatus(2, 4),
		}, nil)},
		{"wrong request ID", bytes.Join([][]byte{
			testSFTPPacket(sftpFXPVersion, []byte{0, 0, 0, 3}),
			testSFTPReply(sftpFXPHandle, 9, testSFTPString([]byte("handle"))),
		}, nil)},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := privateRoot(t)
			entries := []importx.Entry{{Path: "a.txt", Kind: "file", Size: 4}}
			var requests bytes.Buffer
			err := readbackSFTP(context.Background(), bytes.NewReader(test.responses), &requests, entries, "/home/boxwarden/import", root)
			if err == nil {
				t.Fatal("hostile SFTP reply accepted")
			}
			if test.name == "oversize packet" && !strings.Contains(err.Error(), "packet bound") {
				t.Fatalf("oversize fixture did not reach packet length guard: %v", err)
			}
			info, statErr := os.Stat(filepath.Join(root, "a.txt"))
			if statErr == nil && info.Size() > 4 {
				t.Fatalf("host wrote %d bytes beyond declared 4", info.Size())
			}
		})
	}
}

func TestSFTPReadbackExactBytesAndPath(t *testing.T) {
	responses := bytes.Join([][]byte{
		testSFTPPacket(sftpFXPVersion, []byte{0, 0, 0, 3}),
		testSFTPReply(sftpFXPHandle, 1, testSFTPString([]byte("handle"))),
		testSFTPReply(sftpFXPData, 2, testSFTPString([]byte("data"))),
		testSFTPStatus(3, sftpFXEOF),
		testSFTPStatus(4, sftpFXOK),
	}, nil)
	root := privateRoot(t)
	var requests bytes.Buffer
	entries := []importx.Entry{{Path: "a.txt", Kind: "file", Size: 4}}
	if err := readbackSFTP(context.Background(), bytes.NewReader(responses), &requests, entries, "/home/boxwarden/import", root); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(root, "a.txt"))
	if err != nil || string(content) != "data" {
		t.Fatalf("readback = %q, %v", content, err)
	}
	if !bytes.Contains(requests.Bytes(), testSFTPString([]byte("/home/boxwarden/import/a.txt"))) {
		t.Fatal("SFTP request lacked exact remote path")
	}
}

func TestSFTPReadbackRejectsEscapingEntryPath(t *testing.T) {
	responses := bytes.Join([][]byte{
		testSFTPPacket(sftpFXPVersion, []byte{0, 0, 0, 3}),
		testSFTPReply(sftpFXPHandle, 1, testSFTPString([]byte("handle"))),
		testSFTPReply(sftpFXPData, 2, testSFTPString([]byte("data"))),
		testSFTPStatus(3, sftpFXEOF),
		testSFTPStatus(4, sftpFXOK),
	}, nil)
	parent := privateRoot(t)
	local := filepath.Join(parent, "readback")
	if err := os.Mkdir(local, 0o700); err != nil {
		t.Fatal(err)
	}
	var requests bytes.Buffer
	entries := []importx.Entry{{Path: "../escape", Kind: "file", Size: 4}}
	if err := readbackSFTP(context.Background(), bytes.NewReader(responses), &requests, entries, "/home/boxwarden/import", local); err == nil {
		t.Fatal("escaping readback path accepted")
	}
	if _, err := os.Stat(filepath.Join(parent, "escape")); !os.IsNotExist(err) {
		t.Fatalf("readback escaped private directory: %v", err)
	}
}

type zeroWriteSFTP struct{}

func (zeroWriteSFTP) Write([]byte) (int, error) { return 0, nil }

func TestSFTPReadbackRejectsShortProtocolWrite(t *testing.T) {
	response := testSFTPPacket(sftpFXPVersion, []byte{0, 0, 0, 3})
	if err := readbackSFTP(context.Background(), bytes.NewReader(response), zeroWriteSFTP{}, nil, "/home/boxwarden/import", privateRoot(t)); err == nil {
		t.Fatal("short SFTP request write accepted")
	}
}

func TestSFTPReadbackProcessCancellationReaps(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := runReadbackProcess(ctx, "/bin/sleep", []string{"30"}, func(reader io.Reader, writer io.Writer) error {
		_, err := io.ReadFull(reader, make([]byte, 1))
		return err
	})
	if err == nil || time.Since(start) > 3*time.Second || !strings.Contains(err.Error(), "context") {
		t.Fatalf("cancellation did not promptly reap child: %v", err)
	}
}

func TestSFTPReadbackUsesStrictPinnedSSHArguments(t *testing.T) {
	connection := testConnection(t)
	arguments := sftpReadbackArguments(connection)
	prefix := strictOpenSSHArguments(connection)
	if len(arguments) != len(prefix)+5 || !sameStrings(arguments[:len(prefix)], prefix) ||
		!sameStrings(arguments[len(prefix):], []string{"-p", "22", "-s", "boxwarden@" + connection.Address, "sftp"}) {
		t.Fatalf("readback argv differs from strict pinned SSH contract: %q", arguments)
	}
}

func TestSFTPReadbackAgainstLocalOpenSSHServer(t *testing.T) {
	const server = "/usr/libexec/sftp-server"
	if runtime.GOOS != "darwin" {
		t.Skip("qualified local OpenSSH server path is macOS-specific")
	}
	if _, err := os.Stat(server); err != nil {
		t.Skipf("local OpenSSH SFTP server unavailable: %v", err)
	}
	remote := privateRoot(t)
	local := privateRoot(t)
	if err := os.WriteFile(filepath.Join(remote, "a.txt"), []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	entries := []importx.Entry{{Path: "a.txt", Kind: "file", Size: 4}}
	if err := runReadbackProcess(ctx, server, nil, func(reader io.Reader, writer io.Writer) error {
		return readbackSFTP(ctx, reader, writer, entries, remote, local)
	}); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(local, "a.txt"))
	if err != nil || string(content) != "data" {
		t.Fatalf("local OpenSSH readback = %q, %v", content, err)
	}
}

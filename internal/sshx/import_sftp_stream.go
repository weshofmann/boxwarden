package sshx

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/weshofmann/boxwarden/internal/importx"
)

// Only the sequential read operations of SFTP v3 are needed for independent
// host readback. Each response is bounded before allocation, and no guest
// supplied byte is written after the declared file or snapshot limit.
const (
	sftpFXPInit    = 1
	sftpFXPVersion = 2
	sftpFXPOpen    = 3
	sftpFXPClose   = 4
	sftpFXPRead    = 5
	sftpFXPStatus  = 101
	sftpFXPHandle  = 102
	sftpFXPData    = 103
	sftpFXOK       = 0
	sftpFXEOF      = 1
	sftpMaxPacket  = 64 << 10
	sftpReadChunk  = 32 << 10
	sftpMaxFile    = 4 << 20
	sftpMaxTotal   = 16 << 20
)

func runPinnedSFTPReadback(ctx context.Context, connection Connection, entries []importx.Entry, remote, local string) error {
	if err := verifyKnownHostsPin(connection); err != nil {
		return err
	}
	arguments := sftpReadbackArguments(connection)
	return runReadbackProcess(ctx, sshPath, arguments, func(reader io.Reader, writer io.Writer) error {
		return readbackSFTP(ctx, reader, writer, entries, remote, local)
	})
}

func sftpReadbackArguments(connection Connection) []string {
	return append(strictOpenSSHArguments(connection), "-p", fmt.Sprint(connection.Port), "-s", "boxwarden@"+connection.Address, "sftp")
}

func runReadbackProcess(ctx context.Context, path string, arguments []string, transfer func(io.Reader, io.Writer) error) error {
	process := exec.CommandContext(ctx, path, arguments...)
	process.Env = []string{"LC_ALL=C", "LANG=C", "TZ=UTC"}
	stdin, err := process.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := process.StdoutPipe()
	if err != nil {
		return err
	}
	var stderr boundedSFTPStderr
	process.Stderr = &stderr
	if err := process.Start(); err != nil {
		return err
	}
	transferErr := transfer(stdout, stdin)
	if transferErr != nil {
		_ = process.Process.Kill()
	}
	_ = stdin.Close()
	_ = stdout.Close()
	waitErr := process.Wait()
	if ctx.Err() != nil {
		return fmt.Errorf("SFTP readback context: %w", ctx.Err())
	}
	if transferErr != nil {
		return fmt.Errorf("SFTP readback protocol: %w", transferErr)
	}
	if waitErr != nil {
		return fmt.Errorf("SFTP readback process: %w", waitErr)
	}
	if stderr.truncated {
		return fmt.Errorf("SFTP readback diagnostic output exceeded bound")
	}
	return nil
}

type boundedSFTPStderr struct {
	data      []byte
	truncated bool
}

func (b *boundedSFTPStderr) Write(input []byte) (int, error) {
	const limit = 256 << 10
	if len(input) > limit-len(b.data) {
		b.truncated = true
	}
	remaining := limit - len(b.data)
	if remaining > len(input) {
		remaining = len(input)
	}
	b.data = append(b.data, input[:remaining]...)
	return len(input), nil
}

type sftpReadProtocol struct {
	reader io.Reader
	writer io.Writer
	nextID uint32
}

func readbackSFTP(ctx context.Context, reader io.Reader, writer io.Writer, entries []importx.Entry, remote, local string) error {
	if !safeSFTPPath(remote) || !safeSFTPPath(local) {
		return fmt.Errorf("invalid SFTP readback roots")
	}
	var total int64
	var files int
	for _, entry := range entries {
		if !filepath.IsLocal(entry.Path) || filepath.Clean(entry.Path) != entry.Path || !safeSFTPPath(remote+"/"+entry.Path) {
			return fmt.Errorf("invalid SFTP readback entry path")
		}
		if entry.Kind == "directory" {
			continue
		}
		if entry.Kind != "file" || entry.Size < 0 || entry.Size > sftpMaxFile || files >= 256 || total > sftpMaxTotal-entry.Size {
			return fmt.Errorf("invalid readback file bounds")
		}
		files++
		total += entry.Size
	}
	protocol := sftpReadProtocol{reader: reader, writer: writer}
	if err := protocol.send(sftpFXPInit, []byte{0, 0, 0, 3}); err != nil {
		return err
	}
	kind, payload, err := protocol.receive()
	if err != nil || kind != sftpFXPVersion || len(payload) < 4 || binary.BigEndian.Uint32(payload[:4]) != 3 {
		return fmt.Errorf("invalid SFTP version reply: %v", err)
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.Kind == "directory" {
			continue
		}
		if err := protocol.readFile(remote+"/"+entry.Path, filepath.Join(local, filepath.FromSlash(entry.Path)), entry.Size); err != nil {
			return err
		}
	}
	return nil
}

func (p *sftpReadProtocol) readFile(remote, local string, size int64) error {
	var request bytes.Buffer
	putSFTPString(&request, []byte(remote))
	writeSFTPUint32(&request, 1) // SSH_FXF_READ
	writeSFTPUint32(&request, 0) // empty attributes
	id, err := p.request(sftpFXPOpen, request.Bytes())
	if err != nil {
		return err
	}
	kind, payload, err := p.reply(id)
	if err != nil || kind != sftpFXPHandle {
		return fmt.Errorf("SFTP open failed: %v", err)
	}
	handle, tail, err := takeSFTPString(payload)
	if err != nil || len(tail) != 0 || len(handle) == 0 || len(handle) > 256 {
		return fmt.Errorf("invalid SFTP handle")
	}
	file, err := os.OpenFile(local, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	var offset int64
	for {
		length := int64(sftpReadChunk)
		if size-offset+1 < length {
			length = size - offset + 1
		}
		request.Reset()
		putSFTPString(&request, handle)
		writeSFTPUint64(&request, uint64(offset))
		writeSFTPUint32(&request, uint32(length))
		id, err := p.request(sftpFXPRead, request.Bytes())
		if err != nil {
			return err
		}
		kind, payload, err := p.reply(id)
		if err != nil {
			return err
		}
		if kind == sftpFXPStatus {
			if len(payload) < 4 || binary.BigEndian.Uint32(payload[:4]) != sftpFXEOF || offset != size {
				return fmt.Errorf("SFTP read ended before declared size or with failure")
			}
			break
		}
		if kind != sftpFXPData {
			return fmt.Errorf("invalid SFTP read response")
		}
		data, tail, err := takeSFTPString(payload)
		if err != nil || len(tail) != 0 || len(data) == 0 || int64(len(data)) > size-offset || int64(len(data)) > length {
			return fmt.Errorf("SFTP read exceeded declared file size or packet bound")
		}
		if err := writeSFTPBytes(file, data); err != nil {
			return err
		}
		offset += int64(len(data))
	}
	request.Reset()
	putSFTPString(&request, handle)
	id, err = p.request(sftpFXPClose, request.Bytes())
	if err != nil {
		return err
	}
	kind, payload, err = p.reply(id)
	if err != nil || kind != sftpFXPStatus || len(payload) < 4 || binary.BigEndian.Uint32(payload[:4]) != sftpFXOK {
		return fmt.Errorf("SFTP close failed: %v", err)
	}
	return file.Close()
}

func (p *sftpReadProtocol) request(kind byte, payload []byte) (uint32, error) {
	p.nextID++
	var body bytes.Buffer
	writeSFTPUint32(&body, p.nextID)
	body.Write(payload)
	return p.nextID, p.send(kind, body.Bytes())
}

func (p *sftpReadProtocol) send(kind byte, payload []byte) error {
	if len(payload)+1 > sftpMaxPacket {
		return fmt.Errorf("SFTP request exceeds packet bound")
	}
	var header [5]byte
	binary.BigEndian.PutUint32(header[:4], uint32(len(payload)+1))
	header[4] = kind
	if err := writeSFTPBytes(p.writer, header[:]); err != nil {
		return err
	}
	return writeSFTPBytes(p.writer, payload)
}

func writeSFTPBytes(writer io.Writer, data []byte) error {
	for len(data) > 0 {
		n, err := writer.Write(data)
		if err != nil {
			return err
		}
		if n <= 0 || n > len(data) {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	return nil
}

func (p *sftpReadProtocol) receive() (byte, []byte, error) {
	var header [5]byte
	if _, err := io.ReadFull(p.reader, header[:]); err != nil {
		return 0, nil, err
	}
	length := binary.BigEndian.Uint32(header[:4])
	if length < 1 || length > sftpMaxPacket {
		return 0, nil, fmt.Errorf("SFTP response exceeds packet bound")
	}
	payload := make([]byte, int(length)-1)
	_, err := io.ReadFull(p.reader, payload)
	return header[4], payload, err
}

func (p *sftpReadProtocol) reply(id uint32) (byte, []byte, error) {
	kind, payload, err := p.receive()
	if err != nil {
		return 0, nil, err
	}
	if len(payload) < 4 || binary.BigEndian.Uint32(payload[:4]) != id {
		return 0, nil, fmt.Errorf("SFTP response ID mismatch")
	}
	return kind, payload[4:], nil
}

func takeSFTPString(payload []byte) ([]byte, []byte, error) {
	if len(payload) < 4 {
		return nil, nil, fmt.Errorf("short SFTP string")
	}
	length := binary.BigEndian.Uint32(payload[:4])
	if length > uint32(len(payload)-4) {
		return nil, nil, fmt.Errorf("invalid SFTP string length")
	}
	return payload[4 : 4+length], payload[4+length:], nil
}

func putSFTPString(buffer *bytes.Buffer, value []byte) {
	writeSFTPUint32(buffer, uint32(len(value)))
	buffer.Write(value)
}

func writeSFTPUint32(buffer *bytes.Buffer, value uint32) {
	var data [4]byte
	binary.BigEndian.PutUint32(data[:], value)
	buffer.Write(data[:])
}

func writeSFTPUint64(buffer *bytes.Buffer, value uint64) {
	var data [8]byte
	binary.BigEndian.PutUint64(data[:], value)
	buffer.Write(data[:])
}

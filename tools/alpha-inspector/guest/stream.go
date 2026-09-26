package main

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
)

const reportName = "report.json"
const maxReportBytes = 4096

func parseTransaction(commandLine string) ([16]byte, error) {
	var result [16]byte
	found := false
	for _, field := range strings.Fields(commandLine) {
		if !strings.HasPrefix(field, "alpha_tx=") {
			continue
		}
		if found {
			return result, fmt.Errorf("duplicate alpha transaction")
		}
		found = true
		value := strings.TrimPrefix(field, "alpha_tx=")
		if len(value) != 32 || strings.ToLower(value) != value {
			return result, fmt.Errorf("invalid alpha transaction")
		}
		decoded, err := hex.DecodeString(value)
		if err != nil {
			return result, fmt.Errorf("invalid alpha transaction: %w", err)
		}
		copy(result[:], decoded)
	}
	if !found || result == [16]byte{} {
		return result, fmt.Errorf("missing alpha transaction")
	}
	return result, nil
}

func writeReport(writer io.Writer, transaction [16]byte, body []byte) error {
	if transaction == [16]byte{} || len(body) == 0 || len(body) > maxReportBytes {
		return fmt.Errorf("invalid proof report")
	}
	var header [22]byte
	copy(header[:4], "BWEX")
	binary.BigEndian.PutUint16(header[4:6], 1)
	copy(header[6:], transaction[:])
	if err := writeAll(writer, header[:]); err != nil {
		return err
	}
	if err := writeFrame(writer, 2, reportName, nil, uint64(len(body)), [32]byte{}); err != nil {
		return err
	}
	if err := writeFrame(writer, 3, "", body, 0, [32]byte{}); err != nil {
		return err
	}
	if err := writeFrame(writer, 4, "", nil, 0, sha256.Sum256(body)); err != nil {
		return err
	}
	return writeFrame(writer, 5, "", nil, uint64(len(body)), [32]byte{})
}

func writeFrame(writer io.Writer, kind byte, name string, payload []byte, declared uint64, digest [32]byte) error {
	var header [47]byte
	header[0] = kind
	binary.BigEndian.PutUint16(header[1:3], uint16(len(name)))
	binary.BigEndian.PutUint32(header[3:7], uint32(len(payload)))
	binary.BigEndian.PutUint64(header[7:15], declared)
	copy(header[15:], digest[:])
	for _, section := range [][]byte{header[:], []byte(name), payload} {
		if err := writeAll(writer, section); err != nil {
			return err
		}
	}
	return nil
}

func writeAll(writer io.Writer, data []byte) error {
	for len(data) > 0 {
		count, err := writer.Write(data)
		if err != nil {
			return err
		}
		if count <= 0 || count > len(data) {
			return io.ErrShortWrite
		}
		data = data[count:]
	}
	return nil
}

package sshx

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/weshofmann/boxwarden/internal/execx"
	"github.com/weshofmann/boxwarden/internal/importx"
)

const (
	sftpPath              = "/usr/bin/sftp"
	maxImportBatchBytes   = 1 << 20
	maxImportTransferTime = 10 * time.Minute
)

// ImportReceipt describes bytes read back to a private host directory after
// SFTP upload. A caller must still recheck the exact live guest binding before
// it promotes a journal to verified.
type ImportReceipt struct {
	Digest     string
	FileCount  int
	TotalBytes int64
	RemotePath string
}

type SFTPClient struct{ runner Runner }

func NewSFTPClient() *SFTPClient {
	return &SFTPClient{runner: newExecRunner(execx.OSRunner{MaxOutputBytes: 256 << 10, MaxStdinBytes: maxImportBatchBytes})}
}

func newSFTPClient(runner Runner) *SFTPClient { return &SFTPClient{runner: runner} }

// TransferImport sends only one re-admitted private snapshot to a transaction
// directory on an already mounted workspace. It then gets every file into a
// fresh private host tree and re-admits the exact bytes against the manifest.
func (c *SFTPClient) TransferImport(ctx context.Context, connection Connection, stagingParent, transactionID, sourceDigest, mountPath string) (receipt ImportReceipt, err error) {
	if c == nil || c.runner == nil || !validWorkspaceMountPath(mountPath) || !validImportUUID(transactionID) || !validImportDigest(sourceDigest) {
		return ImportReceipt{}, fmt.Errorf("invalid bounded import transfer")
	}
	if err := validateConnection(connection); err != nil {
		return ImportReceipt{}, err
	}
	if err := verifyKnownHostsPin(connection); err != nil {
		return ImportReceipt{}, fmt.Errorf("SFTP known-host pin: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, maxImportTransferTime)
	defer cancel()
	snapshot, err := importx.InspectSnapshot(stagingParent, transactionID)
	if err != nil || snapshot.Digest != sourceDigest {
		return ImportReceipt{}, fmt.Errorf("import source snapshot differs before transfer: %v", err)
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return ImportReceipt{}, err
	}
	readbackParent, err := os.MkdirTemp(stagingParent, ".readback-"+transactionID+"-"+hex.EncodeToString(nonce[:])+"-")
	if err != nil {
		return ImportReceipt{}, err
	}
	readbackInfo, err := os.Lstat(readbackParent)
	if err != nil {
		return ImportReceipt{}, err
	}
	defer func() {
		current, statErr := os.Lstat(readbackParent)
		if statErr != nil || !os.SameFile(readbackInfo, current) {
			err = errors.Join(err, fmt.Errorf("import readback directory changed before cleanup: %v", statErr))
			return
		}
		err = errors.Join(err, os.RemoveAll(readbackParent))
	}()
	readbackDirectory := filepath.Join(readbackParent, transactionID)
	if err := os.Mkdir(readbackDirectory, 0o700); err != nil {
		return ImportReceipt{}, err
	}
	for _, entry := range snapshot.Entries {
		if entry.Kind == "directory" {
			if err := os.Mkdir(filepath.Join(readbackDirectory, filepath.FromSlash(entry.Path)), 0o700); err != nil {
				return ImportReceipt{}, err
			}
		}
	}
	remote := mountPath + "/boxwarden-import-" + transactionID
	upload, download, err := importBatches(snapshot, readbackDirectory, remote)
	if err != nil {
		return ImportReceipt{}, err
	}
	if err := c.runSFTP(ctx, connection, upload); err != nil {
		return ImportReceipt{}, fmt.Errorf("upload bounded import: %w", err)
	}
	current, err := importx.InspectSnapshot(stagingParent, transactionID)
	if err != nil || current.Digest != sourceDigest || current.FileCount != snapshot.FileCount || current.TotalBytes != snapshot.TotalBytes {
		return ImportReceipt{}, fmt.Errorf("import source changed while uploading: %v", err)
	}
	if err := c.runSFTP(ctx, connection, download); err != nil {
		return ImportReceipt{}, fmt.Errorf("read back bounded import: %w", err)
	}
	for _, entry := range snapshot.Entries {
		if entry.Kind == "file" {
			if err := os.Chmod(filepath.Join(readbackDirectory, filepath.FromSlash(entry.Path)), 0o600); err != nil {
				return ImportReceipt{}, err
			}
		}
	}
	raw, err := json.Marshal(struct {
		Version int             `json:"version"`
		Entries []importx.Entry `json:"entries"`
	}{Version: 1, Entries: snapshot.Entries})
	if err != nil {
		return ImportReceipt{}, err
	}
	if err := os.WriteFile(filepath.Join(readbackDirectory, ".boxwarden-import-manifest.json"), append(raw, '\n'), 0o600); err != nil {
		return ImportReceipt{}, err
	}
	readback, err := importx.InspectSnapshot(readbackParent, transactionID)
	if err != nil || readback.Digest != sourceDigest || readback.FileCount != snapshot.FileCount || readback.TotalBytes != snapshot.TotalBytes {
		return ImportReceipt{}, fmt.Errorf("import host readback differs from captured source: %v", err)
	}
	return ImportReceipt{Digest: readback.Digest, FileCount: readback.FileCount, TotalBytes: readback.TotalBytes, RemotePath: remote}, nil
}

func (c *SFTPClient) runSFTP(ctx context.Context, connection Connection, batch []byte) error {
	if len(batch) == 0 || len(batch) > maxImportBatchBytes {
		return fmt.Errorf("SFTP batch exceeds bound")
	}
	if err := verifyKnownHostsPin(connection); err != nil {
		return err
	}
	result, err := c.runner.Run(ctx, Command{Path: sftpPath, Args: sftpArguments(connection), Stdin: batch})
	if err != nil {
		return err
	}
	if result.Truncated {
		return fmt.Errorf("SFTP diagnostic output exceeded bound")
	}
	return nil
}

func sftpArguments(connection Connection) []string {
	arguments := strictOpenSSHArguments(connection)
	address := connection.Address
	if strings.Contains(address, ":") {
		address = "[" + address + "]"
	}
	return append(arguments, "-q", "-b", "-", "-P", strconv.Itoa(int(connection.Port)), "boxwarden@"+address)
}

func importBatches(snapshot importx.Snapshot, readbackDirectory, remote string) ([]byte, []byte, error) {
	if !safeSFTPPath(snapshot.Directory) || !safeSFTPPath(readbackDirectory) || !safeSFTPPath(remote) {
		return nil, nil, fmt.Errorf("import path cannot be represented safely in SFTP batch")
	}
	var upload, download strings.Builder
	// macOS OpenSSH sftp accepts only `mkdir path`. Ignore an existing
	// transaction directory on retry, then require that it is traversable.
	upload.WriteString("-mkdir " + quoteSFTPPath(remote) + "\ncd " + quoteSFTPPath(remote) + "\n")
	for _, entry := range snapshot.Entries {
		remotePath := remote + "/" + entry.Path
		if !safeSFTPPath(remotePath) {
			return nil, nil, fmt.Errorf("unsafe import entry path")
		}
		if entry.Kind == "directory" {
			upload.WriteString("-mkdir " + quoteSFTPPath(remotePath) + "\ncd " + quoteSFTPPath(remotePath) + "\n")
			continue
		}
		if entry.Kind != "file" {
			return nil, nil, fmt.Errorf("invalid import entry kind")
		}
		localSource := filepath.Join(snapshot.Directory, filepath.FromSlash(entry.Path))
		localReadback := filepath.Join(readbackDirectory, filepath.FromSlash(entry.Path))
		if !safeSFTPPath(localSource) || !safeSFTPPath(localReadback) {
			return nil, nil, fmt.Errorf("unsafe local import path")
		}
		upload.WriteString("put -f " + quoteSFTPPath(localSource) + " " + quoteSFTPPath(remotePath) + "\n")
		download.WriteString("get -f " + quoteSFTPPath(remotePath) + " " + quoteSFTPPath(localReadback) + "\n")
	}
	upload.WriteString("quit\n")
	download.WriteString("quit\n")
	if upload.Len() > maxImportBatchBytes || download.Len() > maxImportBatchBytes {
		return nil, nil, fmt.Errorf("SFTP batch exceeds bound")
	}
	return []byte(upload.String()), []byte(download.String()), nil
}

func safeSFTPPath(value string) bool {
	if len(value) == 0 || len(value) > 4096 || !filepath.IsAbs(value) || filepath.Clean(value) != value {
		return false
	}
	for _, char := range value {
		if !(char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '/' || char == '.' || char == '_' || char == '-' || char == ' ') {
			return false
		}
	}
	return true
}

func quoteSFTPPath(value string) string { return "\"" + value + "\"" }

func validImportUUID(value string) bool {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return false
	}
	for _, char := range value {
		if !(char >= '0' && char <= '9' || char >= 'a' && char <= 'f' || char == '-') {
			return false
		}
	}
	return true
}

func validImportDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, char := range value {
		if !(char >= '0' && char <= '9' || char >= 'a' && char <= 'f') {
			return false
		}
	}
	return true
}

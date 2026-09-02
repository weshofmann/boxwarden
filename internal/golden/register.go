package golden

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"syscall"

	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/config"
	"github.com/weshofmann/boxwarden/internal/domain"
	"github.com/weshofmann/boxwarden/internal/lock"
)

type currentPointer struct {
	Version  int       `json:"version"`
	Domain   domain.ID `json:"domain"`
	Revision string    `json:"revision"`
}

var (
	syncRoot        = syncRootDirectory
	beforeOpenChild = func(*os.Root, string) {}
)

func Register(ctx context.Context, configured config.Domain, tartName string, observer backend.Observer) (Record, error) {
	domainID, err := admittedDomain(configured)
	if err != nil {
		return Record{}, err
	}
	if !validTartName(tartName) {
		return Record{}, fmt.Errorf("invalid Tart object identity %q", tartName)
	}
	if observer == nil {
		return Record{}, fmt.Errorf("golden observer is required")
	}
	held, err := AcquireLock(ctx, configured)
	if err != nil {
		return Record{}, err
	}
	defer held.Release()
	observation, err := observer.Observe(ctx, tartName)
	if err != nil {
		return Record{}, fmt.Errorf("observe golden %q: %w", tartName, err)
	}
	if observation.ObjectID != tartName || !observation.Exists || observation.State != backend.ObjectStopped {
		return Record{}, fmt.Errorf("golden %q must be one existing stopped object", tartName)
	}
	record := Record{Version: recordVersion, Domain: domainID, Revision: tartName, Backend: BackendRef{Kind: "tart", ObjectID: tartName}}
	if err := persistRegistration(configured.StateRoot, record); err != nil {
		return Record{}, err
	}
	return record, nil
}

func LoadCurrent(ctx context.Context, configured config.Domain) (Record, error) {
	held, err := AcquireLock(ctx, configured)
	if err != nil {
		return Record{}, err
	}
	defer held.Release()
	return LoadCurrentLocked(configured)
}

// AcquireLock obtains the domain golden lock. Callers with both locks acquire
// their session lock first, then this lock, and persist the selected revision
// before releasing this lock for long backend work.
func AcquireLock(ctx context.Context, configured config.Domain) (*lock.Held, error) {
	if _, err := admittedDomain(configured); err != nil {
		return nil, err
	}
	return lock.AcquireGolden(ctx, configured.StateRoot)
}

// LoadCurrentLocked resolves current.json while the caller holds AcquireLock.
func LoadCurrentLocked(configured config.Domain) (Record, error) {
	domainID, err := admittedDomain(configured)
	if err != nil {
		return Record{}, err
	}
	root, err := openStateRoot(configured.StateRoot)
	if err != nil {
		return Record{}, fmt.Errorf("state root: %w", err)
	}
	defer root.Close()
	goldens, err := openPrivateChild(root, "goldens", false)
	if err != nil {
		return Record{}, fmt.Errorf("golden directory: %w", err)
	}
	defer goldens.Close()
	records, err := openPrivateChild(goldens, "records", false)
	if err != nil {
		return Record{}, fmt.Errorf("golden record directory: %w", err)
	}
	defer records.Close()
	pointer, err := loadPointer(goldens, "current.json", domainID)
	if err != nil {
		return Record{}, err
	}
	return loadRecord(records, pointer.Revision+".json", domainID, pointer.Revision)
}

// LoadRevisionLocked resolves one exact revision while the caller holds AcquireLock.
func LoadRevisionLocked(configured config.Domain, revision string) (Record, error) {
	domainID, err := admittedDomain(configured)
	if err != nil {
		return Record{}, err
	}
	if !validTartName(revision) {
		return Record{}, fmt.Errorf("invalid golden revision %q", revision)
	}
	root, err := openStateRoot(configured.StateRoot)
	if err != nil {
		return Record{}, fmt.Errorf("state root: %w", err)
	}
	defer root.Close()
	goldens, err := openPrivateChild(root, "goldens", false)
	if err != nil {
		return Record{}, fmt.Errorf("golden directory: %w", err)
	}
	defer goldens.Close()
	records, err := openPrivateChild(goldens, "records", false)
	if err != nil {
		return Record{}, fmt.Errorf("golden record directory: %w", err)
	}
	defer records.Close()
	return loadRecord(records, revision+".json", domainID, revision)
}

func admittedDomain(configured config.Domain) (domain.ID, error) {
	id, err := domain.Parse(string(configured.ID))
	if err != nil || id != configured.ID {
		return "", fmt.Errorf("invalid configured domain")
	}
	return id, nil
}

func persistRegistration(stateRoot string, record Record) error {
	root, err := openStateRoot(stateRoot)
	if err != nil {
		return fmt.Errorf("state root: %w", err)
	}
	defer root.Close()
	goldens, err := openPrivateChild(root, "goldens", true)
	if err != nil {
		return fmt.Errorf("golden directory: %w", err)
	}
	defer goldens.Close()
	records, err := openPrivateChild(goldens, "records", true)
	if err != nil {
		return fmt.Errorf("golden record directory: %w", err)
	}
	defer records.Close()
	encoded, err := json.Marshal(record)
	if err != nil {
		return err
	}
	name := record.Revision + ".json"
	if _, err := records.Lstat(name); err == nil {
		existing, err := loadRecord(records, name, record.Domain, record.Revision)
		if err != nil {
			return err
		}
		if existing != record {
			return fmt.Errorf("immutable golden revision %q conflicts with existing record", record.Revision)
		}
	} else if errors.Is(err, os.ErrNotExist) {
		if err := writeImmutable(records, name, encoded); err != nil {
			return err
		}
	} else {
		return err
	}
	pointer, err := json.Marshal(currentPointer{Version: recordVersion, Domain: record.Domain, Revision: record.Revision})
	if err != nil {
		return err
	}
	return replaceAtomically(goldens, "current.json", pointer)
}

func loadRecord(root *os.Root, name string, expectedDomain domain.ID, revision string) (Record, error) {
	file, err := openPrivateRegular(root, name)
	if err != nil {
		return Record{}, fmt.Errorf("open golden record: %w", err)
	}
	defer file.Close()
	var record Record
	if err := decodeStrict(file, &record); err != nil {
		return Record{}, fmt.Errorf("decode golden record: %w", err)
	}
	if err := validRecord(record); err != nil {
		return Record{}, err
	}
	if record.Domain != expectedDomain {
		return Record{}, fmt.Errorf("golden record domain %q does not match requested domain %q", record.Domain, expectedDomain)
	}
	if record.Revision != revision {
		return Record{}, fmt.Errorf("golden record revision mismatch")
	}
	return record, nil
}

func loadPointer(root *os.Root, name string, expectedDomain domain.ID) (currentPointer, error) {
	file, err := openPrivateRegular(root, name)
	if err != nil {
		return currentPointer{}, fmt.Errorf("open golden pointer: %w", err)
	}
	defer file.Close()
	var pointer currentPointer
	if err := decodeStrict(file, &pointer); err != nil {
		return currentPointer{}, fmt.Errorf("decode golden pointer: %w", err)
	}
	if pointer.Version != recordVersion || pointer.Domain != expectedDomain || !validTartName(pointer.Revision) {
		return currentPointer{}, fmt.Errorf("invalid golden pointer")
	}
	return pointer, nil
}

func decodeStrict(file *os.File, value any) error {
	contents, err := io.ReadAll(io.LimitReader(file, (1<<20)+1))
	if err != nil {
		return err
	}
	if len(contents) > 1<<20 {
		return fmt.Errorf("golden JSON exceeds 1 MiB")
	}
	validator := json.NewDecoder(bytes.NewReader(contents))
	if err := validateJSONValue(validator); err != nil {
		return err
	}
	if token, err := validator.Token(); err != io.EOF {
		if err != nil {
			return err
		}
		return fmt.Errorf("unexpected trailing token %v", token)
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	if token, err := decoder.Token(); err != io.EOF {
		if err != nil {
			return err
		}
		return fmt.Errorf("unexpected trailing token %v", token)
	}
	return nil
}

func validateJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		seen := map[string]bool{}
		for decoder.More() {
			field, err := decoder.Token()
			if err != nil {
				return err
			}
			name, ok := field.(string)
			if !ok {
				return fmt.Errorf("expected object field")
			}
			if seen[name] {
				return fmt.Errorf("duplicate field %q", name)
			}
			seen[name] = true
			if err := validateJSONValue(decoder); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil {
			return err
		}
		if end != json.Delim('}') {
			return fmt.Errorf("expected object end")
		}
	case '[':
		for decoder.More() {
			if err := validateJSONValue(decoder); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil {
			return err
		}
		if end != json.Delim(']') {
			return fmt.Errorf("expected array end")
		}
	default:
		return fmt.Errorf("unexpected delimiter %q", delimiter)
	}
	return nil
}

func writeImmutable(root *os.Root, name string, contents []byte) error {
	if _, err := root.Lstat(name); err == nil {
		return fmt.Errorf("immutable target already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return writeThenRename(root, name, contents)
}
func replaceAtomically(root *os.Root, name string, contents []byte) error {
	if info, err := root.Lstat(name); err == nil {
		if err := requirePrivateRegular(info); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return writeThenRename(root, name, contents)
}
func writeThenRename(root *os.Root, name string, contents []byte) (err error) {
	temp, err := temporaryName(name)
	if err != nil {
		return err
	}
	file, err := root.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = root.Remove(temp)
		}
	}()
	if _, err = file.Write(contents); err != nil {
		file.Close()
		return err
	}
	if err = file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	if err = root.Rename(temp, name); err != nil {
		return err
	}
	return syncRoot(root)
}
func temporaryName(name string) (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return "." + name + ".tmp-" + hex.EncodeToString(bytes), nil
}

func openStateRoot(path string) (*os.Root, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if err := requirePrivateDirectory(info); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, err
	}
	opened, err := root.Stat(".")
	if err != nil {
		root.Close()
		return nil, err
	}
	if !os.SameFile(info, opened) {
		root.Close()
		return nil, fmt.Errorf("changed while opening")
	}
	return root, nil
}
func openPrivateChild(parent *os.Root, name string, create bool) (*os.Root, error) {
	info, err := parent.Lstat(name)
	if errors.Is(err, os.ErrNotExist) && create {
		if err := parent.Mkdir(name, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		if err := syncRoot(parent); err != nil {
			return nil, fmt.Errorf("sync parent after create: %w", err)
		}
		info, err = parent.Lstat(name)
	}
	if err != nil {
		return nil, err
	}
	if err := requirePrivateDirectory(info); err != nil {
		return nil, err
	}
	beforeOpenChild(parent, name)
	child, err := parent.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	opened, err := child.Stat(".")
	if err != nil {
		child.Close()
		return nil, err
	}
	if !os.SameFile(info, opened) {
		child.Close()
		return nil, fmt.Errorf("changed while opening")
	}
	return child, nil
}
func syncRootDirectory(root *os.Root) error {
	file, err := root.Open(".")
	if err != nil {
		return err
	}
	defer file.Close()
	return file.Sync()
}
func requirePrivateDirectory(info os.FileInfo) error {
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("must be a non-symlink directory")
	}
	if info.Mode().Perm() != 0o700 {
		return fmt.Errorf("must have mode 0700")
	}
	return nil
}
func requirePrivateRegular(info os.FileInfo) error {
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("must be a regular non-symlink file")
	}
	if info.Mode().Perm() != 0o600 {
		return fmt.Errorf("must have mode 0600")
	}
	return nil
}
func openPrivateRegular(root *os.Root, name string) (*os.File, error) {
	info, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if err := requirePrivateRegular(info); err != nil {
		return nil, err
	}
	file, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	opened, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, err
	}
	if !os.SameFile(info, opened) {
		file.Close()
		return nil, fmt.Errorf("changed while opening")
	}
	return file, nil
}

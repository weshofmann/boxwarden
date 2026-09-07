package supervisor

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// Production break: a rename that replaces an entry after the publication
// check can replace a competing generation namespace. The primitive must
// report EEXIST and leave that namespace untouched.
func TestRenameWithoutReplacePreservesConcurrentGenerationNamespace(t *testing.T) {
	parent := t.TempDir()
	if err := os.Chmod(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	stage := filepath.Join(parent, "stage")
	final := filepath.Join(parent, "generation")
	if err := os.Mkdir(stage, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(final, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := renameWithoutReplace(stage, final); !errors.Is(err, os.ErrExist) {
		t.Fatalf("renameWithoutReplace() error = %v, want existing generation rejection", err)
	}
	if _, err := os.Lstat(stage); err != nil {
		t.Fatalf("staged generation was consumed on collision: %v", err)
	}
	entries, err := os.ReadDir(final)
	if err != nil || len(entries) != 0 {
		t.Fatalf("competing generation after collision = %#v error %v, want unchanged empty directory", entries, err)
	}
}

// Production break: pre-creating an empty generation directory must not let a
// caller turn an unbound namespace into supervisor authority.
func TestPublishOrAdmitRequestAtomicallyPublishesBoundGeneration(t *testing.T) {
	base := t.TempDir()
	if err := os.Chmod(base, 0o700); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(base, "runtime")
	request := publicationRequest(root)
	path, first, err := publishOrAdmitRequest(request)
	if err != nil {
		t.Fatalf("publishOrAdmitRequest() error = %v", err)
	}
	if !first || path != filepath.Join(request.RuntimeDirectory, requestName) {
		t.Fatalf("publication = path %q first %t, want bound request path and first publication", path, first)
	}
	loaded, err := readLaunchRequest(path)
	if err != nil || !reflect.DeepEqual(loaded, request) {
		t.Fatalf("published immutable request = %#v error %v, want %#v", loaded, err, request)
	}
	entries, err := os.ReadDir(request.RuntimeDirectory)
	if err != nil || len(entries) != 2 || entries[0].Name() != lockName || entries[1].Name() != requestName {
		t.Fatalf("published generation entries = %#v error %v, want exactly immutable request and bound lock", entries, err)
	}
	lock, err := admitBoundGenerationLock(filepath.Join(request.RuntimeDirectory, lockName), request)
	if err != nil {
		t.Fatalf("admitBoundGenerationLock() error = %v", err)
	}
	defer lock.close()
	data, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	wantDigest := sha256.Sum256(data)
	lockData, err := lock.read()
	if err != nil {
		t.Fatal(err)
	}
	var record generationLockRecord
	if err := decodeExact(lockData, &record); err != nil {
		t.Fatal(err)
	}
	if record.Version != 1 || record.Binding != request.Binding || record.RequestSHA256 != hex.EncodeToString(wantDigest[:]) {
		t.Fatalf("bound generation lock = %#v, want version 1, exact binding, and canonical request digest", record)
	}

	if _, _, err := publishOrAdmitRequest(request); err != nil {
		t.Fatalf("exact request-only retry error = %v", err)
	}
	foreign := request
	foreign.Binding.BackendObject = "boxwarden-work-other"
	if _, _, err := publishOrAdmitRequest(foreign); err == nil {
		t.Fatal("foreign request-only generation admitted")
	}
}

// Production break: an exact legacy request-only retry must complete the
// namespace with an O_EXCL-bound lock, rather than spawning against an
// unbound request or classifying it as opaque drift.
func TestPublishOrAdmitRequestCompletesExactRequestOnlyNamespaceWithBoundLock(t *testing.T) {
	base := t.TempDir()
	if err := os.Chmod(base, 0o700); err != nil {
		t.Fatal(err)
	}
	request := publicationRequest(filepath.Join(base, "runtime"))
	if err := os.MkdirAll(request.RuntimeDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := writeLaunchRequest(filepath.Join(request.RuntimeDirectory, requestName), request); err != nil {
		t.Fatal(err)
	}
	if _, first, err := publishOrAdmitRequest(request); err != nil {
		t.Fatalf("request-only retry error = %v", err)
	} else if first {
		t.Fatal("request-only retry was reported as first publication")
	}
	if lock, err := admitBoundGenerationLock(filepath.Join(request.RuntimeDirectory, lockName), request); err != nil {
		t.Fatalf("request-only retry did not publish exact bound lock: %v", err)
	} else {
		_ = lock.close()
	}
}

// Production break: request-only completion is recovery of already-visible
// state. It must admit the exact request before creating a lock; a foreign
// request-only directory is drift and receives no new artifact.
func TestPublishOrAdmitRequestDoesNotMutateForeignRequestOnlyNamespace(t *testing.T) {
	base := t.TempDir()
	if err := os.Chmod(base, 0o700); err != nil {
		t.Fatal(err)
	}
	want := publicationRequest(filepath.Join(base, "runtime"))
	if err := os.MkdirAll(want.RuntimeDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	foreign := want
	foreign.Binding.BackendObject = "boxwarden-work-foreign"
	requestPath := filepath.Join(want.RuntimeDirectory, requestName)
	if err := writeLaunchRequest(requestPath, foreign); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(requestPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := publishOrAdmitRequest(want); err == nil {
		t.Fatal("foreign request-only namespace was admitted")
	}
	after, err := os.ReadFile(requestPath)
	if err != nil || !reflect.DeepEqual(after, before) {
		t.Fatalf("foreign request was changed: bytes=%q error=%v", after, err)
	}
	if _, err := os.Lstat(filepath.Join(want.RuntimeDirectory, lockName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("foreign request-only namespace gained a lock: %v", err)
	}
}

// Production break: a lock helper is not a generic arbitrary-path writer.
// It may write only the fixed lock name in the request's exact runtime root.
func TestWriteBoundGenerationLockRejectsWrongParent(t *testing.T) {
	base := t.TempDir()
	if err := os.Chmod(base, 0o700); err != nil {
		t.Fatal(err)
	}
	request := publicationRequest(filepath.Join(base, "runtime"))
	wrong := filepath.Join(base, lockName)
	if err := writeBoundGenerationLock(wrong, request); err == nil {
		t.Fatal("writeBoundGenerationLock accepted a lock path outside the exact runtime directory")
	}
}

func TestAdmitBoundGenerationLockRejectsUnsafeOrNonCanonicalRecords(t *testing.T) {
	for _, scenario := range []struct {
		name  string
		write func(t *testing.T, path string, request LaunchRequest)
	}{
		{
			name: "foreign binding",
			write: func(t *testing.T, path string, request LaunchRequest) {
				t.Helper()
				foreign := request
				foreign.Binding.Generation = "other-generation"
				if err := writeBoundGenerationLock(path, foreign); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "duplicate field",
			write: func(t *testing.T, path string, request LaunchRequest) {
				t.Helper()
				record, err := boundLockRecord(request)
				if err != nil {
					t.Fatal(err)
				}
				binding, err := json.Marshal(record.Binding)
				if err != nil {
					t.Fatal(err)
				}
				data := []byte(`{"version":1,"version":1,"binding":` + string(binding) + `,"request_sha256":"` + record.RequestSHA256 + `"}`)
				if err := writePrivateFile(path, data); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "unknown field",
			write: func(t *testing.T, path string, request LaunchRequest) {
				t.Helper()
				record, err := boundLockRecord(request)
				if err != nil {
					t.Fatal(err)
				}
				data, err := json.Marshal(struct {
					generationLockRecord
					Extra string `json:"extra"`
				}{generationLockRecord: record, Extra: "unexpected"})
				if err != nil {
					t.Fatal(err)
				}
				if err := writePrivateFile(path, data); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "trailing value",
			write: func(t *testing.T, path string, request LaunchRequest) {
				t.Helper()
				record, err := boundLockRecord(request)
				if err != nil {
					t.Fatal(err)
				}
				data, err := json.Marshal(record)
				if err != nil {
					t.Fatal(err)
				}
				if err := writePrivateFile(path, append(data, []byte(`{}`)...)); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "symlink",
			write: func(t *testing.T, path string, _ LaunchRequest) {
				t.Helper()
				target := path + ".target"
				if err := writePrivateFile(target, []byte("target")); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Base(target), path); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "wrong mode",
			write: func(t *testing.T, path string, request LaunchRequest) {
				t.Helper()
				if err := writeBoundGenerationLock(path, request); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(path, 0o644); err != nil {
					t.Fatal(err)
				}
			},
		},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			base := t.TempDir()
			if err := os.Chmod(base, 0o700); err != nil {
				t.Fatal(err)
			}
			request := publicationRequest(filepath.Join(base, "runtime"))
			if err := os.MkdirAll(request.RuntimeDirectory, 0o700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(request.RuntimeDirectory, lockName)
			scenario.write(t, path, request)
			if retained, err := admitBoundGenerationLock(path, request); err == nil {
				_ = retained.close()
				t.Fatal("unsafe or noncanonical generation lock was admitted")
			} else if retained != nil {
				t.Fatal("rejected generation lock retained a descriptor")
			}
		})
	}
}

func TestAdmitBoundGenerationLockRejectsNonPrivateRuntimeParent(t *testing.T) {
	base := t.TempDir()
	if err := os.Chmod(base, 0o700); err != nil {
		t.Fatal(err)
	}
	request := publicationRequest(filepath.Join(base, "runtime"))
	if err := os.MkdirAll(request.RuntimeDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(request.RuntimeDirectory, lockName)
	if err := writeBoundGenerationLock(path, request); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(request.RuntimeDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	if retained, err := admitBoundGenerationLock(path, request); err == nil {
		_ = retained.close()
		t.Fatal("generation lock under a non-private runtime parent was admitted")
	}
}

// O_EXCL collision handling admits only the same exact lock record; it never
// treats a visible lock as sufficient merely because it happened to win.
func TestPublishBoundGenerationLockAdmitsOnlyExactCollision(t *testing.T) {
	base := t.TempDir()
	if err := os.Chmod(base, 0o700); err != nil {
		t.Fatal(err)
	}
	request := publicationRequest(filepath.Join(base, "runtime"))
	if err := os.MkdirAll(request.RuntimeDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(request.RuntimeDirectory, lockName)
	if err := writeBoundGenerationLock(path, request); err != nil {
		t.Fatal(err)
	}
	if err := publishBoundGenerationLock(path, request); err != nil {
		t.Fatalf("exact O_EXCL collision was not admitted: %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := writePrivateFile(path, []byte("foreign")); err != nil {
		t.Fatal(err)
	}
	if err := publishBoundGenerationLock(path, request); err == nil {
		t.Fatal("foreign O_EXCL collision was admitted")
	}
}

func TestPublishOrAdmitRequestRejectsPrecreatedEmptyGeneration(t *testing.T) {
	base := t.TempDir()
	if err := os.Chmod(base, 0o700); err != nil {
		t.Fatal(err)
	}
	request := publicationRequest(filepath.Join(base, "runtime"))
	if err := os.MkdirAll(request.RuntimeDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, _, err := publishOrAdmitRequest(request); err == nil {
		t.Fatal("pre-created empty generation was admitted")
	}
}

// Production break: a CA admitted for another security domain must never be
// attached to this generation's immutable request.
func TestPublishOrAdmitRequestRejectsCADomainDifferentFromBinding(t *testing.T) {
	base := t.TempDir()
	if err := os.Chmod(base, 0o700); err != nil {
		t.Fatal(err)
	}
	request := publicationRequest(filepath.Join(base, "runtime"))
	request.CA.Domain = "personal"
	if _, _, err := publishOrAdmitRequest(request); err == nil {
		t.Fatal("publishOrAdmitRequest() accepted CA identity for another domain")
	}
}

func publicationRequest(root string) LaunchRequest {
	request := LaunchRequest{Binding: Binding{Domain: "work", SessionID: "00112233-4455-4677-8899-aabbccddeeff", BackendKind: "tart", BackendObject: "boxwarden-work-00112233445546778899aabbccddeeff", Generation: "11111111-2222-4333-8444-555555555555"}, RuntimeDirectory: filepath.Join(root, "work", "00112233-4455-4677-8899-aabbccddeeff", "11111111-2222-4333-8444-555555555555"), HostConfigPath: "/private/config", SessionRecordName: "dev", Host: testHostExpectation(), CA: testCAExpectation()}
	request.CA.Domain = request.Binding.Domain
	return request
}

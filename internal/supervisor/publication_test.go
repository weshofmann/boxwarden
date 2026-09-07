package supervisor

import (
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
	if err != nil || len(entries) != 1 || entries[0].Name() != requestName {
		t.Fatalf("published generation entries = %#v error %v, want only immutable request", entries, err)
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

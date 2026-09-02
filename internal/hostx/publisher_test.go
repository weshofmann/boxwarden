package hostx

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRootedPublisherPublishesManifestLastAndValidatesExactTree(t *testing.T) {
	root, source, digest := publisherFixture(t)
	var steps []string
	p := testPublisher(root, digest)
	p.observe = func(step string) { steps = append(steps, step) }
	request := publisherRequest(source)
	caller := Caller{UID: os.Getuid(), Name: "operator", Home: filepath.Join(root, "home")}
	group := Group{ID: os.Getgid(), Name: OperatorGroupName, Members: []int{caller.UID}}

	if state, err := p.State(context.Background(), request, caller, group); err != nil || state != publicationAbsent {
		t.Fatalf("State(absent) = %v, %v", state, err)
	}
	if err := p.Publish(context.Background(), request, caller, group); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if got, want := fmt.Sprint(steps), "[tree-renamed tree-synced manifest-renamed manifest-synced]"; got != want {
		t.Fatalf("publication steps = %s, want %s", got, want)
	}
	if state, err := p.State(context.Background(), request, caller, group); err != nil || state != publicationComplete {
		t.Fatalf("State(complete) = %v, %v", state, err)
	}
	entries, err := os.ReadDir(p.finalDir())
	if err != nil || len(entries) != 2 {
		t.Fatalf("final entries = %v, %v; want softnet and manifest only", entries, err)
	}
	for path, mode := range map[string]uint32{
		filepath.Join(p.finalDir(), "softnet"):       0o550,
		filepath.Join(p.finalDir(), "manifest.json"): 0o400,
	} {
		info, err := os.Lstat(path)
		if err != nil || unixMode(info) != mode {
			t.Fatalf("%s mode = %04o, %v; want %04o", path, unixMode(info), err, mode)
		}
	}

	if err := os.WriteFile(filepath.Join(p.finalDir(), "unexpected"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if state, _ := p.State(context.Background(), request, caller, group); state != publicationUnexpected {
		t.Fatalf("State(extra entry) = %v, want unexpected", state)
	}
}

func TestRootedPublisherManifestFailureLeavesFailClosedPartialFinalTree(t *testing.T) {
	root, source, digest := publisherFixture(t)
	p := testPublisher(root, digest)
	p.fail = func(step string) error {
		if step == "before-manifest-rename" {
			return fmt.Errorf("injected manifest failure")
		}
		return nil
	}
	request := publisherRequest(source)
	caller := Caller{UID: os.Getuid(), Name: "operator", Home: filepath.Join(root, "home")}
	group := Group{ID: os.Getgid(), Name: OperatorGroupName, Members: []int{caller.UID}}
	if err := p.Publish(context.Background(), request, caller, group); err == nil {
		t.Fatal("Publish() error = nil, want interruption")
	}
	if _, err := os.Lstat(filepath.Join(p.finalDir(), "softnet")); err != nil {
		t.Fatalf("published softnet missing after manifest interruption: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(p.finalDir(), "manifest.json")); !os.IsNotExist(err) {
		t.Fatalf("manifest exists after interrupted publication: %v", err)
	}
	if state, _ := p.State(context.Background(), request, caller, group); state != publicationUnexpected {
		t.Fatalf("State(partial) = %v, want unexpected", state)
	}
}

func TestRootedPublisherDoesNotAdoptCompleteLookingV1Manifest(t *testing.T) {
	root, source, digest := publisherFixture(t)
	p := testPublisher(root, digest)
	request := publisherRequest(source)
	caller := Caller{UID: os.Getuid(), Name: "operator", Home: filepath.Join(root, "home")}
	group := Group{ID: os.Getgid(), Name: OperatorGroupName, Members: []int{caller.UID}}
	if err := p.Publish(context.Background(), request, caller, group); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	manifestPath := filepath.Join(p.finalDir(), "manifest.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(manifestPath, 0o600); err != nil {
		t.Fatal(err)
	}
	legacy := strings.Replace(string(data), `"version":2`, `"version":1`, 1)
	legacy = strings.Replace(legacy, `"macos_build":"25G83",`, ``, 1)
	v1Data := []byte(legacy)
	if err := os.WriteFile(manifestPath, v1Data, 0o400); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(manifestPath, 0o400); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(manifestPath)
	if err != nil || unixMode(info) != 0o400 {
		t.Fatalf("manifest mode = %04o, %v; want 0400", unixMode(info), err)
	}
	if got, err := os.ReadFile(manifestPath); err != nil || string(got) != string(v1Data) {
		t.Fatalf("manifest bytes = %q, %v; want unchanged v1 bytes", got, err)
	}
	if state, err := p.Preflight(context.Background(), request, caller); err != nil || state != publicationUnexpected {
		t.Fatalf("Preflight(v1 manifest) = %v, %v; want unexpected", state, err)
	}
	if state, err := p.State(context.Background(), request, caller, group); err != nil || state != publicationUnexpected {
		t.Fatalf("State(v1 manifest) = %v, %v; want unexpected", state, err)
	}
}

func TestRootedPublisherCopiesFromValidatedFDWhenSourcePathIsReplaced(t *testing.T) {
	root, source, digest := publisherFixture(t)
	p := testPublisher(root, digest)
	p.fail = func(step string) error {
		if step == "after-source-open" {
			if err := os.Rename(source, source+".validated"); err != nil {
				return err
			}
			return os.WriteFile(source, []byte("replacement bytes"), 0o755)
		}
		return nil
	}
	request := publisherRequest(source)
	caller := Caller{UID: os.Getuid(), Name: "operator", Home: filepath.Join(root, "home")}
	group := Group{ID: os.Getgid(), Name: OperatorGroupName, Members: []int{caller.UID}}
	if err := p.Publish(context.Background(), request, caller, group); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	got, err := SHA256File(filepath.Join(p.finalDir(), "softnet"))
	if err != nil || got != digest {
		t.Fatalf("published digest = %q, %v; want validated FD digest %q", got, err, digest)
	}
}

func TestRootedPublisherRefusesFinalTreeRaceAndCleansOnlyOwnedStage(t *testing.T) {
	root, source, digest := publisherFixture(t)
	p := testPublisher(root, digest)
	p.fail = func(step string) error {
		if step == "before-tree-rename" {
			return os.Mkdir(p.finalDir(), 0o700)
		}
		return nil
	}
	request := publisherRequest(source)
	caller := Caller{UID: os.Getuid(), Name: "operator", Home: filepath.Join(root, "home")}
	group := Group{ID: os.Getgid(), Name: OperatorGroupName, Members: []int{caller.UID}}
	if err := p.Publish(context.Background(), request, caller, group); err == nil {
		t.Fatal("Publish() error = nil, want no-replace race refusal")
	}
	entries, err := os.ReadDir(filepath.Dir(p.finalDir()))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".staging-") {
			t.Fatalf("owned staging entry was not cleaned: %q", entry.Name())
		}
	}
	if info, err := os.Lstat(p.finalDir()); err != nil || !info.IsDir() {
		t.Fatalf("racing final tree was overwritten or removed: %v, %v", info, err)
	}
}

func TestRootedPublisherCompleteStateRejectsIntermediateAncestorDrift(t *testing.T) {
	for name, mutate := range map[string]func(*RootedPublisher, string){
		"mode": func(_ *RootedPublisher, ancestor string) {
			if err := os.Chmod(ancestor, 0o775); err != nil {
				t.Fatal(err)
			}
		},
		"ownership": func(p *RootedPublisher, _ string) { p.rootGID++ },
		"ACL": func(p *RootedPublisher, ancestor string) {
			p.acl = pathACLInspector{ancestor: true}
		},
	} {
		t.Run(name, func(t *testing.T) {
			root, source, digest := publisherFixture(t)
			p := testPublisher(root, digest)
			request := publisherRequest(source)
			caller := Caller{UID: os.Getuid(), Name: "operator", Home: filepath.Join(root, "home")}
			group := Group{ID: os.Getgid(), Name: OperatorGroupName, Members: []int{caller.UID}}
			if err := p.Publish(context.Background(), request, caller, group); err != nil {
				t.Fatal(err)
			}
			ancestor := filepath.Join(root, "toolchains", "softnet")
			mutate(&p, ancestor)
			if state, _ := p.State(context.Background(), request, caller, group); state != publicationUnexpected {
				t.Fatalf("State(%s drift) = %v, want unexpected", name, state)
			}
		})
	}
}

func TestRootedPublisherRejectsCurrentPointerAndPairedTartDrift(t *testing.T) {
	root, source, digest := publisherFixture(t)
	p := testPublisher(root, digest)
	request := publisherRequest(source)
	caller := Caller{UID: os.Getuid(), Name: "operator", Home: filepath.Join(root, "home")}
	group := Group{ID: os.Getgid(), Name: OperatorGroupName, Members: []int{caller.UID}}
	current := filepath.Join(root, "toolchains", "softnet", "current")
	if err := os.MkdirAll(filepath.Dir(current), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("0.19.0", current); err != nil {
		t.Fatal(err)
	}
	if err := p.Publish(context.Background(), request, caller, group); err == nil {
		t.Fatal("Publish(current pointer) error = nil")
	}
	if err := os.Remove(current); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(request.Tart.Path, []byte("drifted tart"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := p.Publish(context.Background(), request, caller, group); err == nil {
		t.Fatal("Publish(drifted paired Tart) error = nil")
	}
}

func publisherFixture(t *testing.T) (string, string, string) {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(base, "Library", "Boxwarden")
	if err := os.Mkdir(filepath.Dir(root), 0o755); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(base, "source-softnet")
	contents := []byte("deterministic synthetic softnet fixture")
	if err := os.WriteFile(source, contents, 0o755); err != nil {
		t.Fatal(err)
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(contents))
	if err := os.WriteFile(source+"-tart", contents, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(base, "tart-home"), 0o700); err != nil {
		t.Fatal(err)
	}
	return root, source, digest
}

func testPublisher(root, digest string) RootedPublisher {
	return RootedPublisher{
		Root:          root,
		Now:           func() time.Time { return time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC) },
		rootUID:       os.Getuid(),
		rootGID:       os.Getgid(),
		softnetDigest: digest,
		tartDigest:    digest,
		softnetMode:   0o550,
		chown:         func(string, int, int) error { return nil },
		acl:           noACLInspector{},
		token:         func() string { return "fixed-owned-token" },
	}
}

func publisherRequest(source string) InstallRequest {
	tart := qualifiedTartForTest()
	tart.Path = source + "-tart"
	return InstallRequest{Version: 1, SoftnetSource: source, Tart: tart, TartHome: filepath.Join(filepath.Dir(source), "tart-home")}
}

type noACLInspector struct{}

func (noACLInspector) HasExtendedACL(string) (bool, error) { return false, nil }

type pathACLInspector map[string]bool

func (i pathACLInspector) HasExtendedACL(path string) (bool, error) { return i[path], nil }

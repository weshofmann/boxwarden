package contract

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestClosedBuildToolAndSDKMetadataBindings(t *testing.T) {
	fixture := func() BuildInputs {
		b := BuildInputs{Version: 1, NativeInputSHA: NativeInputSHA, SDKPath: SDKPath, SDKCanonicalName: SDKCanonicalName, PythonAlias: PythonAlias}
		for i := range b.Inputs {
			b.Inputs[i] = ToolInputPolicy(i)
			b.Inputs[i].SHA = strings.Repeat("a", 64)
		}
		return b
	}
	b := fixture()
	raw, _ := json.Marshal(b)
	if _, e := ParseBuildInputs(raw, SHA(raw)); e != nil {
		t.Fatal(e)
	}
	for _, change := range []func(*BuildInputs){func(b *BuildInputs) { b.Inputs[10] = ToolInput{} }, func(b *BuildInputs) { b.Inputs[0].Path = "/other/go" }, func(b *BuildInputs) { b.Inputs[7].Kind = "executable" }, func(b *BuildInputs) { b.Inputs[7].SHA = "" }, func(b *BuildInputs) { b.Inputs[8] = b.Inputs[7] }, func(b *BuildInputs) { b.SDKCanonicalName = "macosx28.0" }, func(b *BuildInputs) { b.NativeInputSHA = "unknown" }, func(b *BuildInputs) { b.PythonAlias = "/usr/bin/python3" }, func(b *BuildInputs) { b.Inputs[6].Kind = "executable" }, func(b *BuildInputs) { b.Inputs[10].Path = PythonAlias }} {
		b := fixture()
		change(&b)
		raw, _ := json.Marshal(b)
		if _, e := ParseBuildInputs(raw, SHA(raw)); e == nil {
			t.Fatal("missing/foreign/mixed SDK or executable binding")
		}
	}
	if _, e := ParseBuildInputs(append(raw, '\n'), SHA(append(raw, '\n'))); e == nil {
		t.Fatal("noncanonical build record")
	}
}

func TestLegacyBuildInputWithoutInterpreterCannotAuthorize(t *testing.T) {
	b := BuildInputs{Version: 1, NativeInputSHA: NativeInputSHA, SDKPath: SDKPath, SDKCanonicalName: SDKCanonicalName}
	for i := 0; i < 10; i++ {
		b.Inputs[i] = ToolInputPolicy(i)
		b.Inputs[i].SHA = strings.Repeat("a", 64)
	}
	raw, _ := json.Marshal(b)
	if _, e := ParseBuildInputs(raw, SHA(raw)); e == nil {
		t.Fatal("launcher without canonical interpreter admitted")
	}
}

func TestActualRetainedElevenBuildInputs(t *testing.T) {
	raw, e := os.ReadFile("testdata/build-inputs.json")
	if e != nil {
		t.Fatal(e)
	}
	if SHA(raw) != "51f6943374600900c8158f853929bfb09bac52ccc433f9676869bdc5a9448b9c" {
		t.Fatal("actual build-input proof changed")
	}
	if _, e := ParseBuildInputs(raw, SHA(raw)); e != nil {
		t.Fatal(e)
	}
}

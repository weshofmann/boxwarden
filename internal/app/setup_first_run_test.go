package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestFirstRunPlanProvisionalJSONWithoutMutation(t *testing.T) {
	var out bytes.Buffer
	var actual FirstRunInput
	o := Options{Output: &out, SetupFirstRunPlan: func(_ context.Context, input FirstRunInput) (FirstRunPlan, error) {
		actual = input
		return FirstRunPlan{Version: 1, Scope: "alpha_first_run", Status: "data_location_required", Alternatives: []string{"/Volumes/private"}}, nil
	}}
	handled, err := runSetupFirstRun(context.Background(), []string{"setup", "plan", "--json", "--setup-id", "00112233-4455-6677-8899-aabbccddeeff", "--package", "/package"}, o)
	if !handled || err != nil {
		t.Fatalf("handled = %v, %v", handled, err)
	}
	if actual.DataLocation != "" || actual.ExpectedDigest != "" {
		t.Fatalf("input = %#v", actual)
	}
	var result FirstRunPlan
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Status != "data_location_required" || len(result.Alternatives) != 1 {
		t.Fatalf("result = %#v", result)
	}
}

func TestFirstRunCreateRequiresCompleteReviewedInputs(t *testing.T) {
	for _, args := range [][]string{{"setup", "create", "--json"}, {"--config", "/other", "setup", "create", "--json"}, {"--domain", "alpha", "setup", "create", "--json"}} {
		var out bytes.Buffer
		o := Options{Output: &out, SetupFirstRunCreate: func(context.Context, FirstRunInput, io.Writer) (SetupInspection, bool, error) {
			t.Fatal("called mutation for incomplete invocation")
			return SetupInspection{}, false, nil
		}}
		handled, err := runSetupFirstRun(context.Background(), args, o)
		if !handled || err == nil {
			t.Fatalf("args=%v handled=%v err=%v", args, handled, err)
		}
	}
}

func TestFirstRunCreateReportsRetainedUncertainty(t *testing.T) {
	var out bytes.Buffer
	o := Options{Output: &out, SetupFirstRunCreate: func(context.Context, FirstRunInput, io.Writer) (SetupInspection, bool, error) {
		return SetupInspection{}, true, errors.New("publication durability unknown")
	}}
	args := []string{"setup", "create", "--json", "--setup-id", "00112233-4455-6677-8899-aabbccddeeff", "--package", "/package", "--data-location", "/Volumes/private", "--iso", "/installer.iso", "--expected-digest", strings.Repeat("a", 64)}
	handled, err := runSetupFirstRun(context.Background(), args, o)
	if !handled || err == nil || !strings.Contains(out.String(), `"uncertain":true`) {
		t.Fatalf("handled=%v err=%v output=%s", handled, err, out.String())
	}
}

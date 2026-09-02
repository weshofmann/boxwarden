package serialx

import (
	"context"
	"testing"
)

func TestStartScreenRequiresQualifiedBinaryAndExactDirectChildSpec(t *testing.T) {
	starter := &fakeScreenStarter{}
	child, err := StartScreen(context.Background(), starter, ScreenBinary{
		Path:    "/usr/bin/screen",
		Mode:    0o755,
		UID:     0,
		GID:     0,
		Links:   1,
		SHA256:  ScreenSHA256,
		Version: ScreenVersion,
	}, "/dev/ttys123", "boxwarden-work-dev")
	if err != nil {
		t.Fatalf("StartScreen() error = %v", err)
	}
	if child != starter.child {
		t.Fatal("StartScreen() did not return direct child handle")
	}
	if got, want := starter.spec, (ScreenSpec{
		Path:  "/usr/bin/screen",
		Args:  []string{"-D", "-m", "-S", "boxwarden-work-dev"},
		Stdin: "/dev/ttys123",
	}); !equalScreenSpec(got, want) {
		t.Fatalf("Screen spec = %#v, want %#v", got, want)
	}
}

func TestStartScreenRejectsUnqualifiedBinaryBeforeStarting(t *testing.T) {
	starter := &fakeScreenStarter{}
	_, err := StartScreen(context.Background(), starter, ScreenBinary{
		Path: "/usr/bin/screen", Mode: 0o755, UID: 0, GID: 0, Links: 2, SHA256: ScreenSHA256, Version: ScreenVersion,
	}, "/dev/ttys123", "boxwarden-work-dev")
	if err == nil {
		t.Fatal("StartScreen() error = nil, want rejection")
	}
	if starter.called {
		t.Fatal("StartScreen() started an unqualified binary")
	}
}

type fakeScreenStarter struct {
	called bool
	spec   ScreenSpec
	child  ScreenChild
}

func (f *fakeScreenStarter) StartScreen(_ context.Context, spec ScreenSpec) (ScreenChild, error) {
	f.called = true
	f.spec = spec
	if f.child == nil {
		f.child = fakeScreenChild{}
	}
	return f.child, nil
}

type fakeScreenChild struct{}

func (fakeScreenChild) Wait() error { return nil }

func equalScreenSpec(left, right ScreenSpec) bool {
	if left.Path != right.Path || left.Stdin != right.Stdin || len(left.Args) != len(right.Args) {
		return false
	}
	for i := range left.Args {
		if left.Args[i] != right.Args[i] {
			return false
		}
	}
	return true
}

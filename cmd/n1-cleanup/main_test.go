//go:build darwin && cgo && n1diagnostic && n1cleanup && !n1candidate

package main

import (
	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/backend/tart"
	"testing"
)

func TestPreflightConcreteObserverCannotBecomeCreator(t *testing.T) {
	actual := tart.NewQualifiedObserver(nil, "/fixed/tart", "/fixed/home")
	if _, ok := any(actual).(backend.Creator); !ok {
		t.Fatal("control must demonstrate original capability")
	}
	restricted := readOnlyTart{observer: actual}
	if _, ok := any(restricted).(backend.Creator); ok {
		t.Fatal("creator escaped into read-only preflight")
	}
	var _ backend.Observer = restricted
}

package hostx

import (
	"context"
	"errors"
	"testing"
)

func TestUninstallRefusesActiveOrUnverifiableConsumers(t *testing.T) {
	for name, checker := range map[string]ConsumerChecker{
		"active":  consumerFake{active: true},
		"unknown": consumerFake{err: errors.New("inventory unavailable")},
	} {
		t.Run(name, func(t *testing.T) {
			if err := CheckUninstallable(context.Background(), SoftnetExecutableSHA256, checker); err == nil {
				t.Fatal("CheckUninstallable() error = nil, want refusal")
			}
		})
	}
	if err := CheckUninstallable(context.Background(), SoftnetExecutableSHA256, consumerFake{}); err != nil {
		t.Fatalf("CheckUninstallable(inactive) error = %v", err)
	}
	if err := CheckUninstallable(context.Background(), "0.19.0", consumerFake{}); err == nil {
		t.Fatal("CheckUninstallable(version selector) error = nil, want exact digest refusal")
	}
}

type consumerFake struct {
	active bool
	err    error
}

func (f consumerFake) HasConsumer(_ context.Context, _ string) (bool, error) { return f.active, f.err }

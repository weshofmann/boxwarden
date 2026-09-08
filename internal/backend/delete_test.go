package backend

import (
	"context"
	"testing"
)

func TestDeleteInterfaceAdmitsOnlyOneObjectID(t *testing.T) {
	var _ Deleter = deleterFake{}
}

type deleterFake struct{}

func (deleterFake) Delete(context.Context, string) error { return nil }

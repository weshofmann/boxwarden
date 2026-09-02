package hostx

import (
	"fmt"
	"os"
	"os/user"
	"strconv"
)

type osRootIdentity struct {
	geteuid  func() int
	getenv   func(string) string
	lookupID func(string) (*user.User, error)
}

func newOSRootIdentity() osRootIdentity {
	return osRootIdentity{geteuid: os.Geteuid, getenv: os.Getenv, lookupID: user.LookupId}
}
func (i osRootIdentity) EffectiveUID() int {
	if i.geteuid == nil {
		return os.Geteuid()
	}
	return i.geteuid()
}
func (i osRootIdentity) SudoCaller() (Caller, error) {
	getenv := i.getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	lookup := i.lookupID
	if lookup == nil {
		lookup = user.LookupId
	}
	rawUID, rawName := getenv("SUDO_UID"), getenv("SUDO_USER")
	uid, err := strconv.Atoi(rawUID)
	if err != nil || uid <= 0 || rawName == "" || rawName == "root" {
		return Caller{}, fmt.Errorf("root phase requires a non-root sudo caller")
	}
	entry, err := lookup(strconv.Itoa(uid))
	if err != nil {
		return Caller{}, fmt.Errorf("lookup sudo caller UID: %w", err)
	}
	resolvedUID, err := strconv.Atoi(entry.Uid)
	if err != nil || resolvedUID != uid || entry.Username != rawName || !canonicalAbsolute(entry.HomeDir) {
		return Caller{}, fmt.Errorf("sudo caller environment does not match directory identity")
	}
	return Caller{UID: uid, Name: entry.Username, Home: entry.HomeDir}, nil
}

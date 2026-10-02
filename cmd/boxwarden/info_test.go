package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestInfoWithoutConfiguration(t *testing.T) {
	for _, arg := range []string{"version", "--version", "help", "--help", "-h"} {
		var out bytes.Buffer
		handled, err := runInfo([]string{arg}, &out)
		if !handled || err != nil || !strings.Contains(out.String(), "Boxwarden") {
			t.Fatalf("%s: handled=%v err=%v output=%q", arg, handled, err, out.String())
		}
		if arg == "help" && (!strings.Contains(out.String(), "project") || !strings.Contains(out.String(), "clipboard")) {
			t.Fatal("help omits packaged workflows")
		}
	}
	for _, args := range [][]string{{"project", "create", "demo"}, {"version", "unexpected"}, {"--config", "/fixture/config", "doctor"}} {
		var out bytes.Buffer
		handled, err := runInfo(args, &out)
		if handled || err != nil || out.Len() != 0 {
			t.Fatalf("ordinary argv intercepted: %v", args)
		}
	}
}

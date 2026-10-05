package app

import "testing"

func TestPackagedSetupNeedsResourcesInsteadOfRuntimeCompiler(t *testing.T) {
	args := []string{"setup", "--source-root", "/source", "--formatter-bundle", "/formatter", "--iso", "/ubuntu.iso", "--prebuilt-resources", "/package/resources", "--openssl", "/tools/openssl", "--openssl-sha256", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "--xorriso", "/tools/xorriso", "--xorriso-sha256", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}
	p, err := parseProject(args)
	if err != nil {
		t.Fatal(err)
	}
	if p.setup.Version != 3 || p.setup.GoBinary != "" || p.setup.PrebuiltResources != "/package/resources" {
		t.Fatalf("setup=%#v", p.setup)
	}
	if _, err := parseProject(append(args, "--go", "/tools/go")); err == nil {
		t.Fatal("prebuilt setup accepted a runtime compiler")
	}
}

func TestPrebuiltExportValidationKeepsExclusiveBuildMode(t *testing.T) {
	i := AlphaExportInput{VolumeID: "volume", DestinationParent: "/returned", Selected: []string{"project"}, SourceRoot: "/source", ISOPath: "/ubuntu.iso", PrebuiltResources: "/package/resources"}
	if err := validAlphaExportInput(i); err != nil {
		t.Fatal(err)
	}
	i.GoBinary = "/tools/go"
	if err := validAlphaExportInput(i); err == nil {
		t.Fatal("accepted ambiguous export inputs")
	}
}

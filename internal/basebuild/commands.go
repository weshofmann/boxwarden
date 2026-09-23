package basebuild

import (
	"errors"
	"path/filepath"
)

// Command is a direct executable path with child-observed argv element
// boundaries. Adapters must not render it into a shell command string.
type Command struct {
	Path string
	Args []string
}

func RenderCommand(request RenderRequest) (Command, error) {
	if !validRunID(request.RunID) {
		return Command{}, errors.New("unsupported guest build run ID")
	}
	if !absoluteClean(request.GuestDefinitionRoot) || !absoluteClean(request.VerifierFile) || !absoluteClean(request.OutputDirectory) {
		return Command{}, errors.New("render paths must be canonical and absolute")
	}
	return Command{Path: filepath.Join(request.GuestDefinitionRoot, "render-golden-seed.sh"), Args: []string{request.RunID, request.VerifierFile, request.OutputDirectory}}, nil
}

func validRunID(value string) bool {
	if value == "run-1" || value == "run-2" {
		return true
	}
	if len(value) != len("run-")+12 || value[:4] != "run-" {
		return false
	}
	for _, c := range value[4:] {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

func RemasterCommand(request RemasterRequest) (Command, error) {
	if !absoluteClean(request.GuestDefinitionRoot) || !absoluteClean(request.SourceISO) || !absoluteClean(request.RenderedUserData) || !absoluteClean(request.OutputISO) {
		return Command{}, errors.New("remaster paths must be canonical and absolute")
	}
	return Command{Path: filepath.Join(request.GuestDefinitionRoot, "remaster-golden-iso.sh"), Args: []string{request.SourceISO, request.RenderedUserData, request.OutputISO}}, nil
}

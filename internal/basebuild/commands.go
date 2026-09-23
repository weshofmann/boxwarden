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
	if request.RunID != "run-1" && request.RunID != "run-2" {
		return Command{}, errors.New("unsupported guest build run ID")
	}
	if !absoluteClean(request.GuestDefinitionRoot) || !absoluteClean(request.VerifierFile) || !absoluteClean(request.OutputDirectory) {
		return Command{}, errors.New("render paths must be canonical and absolute")
	}
	return Command{Path: filepath.Join(request.GuestDefinitionRoot, "render-golden-seed.sh"), Args: []string{request.RunID, request.VerifierFile, request.OutputDirectory}}, nil
}

func RemasterCommand(request RemasterRequest) (Command, error) {
	if !absoluteClean(request.GuestDefinitionRoot) || !absoluteClean(request.SourceISO) || !absoluteClean(request.RenderedUserData) || !absoluteClean(request.OutputISO) {
		return Command{}, errors.New("remaster paths must be canonical and absolute")
	}
	return Command{Path: filepath.Join(request.GuestDefinitionRoot, "remaster-golden-iso.sh"), Args: []string{request.SourceISO, request.RenderedUserData, request.OutputISO}}, nil
}

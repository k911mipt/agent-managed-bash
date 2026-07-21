package state

import (
	"os"
	"path/filepath"
	"strings"
)

type pathComponentOpener func(directoryFD int, component string, directory bool) (int, error)

type pathOpenRequest struct {
	workspacePath string
	candidatePath string
	expected      finalPathKind
}

type finalPathKind uint8

const (
	finalRegularFile finalPathKind = iota
	finalDirectory
)

func (p Policy) OpenWorkspacePath(workspacePath string, candidatePath string) (*os.File, Decision) {
	return p.openWorkspacePathWith(pathOpenRequest{
		workspacePath: workspacePath, candidatePath: candidatePath, expected: finalRegularFile,
	}, openPathComponent)
}

func (p Policy) validateWorkspaceDirectories(workspacePath string, cwd string) Decision {
	directory, decision := p.openWorkspacePathWith(pathOpenRequest{
		workspacePath: workspacePath, candidatePath: cwd, expected: finalDirectory,
	}, openPathComponent)
	if !decision.Allowed {
		return decision
	}
	if err := directory.Close(); err != nil {
		return Decision{Allowed: false, Code: p.path.unavailableCode}
	}
	return Decision{Allowed: true, Code: CodeAllow}
}

func (p Policy) validateWorkspacePath(workspacePath string, candidatePath string) (string, Decision) {
	if workspacePath == string(filepath.Separator) || !canonicalAbsolute(workspacePath) || !canonicalAbsolute(candidatePath) {
		return "", Decision{Allowed: false, Code: p.path.invalidCode}
	}
	relative, err := filepath.Rel(workspacePath, candidatePath)
	if err != nil {
		return "", Decision{Allowed: false, Code: p.path.invalidCode}
	}
	if relative != "." && !filepath.IsLocal(relative) {
		return "", Decision{Allowed: false, Code: p.path.outsideCode}
	}
	return relative, Decision{Allowed: true, Code: CodeAllow}
}

func canonicalAbsolute(path string) bool {
	return filepath.IsAbs(path) && filepath.Clean(path) == path
}

func splitPath(path string) []string {
	if path == "." || path == string(filepath.Separator) {
		return nil
	}
	return strings.Split(strings.TrimPrefix(path, string(filepath.Separator)), string(filepath.Separator))
}

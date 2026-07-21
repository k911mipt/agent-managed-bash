//go:build !linux && !darwin

package state

import "os"

func openPathComponent(_ int, _ string, _ bool) (int, error) {
	return -1, os.ErrInvalid
}

func (p Policy) openWorkspacePathWith(
	request pathOpenRequest,
	_ pathComponentOpener,
) (*os.File, Decision) {
	_, lexicalDecision := p.validateWorkspacePath(request.workspacePath, request.candidatePath)
	if !lexicalDecision.Allowed {
		return nil, lexicalDecision
	}
	return nil, Decision{Allowed: false, Code: p.path.unavailableCode}
}

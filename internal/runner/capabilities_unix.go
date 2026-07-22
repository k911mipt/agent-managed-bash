//go:build linux || darwin

package runner

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

func openLaunchCapabilities(workspacePath string, cwdPath string) (*os.File, *os.File, error) {
	workspace, err := openWorkspaceDirectory(workspacePath)
	if err != nil {
		return nil, nil, err
	}
	cwd, err := openDirectoryBeneath(workspace, workspacePath, cwdPath)
	if err != nil {
		return nil, nil, errors.Join(err, workspace.Close())
	}
	return workspace, cwd, nil
}

func openDirectoryBeneath(workspace *os.File, workspacePath string, targetPath string) (*os.File, error) {
	relative, err := filepath.Rel(workspacePath, targetPath)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return nil, ErrUnsafeFilesystem
	}
	current, err := duplicateDirectory(workspace, "cwd")
	if err != nil {
		return nil, err
	}
	if relative == "." {
		return current, nil
	}
	for _, component := range strings.Split(relative, string(filepath.Separator)) {
		next, openErr := openDirectoryAt(current, component, false)
		closeErr := current.Close()
		if openErr != nil || closeErr != nil {
			if next != nil {
				closeErr = errors.Join(closeErr, next.Close())
			}
			return nil, errors.Join(openErr, closeErr)
		}
		current = next
	}
	return current, nil
}

func duplicateDirectory(directory *os.File, name string) (*os.File, error) {
	fd, err := duplicateCloseOnExec(directory)
	if err != nil {
		return nil, err
	}
	duplicated, err := fileFromFD(fd, name)
	if err != nil {
		return nil, err
	}
	if err := validateDirectory(duplicated, false); err != nil {
		return nil, errors.Join(err, duplicated.Close())
	}
	return duplicated, nil
}

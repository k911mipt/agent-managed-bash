//go:build linux || darwin

package state

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func openPathComponent(directoryFD int, component string, directory bool) (int, error) {
	flags := unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK
	if directory {
		flags |= unix.O_DIRECTORY
	}
	return unix.Openat(directoryFD, component, flags, 0)
}

func (p Policy) openWorkspacePathWith(
	request pathOpenRequest,
	opener pathComponentOpener,
) (opened *os.File, decision Decision) {
	relative, lexicalDecision := p.validateWorkspacePath(request.workspacePath, request.candidatePath)
	if !lexicalDecision.Allowed {
		return nil, lexicalDecision
	}

	currentFD, err := unix.Open(string(os.PathSeparator), unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, Decision{Allowed: false, Code: p.path.unavailableCode}
	}
	defer func() {
		if currentFD < 0 {
			return
		}
		if closeErr := unix.Close(currentFD); closeErr != nil {
			if opened != nil {
				if fileCloseErr := opened.Close(); fileCloseErr != nil {
					decision = Decision{Allowed: false, Code: p.path.unavailableCode}
				}
			}
			opened = nil
			decision = Decision{Allowed: false, Code: p.path.unavailableCode}
		}
	}()

	for _, component := range splitPath(request.workspacePath) {
		currentFD, decision = p.openChild(currentFD, pathComponent{name: component, directory: true}, opener)
		if !decision.Allowed {
			return nil, decision
		}
	}

	components := splitPath(relative)
	for index, component := range components {
		last := index == len(components)-1
		currentFD, decision = p.openChild(
			currentFD,
			pathComponent{name: component, directory: !last || request.expected == finalDirectory},
			opener,
		)
		if !decision.Allowed {
			return nil, decision
		}
	}
	if !allowedFinalDescriptor(currentFD, request.expected) {
		return nil, Decision{Allowed: false, Code: p.path.unavailableCode}
	}

	opened = os.NewFile(uintptr(currentFD), request.candidatePath)
	if opened == nil {
		return nil, Decision{Allowed: false, Code: p.path.unavailableCode}
	}
	currentFD = -1
	return opened, Decision{Allowed: true, Code: CodeAllow}
}

func allowedFinalDescriptor(fd int, expected finalPathKind) bool {
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return false
	}
	fileType := stat.Mode & unix.S_IFMT
	switch expected {
	case finalRegularFile:
		return fileType == unix.S_IFREG
	case finalDirectory:
		return fileType == unix.S_IFDIR
	default:
		return false
	}
}

func (p Policy) openChild(
	parentFD int,
	component pathComponent,
	opener pathComponentOpener,
) (int, Decision) {
	childFD, err := opener(parentFD, component.name, component.directory)
	if err != nil {
		if errors.Is(err, unix.ELOOP) || pathComponentIsSymlink(parentFD, component.name) {
			return parentFD, Decision{Allowed: false, Code: p.path.symlinkCode}
		}
		return parentFD, Decision{Allowed: false, Code: p.path.unavailableCode}
	}
	if err := unix.Close(parentFD); err != nil {
		if closeErr := unix.Close(childFD); closeErr != nil {
			return parentFD, Decision{Allowed: false, Code: p.path.unavailableCode}
		}
		return parentFD, Decision{Allowed: false, Code: p.path.unavailableCode}
	}
	return childFD, Decision{Allowed: true, Code: CodeAllow}
}

type pathComponent struct {
	name      string
	directory bool
}

func pathComponentIsSymlink(directoryFD int, component string) bool {
	var stat unix.Stat_t
	if err := unix.Fstatat(directoryFD, component, &stat, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return false
	}
	return stat.Mode&unix.S_IFMT == unix.S_IFLNK
}

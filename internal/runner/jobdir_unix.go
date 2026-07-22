//go:build linux || darwin

package runner

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

const (
	privateDirectoryMode = 0o700
	privateFileMode      = 0o600
)

func openWorkspaceDirectory(path string) (*os.File, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || path == string(filepath.Separator) {
		return nil, fmt.Errorf("workspace %q: %w", path, ErrUnsafeFilesystem)
	}
	current, err := openAbsoluteRoot()
	if err != nil {
		return nil, err
	}
	for _, component := range strings.Split(strings.TrimPrefix(path, string(filepath.Separator)), string(filepath.Separator)) {
		next, openErr := openDirectoryAt(current, component, false)
		closeErr := current.Close()
		if openErr != nil {
			return nil, errors.Join(openErr, closeErr)
		}
		if closeErr != nil {
			return nil, errors.Join(fmt.Errorf("close workspace parent: %w", closeErr), next.Close())
		}
		current = next
	}
	return current, nil
}

func openAbsoluteRoot() (*os.File, error) {
	fd, err := unix.Open(string(filepath.Separator), unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, fmt.Errorf("open filesystem root: %w", err)
	}
	return fileFromFD(fd, string(filepath.Separator))
}

func ensurePrivateDirectory(parent *os.File, name string) (*os.File, error) {
	created := false
	if err := unix.Mkdirat(fileFD(parent), name, privateDirectoryMode); err != nil {
		if !errors.Is(err, unix.EEXIST) {
			return nil, fmt.Errorf("create directory %q: %w", name, err)
		}
	} else {
		created = true
	}
	directory, err := openDirectoryAt(parent, name, true)
	if err != nil {
		return nil, err
	}
	if created {
		if err := parent.Sync(); err != nil {
			return nil, errors.Join(fmt.Errorf("sync parent for %q: %w", name, err), directory.Close())
		}
	}
	return directory, nil
}

func createPrivateDirectory(parent *os.File, name string) (*os.File, error) {
	return createPrivateDirectoryWith(parent, name, directoryCreateOps{
		open: func(parent *os.File, name string) (*os.File, error) {
			return openDirectoryAt(parent, name, true)
		},
		sync: func(parent *os.File) error { return parent.Sync() },
	})
}

type directoryCreateOps struct {
	open func(*os.File, string) (*os.File, error)
	sync func(*os.File) error
}

func createPrivateDirectoryWith(parent *os.File, name string, operations directoryCreateOps) (*os.File, error) {
	if err := unix.Mkdirat(fileFD(parent), name, privateDirectoryMode); err != nil {
		if errors.Is(err, unix.EEXIST) {
			return nil, fmt.Errorf("create directory %q: %w", name, ErrJobExists)
		}
		return nil, fmt.Errorf("create directory %q: %w", name, err)
	}
	directory, err := operations.open(parent, name)
	if err != nil {
		return nil, errors.Join(err, removeDirectory(parent, name))
	}
	if err := operations.sync(parent); err != nil {
		return nil, errors.Join(
			fmt.Errorf("sync new directory %q: %w", name, err),
			directory.Close(), removeDirectory(parent, name),
		)
	}
	return directory, nil
}

func openDirectoryAt(parent *os.File, name string, private bool) (*os.File, error) {
	fd, err := unix.Openat(fileFD(parent), name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, classifyOpenError("open directory "+name, err)
	}
	directory, err := fileFromFD(fd, name)
	if err != nil {
		return nil, err
	}
	if err := validateDirectory(directory, private); err != nil {
		return nil, errors.Join(err, directory.Close())
	}
	return directory, nil
}

func createPrivateFileAt(directory *os.File, name string) (*os.File, error) {
	flags := unix.O_RDWR | unix.O_CREAT | unix.O_EXCL | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK
	fd, err := unix.Openat(fileFD(directory), name, flags, privateFileMode)
	if err != nil {
		return nil, classifyOpenError("create file "+name, err)
	}
	file, err := fileFromFD(fd, name)
	if err != nil {
		return nil, err
	}
	if err := validatePrivateFile(file); err != nil {
		return nil, errors.Join(err, file.Close())
	}
	return file, nil
}

func openPrivateFileAt(directory *os.File, name string, flags int) (*os.File, error) {
	fd, err := unix.Openat(fileFD(directory), name, flags|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, classifyOpenError("open file "+name, err)
	}
	file, err := fileFromFD(fd, name)
	if err != nil {
		return nil, err
	}
	if err := validatePrivateFile(file); err != nil {
		return nil, errors.Join(err, file.Close())
	}
	return file, nil
}

func validateDirectory(file *os.File, private bool) error {
	var stat unix.Stat_t
	if err := unix.Fstat(fileFD(file), &stat); err != nil {
		return fmt.Errorf("inspect directory: %w", err)
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFDIR {
		return ErrUnsafeFilesystem
	}
	if private && (stat.Uid != uint32(os.Geteuid()) || stat.Mode&0o777 != privateDirectoryMode) {
		return ErrUnsafeFilesystem
	}
	return nil
}

func validatePrivateFile(file *os.File) error {
	var stat unix.Stat_t
	if err := unix.Fstat(fileFD(file), &stat); err != nil {
		return fmt.Errorf("inspect file: %w", err)
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Uid != uint32(os.Geteuid()) ||
		stat.Mode&0o777 != privateFileMode || stat.Nlink != 1 {
		return ErrUnsafeFilesystem
	}
	return nil
}

func classifyOpenError(operation string, err error) error {
	if errors.Is(err, unix.ELOOP) || errors.Is(err, unix.ENOTDIR) {
		return fmt.Errorf("%s: %w", operation, ErrUnsafeFilesystem)
	}
	if errors.Is(err, unix.ENOENT) {
		return fmt.Errorf("%s: %w", operation, ErrJobNotFound)
	}
	return fmt.Errorf("%s: %w", operation, err)
}

func fileFromFD(fd int, name string) (*os.File, error) {
	file := os.NewFile(uintptr(fd), name)
	if file == nil {
		return nil, errors.Join(ErrUnsafeFilesystem, unix.Close(fd))
	}
	return file, nil
}

func fileFD(file *os.File) int {
	return int(file.Fd())
}

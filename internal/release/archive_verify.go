package release

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
)

func verifyArchive(input io.Reader, expected expectation) error {
	buffered := bufio.NewReader(input)
	gzipReader, err := gzip.NewReader(buffered)
	if err != nil {
		return fmt.Errorf("open gzip: %w: %w", err, ErrInvalidArchive)
	}
	gzipReader.Multistream(false)
	if !gzipReader.Header.ModTime.Equal(expected.Epoch) || gzipReader.Header.Name != "" || gzipReader.Header.Comment != "" ||
		len(gzipReader.Header.Extra) != 0 || gzipReader.Header.OS != 255 {
		return fmt.Errorf("noncanonical gzip header: %w", ErrInvalidArchive)
	}

	manifestData, hashes, err := verifyTar(tar.NewReader(gzipReader), expected)
	if err != nil {
		return err
	}
	if err := gzipReader.Close(); err != nil {
		return fmt.Errorf("close gzip: %w: %w", err, ErrInvalidArchive)
	}
	if _, err := buffered.Peek(1); err != io.EOF {
		return fmt.Errorf("trailing gzip data: %w", ErrInvalidArchive)
	}
	parsed, err := parseManifest(manifestData, expected)
	if err != nil {
		return err
	}
	canonicalManifest, err := marshalManifest(parsed)
	if err != nil {
		return err
	}
	if !bytes.Equal(manifestData, canonicalManifest) {
		return fmt.Errorf("noncanonical manifest encoding: %w", ErrInvalidArchive)
	}
	for _, item := range parsed.Artifacts {
		if hashes[item.Path] != item.SHA256 {
			return fmt.Errorf("hash mismatch for %q: %w", item.Path, ErrInvalidArchive)
		}
	}
	return nil
}

func verifyTar(reader *tar.Reader, expected expectation) ([]byte, map[string]string, error) {
	root := archiveRoot(expected) + "/"
	allowed := allowedArchiveEntries(root)
	seen := make(map[string]struct{}, len(allowed))
	hashes := make(map[string]string, len(artifactModes))
	var manifestData []byte
	previous := ""
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, fmt.Errorf("read tar: %w: %w", err, ErrInvalidArchive)
		}
		entryMode, exists := allowed[header.Name]
		if !exists || previous >= header.Name {
			return nil, nil, fmt.Errorf("undeclared, duplicate, or unsorted entry %q: %w", header.Name, ErrInvalidArchive)
		}
		previous = header.Name
		if _, duplicate := seen[header.Name]; duplicate {
			return nil, nil, fmt.Errorf("duplicate entry %q: %w", header.Name, ErrInvalidArchive)
		}
		seen[header.Name] = struct{}{}
		if err := verifyHeader(header, entryMode, expected); err != nil {
			return nil, nil, err
		}
		if header.Typeflag == tar.TypeReg {
			if header.Name == root+"manifest.json" {
				manifestData, err = io.ReadAll(reader)
				if err != nil {
					return nil, nil, fmt.Errorf("read manifest: %w: %w", err, ErrInvalidArchive)
				}
				continue
			}
			digest := sha256.New()
			if _, err := io.Copy(digest, reader); err != nil {
				return nil, nil, fmt.Errorf("hash %q: %w: %w", header.Name, err, ErrInvalidArchive)
			}
			hashes[header.Name[len(root):]] = hex.EncodeToString(digest.Sum(nil))
		}
	}
	if len(seen) != len(allowed) || manifestData == nil || len(hashes) != len(artifactModes) {
		return nil, nil, fmt.Errorf("archive layout incomplete: %w", ErrInvalidArchive)
	}
	return manifestData, hashes, nil
}

func verifyHeader(header *tar.Header, mode int64, expected expectation) error {
	directory := header.Name[len(header.Name)-1] == '/'
	expectedType := byte(tar.TypeReg)
	if directory {
		expectedType = tar.TypeDir
	}
	if header.Typeflag != expectedType || header.Mode != mode || !header.ModTime.Equal(expected.Epoch) ||
		header.Uid != 0 || header.Gid != 0 || header.Uname != "" || header.Gname != "" ||
		header.Linkname != "" || header.Format != tar.FormatUSTAR || len(header.PAXRecords) != 0 || len(header.Xattrs) != 0 ||
		!header.AccessTime.IsZero() || !header.ChangeTime.IsZero() {
		return fmt.Errorf("noncanonical metadata for %q: %w", header.Name, ErrInvalidArchive)
	}
	return nil
}

func allowedArchiveEntries(root string) map[string]int64 {
	entries := map[string]int64{
		root: 0o755, root + "bin/": 0o755, root + "lib/": 0o755, root + "lib/opencode/": 0o755,
		root + "manifest.json": 0o644,
	}
	for artifactPath, mode := range artifactModes {
		entries[root+artifactPath] = mode
	}
	return entries
}

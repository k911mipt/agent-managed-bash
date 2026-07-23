package release

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"sort"
)

type archiveEntry struct {
	name     string
	mode     int64
	typeflag byte
	data     []byte
}

func writeArchive(output io.Writer, expected expectation, payloads []payloadFile) (err error) {
	manifestValue, err := buildManifest(expected, payloads)
	if err != nil {
		return err
	}
	manifestData, err := marshalManifest(manifestValue)
	if err != nil {
		return err
	}
	entries := archiveEntries(expected, payloads, manifestData)

	gzipWriter, err := gzip.NewWriterLevel(output, gzip.BestCompression)
	if err != nil {
		return fmt.Errorf("create gzip writer: %w", err)
	}
	gzipWriter.Header.ModTime = expected.Epoch
	gzipWriter.Header.OS = 255
	tarWriter := tar.NewWriter(gzipWriter)
	defer func() {
		err = errors.Join(err, tarWriter.Close(), gzipWriter.Close())
	}()
	for _, entry := range entries {
		header := &tar.Header{
			Name: entry.name, Mode: entry.mode, Size: int64(len(entry.data)), Typeflag: entry.typeflag,
			ModTime: expected.Epoch, Format: tar.FormatUSTAR,
		}
		if entry.typeflag == tar.TypeDir {
			header.Size = 0
		}
		if err := tarWriter.WriteHeader(header); err != nil {
			return fmt.Errorf("write tar header %q: %w", entry.name, err)
		}
		if entry.typeflag == tar.TypeReg {
			if _, err := tarWriter.Write(entry.data); err != nil {
				return fmt.Errorf("write tar payload %q: %w", entry.name, err)
			}
		}
	}
	return nil
}

func archiveEntries(expected expectation, payloads []payloadFile, manifestData []byte) []archiveEntry {
	root := archiveRoot(expected) + "/"
	entries := []archiveEntry{
		{name: root, mode: 0o755, typeflag: tar.TypeDir},
		{name: root + "bin/", mode: 0o755, typeflag: tar.TypeDir},
		{name: root + "lib/", mode: 0o755, typeflag: tar.TypeDir},
		{name: root + "lib/opencode/", mode: 0o755, typeflag: tar.TypeDir},
		{name: root + "manifest.json", mode: 0o644, typeflag: tar.TypeReg, data: manifestData},
	}
	for _, payload := range payloads {
		entries = append(entries, archiveEntry{
			name: root + payload.Path, mode: payload.Mode, typeflag: tar.TypeReg, data: payload.Data,
		})
	}
	sort.Slice(entries, func(first int, second int) bool { return entries[first].name < entries[second].name })
	return entries
}

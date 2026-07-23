package release

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type testArchiveEntry struct {
	header tar.Header
	data   []byte
}

func Test_writeArchive_is_byte_reproducible_and_self_verifying(t *testing.T) {
	// Given
	expected := testExpectation("linux", "amd64")
	payloads := validTestPayloads()
	var first bytes.Buffer
	var second bytes.Buffer

	// When
	firstErr := writeArchive(&first, expected, payloads)
	secondErr := writeArchive(&second, expected, payloads)
	verifyErr := verifyArchive(bytes.NewReader(first.Bytes()), expected)

	// Then
	require.NoError(t, firstErr)
	require.NoError(t, secondErr)
	require.NoError(t, verifyErr)
	require.Equal(t, first.Bytes(), second.Bytes())
}

func Test_writeArchive_normalizes_layout_and_metadata(t *testing.T) {
	// Given
	expected := testExpectation("darwin", "arm64")
	var output bytes.Buffer
	require.NoError(t, writeArchive(&output, expected, validTestPayloads()))

	// When
	gzipReader, err := gzip.NewReader(bytes.NewReader(output.Bytes()))
	require.NoError(t, err)
	entries := readTestArchiveEntries(t, gzipReader)

	// Then
	require.True(t, gzipReader.Header.ModTime.Equal(expected.Epoch))
	require.Empty(t, gzipReader.Header.Name)
	require.Empty(t, gzipReader.Header.Comment)
	require.Nil(t, gzipReader.Header.Extra)
	require.Equal(t, byte(255), gzipReader.Header.OS)
	require.NoError(t, gzipReader.Close())

	root := "agent-managed-bash-0.1.0-darwin-arm64/"
	expectedNames := []string{
		root,
		root + "LICENSE",
		root + "README.md",
		root + "THIRD_PARTY_NOTICES.txt",
		root + "bin/",
		root + "bin/managed-bash",
		root + "install.sh",
		root + "lib/",
		root + "lib/opencode/",
		root + "lib/opencode/managed-bash.js",
		root + "manifest.json",
		root + "uninstall.sh",
	}
	actualNames := make([]string, len(entries))
	for index, entry := range entries {
		actualNames[index] = entry.header.Name
		require.True(t, entry.header.ModTime.Equal(expected.Epoch))
		require.Zero(t, entry.header.Uid)
		require.Zero(t, entry.header.Gid)
		require.Empty(t, entry.header.Uname)
		require.Empty(t, entry.header.Gname)
		require.Empty(t, entry.header.Linkname)
		require.Equal(t, tar.FormatUSTAR, entry.header.Format)
	}
	require.Equal(t, expectedNames, actualNames)
}

func Test_verifyArchive_rejects_malicious_and_noncanonical_entries(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*[]testArchiveEntry)
	}{
		{"absolute path", func(entries *[]testArchiveEntry) { (*entries)[1].header.Name = "/LICENSE" }},
		{"traversal path", func(entries *[]testArchiveEntry) {
			(*entries)[1].header.Name = "agent-managed-bash-0.1.0-linux-amd64/../LICENSE"
		}},
		{"undeclared path", func(entries *[]testArchiveEntry) {
			(*entries)[1].header.Name = "agent-managed-bash-0.1.0-linux-amd64/extra"
		}},
		{"duplicate path", func(entries *[]testArchiveEntry) { *entries = append(*entries, (*entries)[1]) }},
		{"symlink", func(entries *[]testArchiveEntry) {
			(*entries)[1].header.Typeflag = tar.TypeSymlink
			(*entries)[1].header.Linkname = "elsewhere"
		}},
		{"hard link", func(entries *[]testArchiveEntry) {
			(*entries)[1].header.Typeflag = tar.TypeLink
			(*entries)[1].header.Linkname = "elsewhere"
		}},
		{"special file", func(entries *[]testArchiveEntry) { (*entries)[1].header.Typeflag = tar.TypeChar }},
		{"wrong mode", func(entries *[]testArchiveEntry) { (*entries)[1].header.Mode = 0o777 }},
		{"wrong mtime", func(entries *[]testArchiveEntry) {
			(*entries)[1].header.ModTime = (*entries)[1].header.ModTime.Add(time.Second)
		}},
		{"tampered payload", func(entries *[]testArchiveEntry) {
			(*entries)[1].data = []byte("tampered")
			(*entries)[1].header.Size = 8
		}},
		{"noncanonical manifest", func(entries *[]testArchiveEntry) {
			for index := range *entries {
				if !bytes.HasSuffix([]byte((*entries)[index].header.Name), []byte("/manifest.json")) {
					continue
				}
				(*entries)[index].data = bytes.ReplaceAll((*entries)[index].data, []byte("\n"), nil)
				(*entries)[index].header.Size = int64(len((*entries)[index].data))
			}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Given
			expected := testExpectation("linux", "amd64")
			entries := validTestArchiveEntries(t, expected)
			test.mutate(&entries)
			archive := encodeTestArchive(t, expected, entries)

			// When
			err := verifyArchive(bytes.NewReader(archive), expected)

			// Then
			require.Error(t, err)
		})
	}
}

func validTestPayloads() []payloadFile {
	paths := requiredArtifactPaths()
	payloads := make([]payloadFile, len(paths))
	for index, artifactPath := range paths {
		payloads[index] = payloadFile{Path: artifactPath, Mode: requiredArtifactModeValue(artifactPath), Data: []byte("content:" + artifactPath)}
	}
	return payloads
}

func validTestArchiveEntries(t *testing.T, expected expectation) []testArchiveEntry {
	t.Helper()
	var output bytes.Buffer
	require.NoError(t, writeArchive(&output, expected, validTestPayloads()))
	gzipReader, err := gzip.NewReader(bytes.NewReader(output.Bytes()))
	require.NoError(t, err)
	entries := readTestArchiveEntries(t, gzipReader)
	require.NoError(t, gzipReader.Close())
	return entries
}

func readTestArchiveEntries(t *testing.T, reader io.Reader) []testArchiveEntry {
	t.Helper()
	tarReader := tar.NewReader(reader)
	var entries []testArchiveEntry
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		data, err := io.ReadAll(tarReader)
		require.NoError(t, err)
		entries = append(entries, testArchiveEntry{header: *header, data: data})
	}
	return entries
}

func encodeTestArchive(t *testing.T, expected expectation, entries []testArchiveEntry) []byte {
	t.Helper()
	sort.SliceStable(entries, func(first int, second int) bool { return entries[first].header.Name < entries[second].header.Name })
	var output bytes.Buffer
	gzipWriter, err := gzip.NewWriterLevel(&output, gzip.BestCompression)
	require.NoError(t, err)
	gzipWriter.Header.ModTime = expected.Epoch
	gzipWriter.Header.OS = 255
	tarWriter := tar.NewWriter(gzipWriter)
	for _, entry := range entries {
		require.NoError(t, tarWriter.WriteHeader(&entry.header))
		if entry.header.Typeflag == tar.TypeReg {
			_, err := tarWriter.Write(entry.data)
			require.NoError(t, err)
		}
	}
	require.NoError(t, tarWriter.Close())
	require.NoError(t, gzipWriter.Close())
	return output.Bytes()
}

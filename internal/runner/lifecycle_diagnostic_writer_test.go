//go:build linux || darwin

package runner_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

type diagnosticWriter struct {
	buffer bytes.Buffer
}

func (writer *diagnosticWriter) Write(raw []byte) (int, error) {
	written := len(raw)
	remaining := diagnosticOutputLimit + 1 - writer.buffer.Len()
	if remaining > 0 {
		if len(raw) > remaining {
			raw = raw[:remaining]
		}
		_, _ = writer.buffer.Write(raw)
	}
	return written, nil
}

func (writer *diagnosticWriter) String() string {
	return boundedDiagnosticText(writer.buffer.Bytes())
}

func Test_diagnosticWriter_bounds_retained_output_while_draining_writes(t *testing.T) {
	var output diagnosticWriter
	raw := bytes.Repeat([]byte("x"), diagnosticOutputLimit+1024)

	written, err := output.Write(raw)
	repeatedWritten, repeatedErr := output.Write(raw)

	require.NoError(t, err)
	require.NoError(t, repeatedErr)
	require.Equal(t, len(raw), written)
	require.Equal(t, len(raw), repeatedWritten)
	require.Len(t, output.buffer.Bytes(), diagnosticOutputLimit+1)
	require.Equal(t, string(raw[:diagnosticOutputLimit])+"\n<truncated>", output.String())
}

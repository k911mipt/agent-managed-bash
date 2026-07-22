//go:build linux || darwin

package runner

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

const (
	internalProtocolVersion byte = 1
	maximumFrameBytes            = 128 << 10
)

var internalMagic = [4]byte{'A', 'M', 'B', '1'}

type frameKind byte

const (
	frameStart frameKind = iota + 1
	framePrepared
	frameCommit
	frameCommitted
	frameFailure
	frameGuardianReady
	frameShellExited
)

type internalFrame struct {
	kind    frameKind
	payload json.RawMessage
}

func writeFrame(writer io.Writer, kind frameKind, value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode internal frame: %w", err)
	}
	if len(payload) > maximumFrameBytes {
		return fmt.Errorf("encode internal frame: %w", ErrInternalProtocol)
	}
	header := make([]byte, 10)
	copy(header[:4], internalMagic[:])
	header[4] = internalProtocolVersion
	header[5] = byte(kind)
	binary.BigEndian.PutUint32(header[6:], uint32(len(payload)))
	if err := writeFull(writer, header); err != nil {
		return fmt.Errorf("write internal frame header: %w", err)
	}
	if err := writeFull(writer, payload); err != nil {
		return fmt.Errorf("write internal frame payload: %w", err)
	}
	return nil
}

func readFrame(reader io.Reader) (internalFrame, error) {
	header := make([]byte, 10)
	if _, err := io.ReadFull(reader, header); err != nil {
		return internalFrame{}, fmt.Errorf("read internal frame header: %w", err)
	}
	if !bytes.Equal(header[:4], internalMagic[:]) || header[4] != internalProtocolVersion {
		return internalFrame{}, ErrInternalProtocol
	}
	size := binary.BigEndian.Uint32(header[6:])
	if size > maximumFrameBytes {
		return internalFrame{}, ErrInternalProtocol
	}
	payload := make([]byte, size)
	if _, err := io.ReadFull(reader, payload); err != nil {
		return internalFrame{}, fmt.Errorf("read internal frame payload: %w", err)
	}
	if !json.Valid(payload) {
		return internalFrame{}, ErrInternalProtocol
	}
	return internalFrame{kind: frameKind(header[5]), payload: payload}, nil
}

func decodeFrame(frame internalFrame, expected frameKind, target any) error {
	if frame.kind != expected {
		return ErrInternalProtocol
	}
	decoder := json.NewDecoder(bytes.NewReader(frame.payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode internal frame: %w", err)
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return ErrInternalProtocol
	}
	return nil
}

func writeFull(writer io.Writer, raw []byte) error {
	for len(raw) > 0 {
		written, err := writer.Write(raw)
		if err != nil {
			return err
		}
		if written == 0 {
			return io.ErrShortWrite
		}
		raw = raw[written:]
	}
	return nil
}

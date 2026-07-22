//go:build linux || darwin

package runner

import (
	"errors"
	"io"
	"os"
)

type captureEvent struct {
	data []byte
	err  error
	done bool
}

func captureMergedOutput(reader *os.File, events chan<- captureEvent) {
	buffer := make([]byte, 32<<10)
	for {
		read, err := reader.Read(buffer)
		if read > 0 {
			chunk := make([]byte, read)
			copy(chunk, buffer[:read])
			events <- captureEvent{data: chunk}
		}
		if err != nil {
			closeErr := reader.Close()
			if errors.Is(err, io.EOF) {
				err = nil
			}
			events <- captureEvent{err: errors.Join(err, closeErr), done: true}
			return
		}
	}
}

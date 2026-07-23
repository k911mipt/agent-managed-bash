//go:build linux || darwin

package installer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
)

type versionResponse struct {
	SchemaVersion int         `json:"schema_version"`
	Action        string      `json:"action"`
	OK            bool        `json:"ok"`
	Result        versionData `json:"result"`
}

type versionData struct {
	Architecture    string `json:"architecture"`
	BinaryVersion   string `json:"binary_version"`
	OS              string `json:"os"`
	Product         string `json:"product"`
	ProtocolVersion int    `json:"protocol_version"`
}

func verifyBinary(ctx context.Context, binaryPath string, expected identity) error {
	command := exec.CommandContext(ctx, binaryPath, "version")
	command.Stdin = bytes.NewBufferString(`{"schema_version":1,"action":"version"}`)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("run staged binary version: %w: %s", err, stderr.String())
	}
	decoder := json.NewDecoder(io.LimitReader(&stdout, 64<<10))
	decoder.DisallowUnknownFields()
	var response versionResponse
	if err := decoder.Decode(&response); err != nil {
		return fmt.Errorf("decode binary version: %w", err)
	}
	if _, err := decoder.Token(); err != io.EOF {
		return fmt.Errorf("binary version has trailing output")
	}
	if response.SchemaVersion != 1 || response.Action != "version" || !response.OK ||
		response.Result.Product != "managed-bash" || response.Result.BinaryVersion != expected.version ||
		response.Result.ProtocolVersion != 1 || response.Result.OS != expected.os ||
		response.Result.Architecture != expected.architecture {
		return fmt.Errorf("binary version identity mismatch")
	}
	return nil
}

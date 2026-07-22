package cli

import (
	"runtime"

	"github.com/k911mipt/agent-managed-bash/internal/protocol/generated"
)

func (application *Application) versionResponse() generated.VersionResponse {
	return generated.VersionResponse{
		Action: string(generated.ActionVersion),
		Ok:     true,
		Result: generated.VersionData{
			Architecture:    runtime.GOARCH,
			BinaryVersion:   application.config.BinaryVersion,
			Os:              runtime.GOOS,
			Product:         "managed-bash",
			ProtocolVersion: generated.SchemaVersion(1),
		},
		SchemaVersion: 1,
	}
}

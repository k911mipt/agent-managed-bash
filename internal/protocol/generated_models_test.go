package protocol_test

import (
	"testing"

	"github.com/k911mipt/agent-managed-bash/internal/protocol/generated"
)

func Test_GeneratedModels_required_fields_have_concrete_types(_ *testing.T) {
	context := generated.TrustedContext{
		SessionID: "session-1", WorkspacePath: "/workspace", Cwd: "/workspace",
	}
	jobReference := generated.JobReference{JobID: "job-1"}
	job := generated.JobMetadata{
		CapturedBytes:    2,
		Command:          "printf ok",
		CreatedAtUnixMs:  1000,
		Cwd:              "/workspace",
		HardTimeoutMs:    60000,
		JobID:            "job-1",
		OutputLimitBytes: 104857600,
		OwnerSessionID:   "session-1",
		StartedAtUnixMs:  1001,
		Status:           generated.JobStatusRunning,
		WorkspacePath:    "/workspace",
	}
	processResult := generated.ProcessResult{
		CapturedBytes: 2, FinishedAtUnixMs: 1010, Status: generated.TerminalStatusSucceeded,
	}
	observation := generated.JobObservation{Job: job, ProcessResult: &processResult}
	output := generated.OutputChunk{
		CapturedBytes: 2, Eof: true, NextCursorBytes: 2, StartCursorBytes: 0, Text: "ok",
	}
	outputObservation := generated.OutputObservation{Observation: observation, Output: output}
	cancellation := generated.CancellationMetadata{
		Requested: true, RequestedAtUnixMs: 1002, RequestedBySessionID: "session-1",
	}
	cancellationResult := generated.CancellationResult{
		Cancellation: &cancellation,
		JobID:        "job-1",
		Outcome:      generated.CancellationOutcomeRequested,
		Status:       generated.JobStatusRunning,
	}
	removeResult := generated.RemoveResult{JobID: "job-1", Removed: true}
	listResult := generated.ListResult{Jobs: []generated.JobMetadata{job}}
	versionData := generated.VersionData{
		Architecture:    "amd64",
		BinaryVersion:   "1.0.0",
		Os:              "linux",
		Product:         "managed-bash",
		ProtocolVersion: 1,
	}
	protocolError := generated.ProtocolError{Code: generated.ErrorCodeInternal, Message: "internal"}

	requireByteCursor(job.CapturedBytes)
	requireJobID(job.JobID)
	requireJobStatus(job.Status)
	requireSessionID(job.OwnerSessionID)
	requireExitClass(generated.ExitClass(2))

	_ = generated.RunRequest{
		SchemaVersion: 1, Action: "run", Context: context,
		Payload: generated.RunPayload{Command: "printf ok"},
	}
	_ = generated.WaitRequest{
		SchemaVersion: 1, Action: "wait", Context: context,
		Payload: generated.WaitPayload{JobID: "job-1"},
	}
	_ = generated.StatusRequest{
		SchemaVersion: 1, Action: "status", Context: context, Payload: jobReference,
	}
	_ = generated.OutputRequest{
		SchemaVersion: 1, Action: "output", Context: context,
		Payload: generated.OutputPayload{JobID: "job-1"},
	}
	_ = generated.CancelRequest{
		SchemaVersion: 1, Action: "cancel", Context: context, Payload: jobReference,
	}
	_ = generated.RemoveRequest{
		SchemaVersion: 1, Action: "remove", Context: context, Payload: jobReference,
	}
	_ = generated.ListRequest{SchemaVersion: 1, Action: "list", Context: context}
	_ = generated.VersionRequest{SchemaVersion: 1, Action: "version"}

	_ = generated.RunResponse{SchemaVersion: 1, Ok: true, Action: "run", Result: job}
	_ = generated.WaitResponse{
		SchemaVersion: 1, Ok: true, Action: "wait", Result: outputObservation,
	}
	_ = generated.StatusResponse{
		SchemaVersion: 1, Ok: true, Action: "status", Result: observation,
	}
	_ = generated.OutputResponse{
		SchemaVersion: 1, Ok: true, Action: "output", Result: outputObservation,
	}
	_ = generated.CancelResponse{
		SchemaVersion: 1, Ok: true, Action: "cancel", Result: cancellationResult,
	}
	_ = generated.RemoveResponse{
		SchemaVersion: 1, Ok: true, Action: "remove", Result: removeResult,
	}
	_ = generated.ListResponse{
		SchemaVersion: 1, Ok: true, Action: "list", Result: listResult,
	}
	_ = generated.VersionResponse{
		SchemaVersion: 1, Ok: true, Action: "version", Result: versionData,
	}
	_ = generated.ErrorResponse{SchemaVersion: 1, Ok: false, Error: protocolError}
	_ = generated.PersistedJobState{
		SchemaVersion: 1,
		Session: generated.SessionMetadata{
			SchemaVersion: 1, SessionID: "session-1", WorkspacePath: "/workspace", CreatedAtUnixMs: 900,
		},
		Job: job, Result: &processResult, Observers: []generated.ObserverCursor{},
	}
}

func requireByteCursor(value generated.ByteCursor) {}

func requireJobID(value generated.JobID) {}

func requireJobStatus(value generated.JobStatus) {}

func requireSessionID(value generated.SessionID) {}

func requireExitClass(value generated.ExitClass) {}

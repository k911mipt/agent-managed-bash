package cli

import (
	"encoding/json"
	"fmt"

	"github.com/k911mipt/agent-managed-bash/internal/protocol/generated"
)

type requestValue interface {
	action() generated.Action
	assertedContext() (generated.TrustedContext, bool)
}

type startRequest struct{ generated.StartRequest }
type runRequest struct{ generated.RunRequest }
type waitRequest struct{ generated.WaitRequest }
type statusRequest struct{ generated.StatusRequest }
type outputRequest struct{ generated.OutputRequest }
type cancelRequest struct{ generated.CancelRequest }
type removeRequest struct{ generated.RemoveRequest }
type listRequest struct{ generated.ListRequest }
type versionRequest struct{ generated.VersionRequest }

func (startRequest) action() generated.Action  { return generated.ActionStart }
func (runRequest) action() generated.Action    { return generated.ActionRun }
func (waitRequest) action() generated.Action   { return generated.ActionWait }
func (statusRequest) action() generated.Action { return generated.ActionStatus }
func (outputRequest) action() generated.Action { return generated.ActionOutput }
func (cancelRequest) action() generated.Action { return generated.ActionCancel }
func (removeRequest) action() generated.Action { return generated.ActionRemove }
func (listRequest) action() generated.Action   { return generated.ActionList }
func (versionRequest) action() generated.Action {
	return generated.ActionVersion
}

func (request startRequest) assertedContext() (generated.TrustedContext, bool) {
	return request.Context, true
}
func (request runRequest) assertedContext() (generated.TrustedContext, bool) {
	return request.Context, true
}
func (request waitRequest) assertedContext() (generated.TrustedContext, bool) {
	return request.Context, true
}
func (request statusRequest) assertedContext() (generated.TrustedContext, bool) {
	return request.Context, true
}
func (request outputRequest) assertedContext() (generated.TrustedContext, bool) {
	return request.Context, true
}
func (request cancelRequest) assertedContext() (generated.TrustedContext, bool) {
	return request.Context, true
}
func (request removeRequest) assertedContext() (generated.TrustedContext, bool) {
	return request.Context, true
}
func (request listRequest) assertedContext() (generated.TrustedContext, bool) {
	return request.Context, true
}
func (versionRequest) assertedContext() (generated.TrustedContext, bool) {
	return generated.TrustedContext{}, false
}

func decodeValidatedRequest(action generated.Action, raw []byte) (requestValue, error) {
	switch action {
	case generated.ActionStart:
		var request generated.StartRequest
		if err := json.Unmarshal(raw, &request); err != nil {
			return nil, decodeValidatedRequestError(err)
		}
		return startRequest{request}, nil
	case generated.ActionRun:
		var request generated.RunRequest
		if err := json.Unmarshal(raw, &request); err != nil {
			return nil, decodeValidatedRequestError(err)
		}
		return runRequest{request}, nil
	case generated.ActionWait:
		var request generated.WaitRequest
		if err := json.Unmarshal(raw, &request); err != nil {
			return nil, decodeValidatedRequestError(err)
		}
		return waitRequest{request}, nil
	case generated.ActionStatus:
		var request generated.StatusRequest
		if err := json.Unmarshal(raw, &request); err != nil {
			return nil, decodeValidatedRequestError(err)
		}
		return statusRequest{request}, nil
	case generated.ActionOutput:
		var request generated.OutputRequest
		if err := json.Unmarshal(raw, &request); err != nil {
			return nil, decodeValidatedRequestError(err)
		}
		return outputRequest{request}, nil
	case generated.ActionCancel:
		var request generated.CancelRequest
		if err := json.Unmarshal(raw, &request); err != nil {
			return nil, decodeValidatedRequestError(err)
		}
		return cancelRequest{request}, nil
	case generated.ActionRemove:
		var request generated.RemoveRequest
		if err := json.Unmarshal(raw, &request); err != nil {
			return nil, decodeValidatedRequestError(err)
		}
		return removeRequest{request}, nil
	case generated.ActionList:
		var request generated.ListRequest
		if err := json.Unmarshal(raw, &request); err != nil {
			return nil, decodeValidatedRequestError(err)
		}
		return listRequest{request}, nil
	case generated.ActionVersion:
		var request generated.VersionRequest
		if err := json.Unmarshal(raw, &request); err != nil {
			return nil, decodeValidatedRequestError(err)
		}
		return versionRequest{request}, nil
	default:
		return nil, fmt.Errorf("decode validated request: unknown action %q", action)
	}
}

func decodeValidatedRequestError(err error) error {
	return fmt.Errorf("decode validated request: %w", err)
}

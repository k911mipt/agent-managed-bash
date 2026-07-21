package state

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/k911mipt/agent-managed-bash/internal/protocol/generated"
	"github.com/stretchr/testify/require"
)

type policyCases struct {
	Transitions   []transitionCase    `json:"transitions"`
	Authorization []authorizationCase `json:"authorization"`
	Removal       []removalCase       `json:"removal"`
	Cancellation  []cancellationCase  `json:"cancellation"`
	Cursors       []cursorCase        `json:"cursors"`
	Ranges        []rangeCase         `json:"ranges"`
	Capture       []captureCase       `json:"capture"`
	Output        []outputCase        `json:"output"`
	OutputRanges  []outputRangeCase   `json:"output_ranges"`
	Paths         []pathCase          `json:"paths"`
	State         []stateCase         `json:"state_validation"`
	Observers     []observerCase      `json:"observers"`
}

type transitionCase struct {
	Name    string              `json:"name"`
	From    generated.JobStatus `json:"from"`
	To      generated.JobStatus `json:"to"`
	Allowed bool                `json:"allowed"`
	Code    Code                `json:"code"`
}

type authorizationCase struct {
	Name          string `json:"name"`
	Operation     string `json:"operation"`
	SameWorkspace bool   `json:"same_workspace"`
	Owner         bool   `json:"owner"`
	Allowed       bool   `json:"allowed"`
	Code          Code   `json:"code"`
}

type removalCase struct {
	Name    string              `json:"name"`
	Status  generated.JobStatus `json:"status"`
	Allowed bool                `json:"allowed"`
	Code    Code                `json:"code"`
}

type cancellationCase struct {
	Name             string              `json:"name"`
	Status           generated.JobStatus `json:"status"`
	AlreadyRequested bool                `json:"already_requested"`
	Allowed          bool                `json:"allowed"`
	Code             Code                `json:"code"`
	PersistRequest   bool                `json:"persist_request"`
}

type cursorCase struct {
	Name     string `json:"name"`
	Cursor   int64  `json:"cursor"`
	Captured int64  `json:"captured"`
	Allowed  bool   `json:"allowed"`
	Code     Code   `json:"code"`
}

type rangeCase struct {
	Name     string `json:"name"`
	Start    int64  `json:"start"`
	End      int64  `json:"end"`
	Captured int64  `json:"captured"`
	Allowed  bool   `json:"allowed"`
	Code     Code   `json:"code"`
}

type captureCase struct {
	Name     string `json:"name"`
	Captured int64  `json:"captured"`
	Incoming int64  `json:"incoming"`
	Accepted int64  `json:"accepted"`
}

type outputCase struct {
	Name     string `json:"name"`
	RawHex   string `json:"raw_hex"`
	Start    int64  `json:"start"`
	End      int64  `json:"end"`
	Captured int64  `json:"captured"`
	Text     string `json:"text"`
	Next     int64  `json:"next"`
	Eof      bool   `json:"eof"`
	Terminal bool   `json:"terminal"`
}

type outputRangeCase struct {
	Name          string `json:"name"`
	Start         *int64 `json:"start"`
	End           *int64 `json:"end"`
	Captured      int64  `json:"captured"`
	ResolvedStart int64  `json:"resolved_start"`
	ResolvedEnd   int64  `json:"resolved_end"`
	Allowed       bool   `json:"allowed"`
	Code          Code   `json:"code"`
}

type observerCase struct {
	Name       string           `json:"name"`
	Action     generated.Action `json:"action"`
	Explicit   *int64           `json:"explicit_cursor"`
	Persisted  *int64           `json:"persisted_cursor"`
	Resolved   int64            `json:"resolved_cursor"`
	Next       int64            `json:"next_cursor"`
	Advanced   int64            `json:"advanced_cursor"`
	ShouldMove bool             `json:"should_advance"`
}

type pathCase struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Allowed bool   `json:"allowed"`
	Code    Code   `json:"code"`
}

type stateCase struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Allowed bool   `json:"allowed"`
	Code    Code   `json:"code"`
}

func loadPolicyCases(t *testing.T) policyCases {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join("..", "..", "fixtures", "v1", "policy", "cases.json"))
	require.NoError(t, err)
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var cases policyCases
	require.NoError(t, decoder.Decode(&cases))
	_, err = decoder.Token()
	require.ErrorIs(t, err, io.EOF)
	require.NotEmpty(t, cases.Transitions)
	require.NotEmpty(t, cases.Authorization)
	require.NotEmpty(t, cases.Removal)
	require.NotEmpty(t, cases.Cancellation)
	require.NotEmpty(t, cases.Cursors)
	require.NotEmpty(t, cases.Ranges)
	require.NotEmpty(t, cases.Capture)
	require.NotEmpty(t, cases.Output)
	require.NotEmpty(t, cases.OutputRanges)
	require.NotEmpty(t, cases.Paths)
	require.NotEmpty(t, cases.State)
	require.NotEmpty(t, cases.Observers)
	return cases
}

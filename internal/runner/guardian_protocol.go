//go:build linux || darwin

package runner

type guardianStart struct {
	Command string `json:"command"`
	GraceMs int64  `json:"grace_ms"`
}

type guardianReady struct {
	GuardianPID    int    `json:"guardian_pid"`
	ShellPID       int    `json:"shell_pid"`
	ProcessGroupID int    `json:"process_group_id"`
	BirthIdentity  string `json:"birth_identity"`
}

type shellExit struct {
	ExitCode *int `json:"exit_code,omitempty"`
	Signal   *int `json:"signal,omitempty"`
}

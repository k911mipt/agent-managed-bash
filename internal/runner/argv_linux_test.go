//go:build linux

package runner_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func Test_Manager_keeps_actual_command_bytes_out_of_every_process_argv(t *testing.T) {
	secret := "argv-secret-6f2f32b8"
	command := `secret=` + secret + `; pid=$$; leaked=0; while [ "$pid" -gt 1 ]; do ` +
		`arguments=$(tr '\0' '\n' </proc/$pid/cmdline); case "$arguments" in *"$secret"*) leaked=1;; esac; ` +
		`pid=$(awk '{print $4}' /proc/$pid/stat); done; if [ "$leaked" -eq 0 ]; then printf clean; else printf leaked; fi`

	result := runLifecycle(t, command, 2*time.Second, 1<<20)

	require.Equal(t, "clean", string(result.output))
}

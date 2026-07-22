//go:build linux

package runner

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

func processBirthIdentity(pid int) (string, error) {
	if pid <= 0 {
		return "", ErrExecution
	}
	raw, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return "", fmt.Errorf("read process identity: %w", err)
	}
	closingParenthesis := strings.LastIndexByte(string(raw), ')')
	if closingParenthesis < 0 || closingParenthesis+2 >= len(raw) {
		return "", ErrExecution
	}
	fields := strings.Fields(string(raw[closingParenthesis+2:]))
	if len(fields) <= 19 {
		return "", ErrExecution
	}
	if _, err := strconv.ParseUint(fields[19], 10, 64); err != nil {
		return "", fmt.Errorf("parse process identity: %w", err)
	}
	return "linux-starttime:" + fields[19], nil
}

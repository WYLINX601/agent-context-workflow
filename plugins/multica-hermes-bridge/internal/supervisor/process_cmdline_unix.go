//go:build !windows

package supervisor

import (
	"os/exec"
	"strconv"
	"strings"
)

func processCommandLine(pid int) string {
	if output, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "command=").Output(); err == nil {
		return strings.TrimSpace(string(output))
	}
	return ""
}

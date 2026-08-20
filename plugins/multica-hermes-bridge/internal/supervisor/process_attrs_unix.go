//go:build !windows

package supervisor

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func configureProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func processMarkersRequired() bool { return true }

func stopPID(identity processIdentity, grace time.Duration) error {
	pid := identity.PID
	if pid <= 0 {
		return nil
	}
	if err := validateProcessIdentity(identity); err != nil {
		return err
	}
	if err := syscall.Kill(-pid, syscall.SIGTERM); err != nil {
		if err := syscall.Kill(pid, syscall.SIGTERM); err != nil && err != syscall.ESRCH {
			return err
		}
	}
	deadline := time.Now().Add(grace)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil {
		if err := syscall.Kill(pid, syscall.SIGKILL); err != nil && err != syscall.ESRCH {
			return err
		}
	}
	return nil
}

func processStartToken(pid int) string {
	if pid <= 0 {
		return ""
	}
	if output, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "lstart=").Output(); err == nil {
		if value := string(output); value != "" {
			return strings.TrimSpace(value)
		}
	}
	return "unknown"
}

func processExecutablePath(pid int) string {
	if pid <= 0 {
		return ""
	}
	if path, err := os.Readlink(filepath.Join("/proc", strconv.Itoa(pid), "exe")); err == nil && path != "" {
		return normalizeExecutablePath(path)
	}
	if output, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "comm=").Output(); err == nil {
		return normalizeExecutablePath(strings.TrimSpace(string(output)))
	}
	return ""
}

func processEnvironmentValue(pid int, wanted string) string {
	if pid <= 0 || wanted == "" {
		return ""
	}
	if data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "environ")); err == nil {
		for _, entry := range strings.Split(string(data), "\x00") {
			key, value, found := strings.Cut(entry, "=")
			if found && key == wanted {
				return value
			}
		}
	}
	// macOS does not expose /proc by default. `ps eww` includes the process
	// environment for same-user processes, which is sufficient for the marker
	// values Supervisor injects and avoids parsing arbitrary command arguments.
	if output, err := exec.Command("ps", "eww", "-p", strconv.Itoa(pid), "-o", "command=").Output(); err == nil {
		for _, entry := range strings.Fields(string(output)) {
			key, value, found := strings.Cut(entry, "=")
			if found && key == wanted {
				return value
			}
		}
	}
	return ""
}

func normalizeExecutablePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	return filepath.Clean(path)
}

func stopProcess(cmd *exec.Cmd, graceSeconds int, identity processIdentity) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	if err := validateProcessIdentity(identity); err != nil {
		return err
	}
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM); err != nil {
		_ = cmd.Process.Signal(syscall.SIGTERM)
	}
	if graceSeconds > 0 {
		// The manager waits for the process with a bounded context; this value
		// is only retained for the platform-specific signature.
	}
	return nil
}

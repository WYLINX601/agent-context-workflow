//go:build windows

package supervisor

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

func configureProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
}

func processMarkersRequired() bool { return false }

func stopProcess(cmd *exec.Cmd, _ int, identity processIdentity) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	if err := validateProcessIdentity(identity); err != nil {
		return err
	}
	return cmd.Process.Kill()
}

func stopPID(identity processIdentity, _ time.Duration) error {
	if identity.PID <= 0 || identity.ProcessStartToken == "" || identity.ProcessStartToken == "unknown" {
		return fmt.Errorf("cannot safely reattach Windows process %d without process identity", identity.PID)
	}
	if err := validateProcessIdentity(identity); err != nil {
		return err
	}
	if !processAlive(identity.PID) {
		return nil
	}
	return exec.Command("taskkill", "/PID", strconv.Itoa(identity.PID), "/T", "/F").Run()
}

func processStartToken(pid int) string {
	if pid <= 0 {
		return ""
	}
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return "unknown"
	}
	defer windows.CloseHandle(handle)
	var created, exited, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(handle, &created, &exited, &kernel, &user); err != nil {
		return "unknown"
	}
	return strconv.FormatInt(created.Nanoseconds(), 10)
}

func processExecutablePath(pid int) string {
	if pid <= 0 {
		return ""
	}
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(handle)
	buffer := make([]uint16, windows.MAX_PATH)
	length := uint32(len(buffer))
	if err := windows.QueryFullProcessImageName(handle, 0, &buffer[0], &length); err != nil {
		return ""
	}
	return normalizeExecutablePath(windows.UTF16ToString(buffer[:length]))
}

func processEnvironmentValue(_ int, _ string) string {
	// Windows process environments are intentionally not read through a shell.
	// PID start time plus the executable path remain the portable identity proof;
	// Job Object ownership is the follow-up hardening for managed Windows runs.
	return ""
}

func normalizeExecutablePath(path string) string {
	return strings.ToLower(filepath.Clean(strings.TrimSpace(path)))
}

//go:build !windows

package supervisor

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// findProcessByLaunchNonce recovers the narrow crash window between Hermes
// exec.Cmd.Start and the registry transaction that records its PID. A process
// is a candidate only when the Supervisor-injected nonce and endpoint both
// match; an arbitrary listener on the same port is never adopted.
func findProcessByLaunchNonce(launchNonce, endpoint, executablePath string) (processIdentity, bool) {
	launchNonce = strings.TrimSpace(launchNonce)
	endpoint = strings.TrimSpace(endpoint)
	if launchNonce == "" || endpoint == "" {
		return processIdentity{}, false
	}
	if entries, err := os.ReadDir("/proc"); err == nil {
		for _, entry := range entries {
			pid, err := strconv.Atoi(entry.Name())
			if err != nil || pid <= 0 || processEnvironmentValue(pid, "MHG_SUPERVISOR_LAUNCH_NONCE") != launchNonce {
				continue
			}
			if processEnvironmentValue(pid, "MHG_SUPERVISOR_ENDPOINT") != endpoint {
				continue
			}
			if actual, ok := provenDiscoveredProcess(pid, endpoint, executablePath); ok {
				return actual, true
			}
		}
		return processIdentity{}, false
	}

	// macOS normally has no /proc. `ps eww` gives us the same marker search in
	// one process-list call, then the common proof checks the candidate PID.
	output, err := exec.Command("ps", "eww", "-axo", "pid=,command=").Output()
	if err != nil {
		return processIdentity{}, false
	}
	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil || pid <= 0 {
			continue
		}
		launchFound, endpointFound := false, false
		for _, field := range fields[1:] {
			key, value, found := strings.Cut(field, "=")
			if !found {
				continue
			}
			switch key {
			case "MHG_SUPERVISOR_LAUNCH_NONCE":
				launchFound = value == launchNonce
			case "MHG_SUPERVISOR_ENDPOINT":
				endpointFound = value == endpoint
			}
		}
		if launchFound && endpointFound {
			if actual, ok := provenDiscoveredProcess(pid, endpoint, executablePath); ok {
				return actual, true
			}
		}
	}
	return processIdentity{}, false
}

func provenDiscoveredProcess(pid int, endpoint, executablePath string) (processIdentity, bool) {
	actual := currentProcessIdentity(pid, endpoint)
	if actual.ProcessStartToken == "" || actual.ProcessStartToken == "unknown" || actual.LaunchNonce == "" || actual.LaunchNonce != processEnvironmentValue(pid, "MHG_SUPERVISOR_LAUNCH_NONCE") {
		return processIdentity{}, false
	}
	if actual.Endpoint != endpoint {
		return processIdentity{}, false
	}
	if executablePath != "" && normalizeExecutablePath(executablePath) != normalizeExecutablePath(actual.ExecutablePath) {
		return processIdentity{}, false
	}
	return actual, true
}

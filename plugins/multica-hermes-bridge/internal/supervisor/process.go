package supervisor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type processIdentity struct {
	PID                int
	ProcessStartToken  string
	ExecutablePath     string
	CommandFingerprint string
	Endpoint           string
	LaunchNonce        string
}

type managedProcess struct {
	cmd      *exec.Cmd
	identity processIdentity
	args     []string
}

func identityMatches(expected, actual processIdentity) bool {
	if expected.PID <= 0 || expected.PID != actual.PID {
		return false
	}
	if expected.ProcessStartToken == "" || expected.ProcessStartToken == "unknown" || actual.ProcessStartToken == "" || actual.ProcessStartToken == "unknown" {
		return false
	}
	if expected.ProcessStartToken != actual.ProcessStartToken {
		return false
	}
	if expected.ExecutablePath != "" && (actual.ExecutablePath == "" || normalizeExecutablePath(expected.ExecutablePath) != normalizeExecutablePath(actual.ExecutablePath)) {
		return false
	}
	if expected.CommandFingerprint != "" && (actual.CommandFingerprint == "" || expected.CommandFingerprint != actual.CommandFingerprint) {
		return false
	}
	if expected.Endpoint != "" {
		if actual.Endpoint == "" {
			if processMarkersRequired() {
				return false
			}
		} else if expected.Endpoint != actual.Endpoint {
			return false
		}
	}
	if expected.LaunchNonce != "" {
		if actual.LaunchNonce == "" {
			if processMarkersRequired() {
				return false
			}
		} else if expected.LaunchNonce != actual.LaunchNonce {
			return false
		}
	}
	return true
}

func commandFingerprint(args []string) string {
	digest := sha256.Sum256([]byte(strings.Join(args, "\x00")))
	return hex.EncodeToString(digest[:])
}

func startHermes(_ context.Context, executable string, args []string, sessionToken, runtimeID, launchNonce, endpoint string) (*managedProcess, error) {
	if strings.TrimSpace(executable) == "" {
		return nil, fmt.Errorf("Hermes executable is empty")
	}
	// The acquisition deadline only bounds startup/readiness. Once the child
	// has started, Supervisor owns it independently of the IPC request context.
	cmd := exec.Command(executable, args...)
	configureProcess(cmd)
	overrides := map[string]string{
		"MHG_SUPERVISOR_RUNTIME_ID":   runtimeID,
		"MHG_SUPERVISOR_LAUNCH_NONCE": launchNonce,
		"MHG_SUPERVISOR_ENDPOINT":     endpoint,
	}
	if sessionToken != "" {
		overrides["HERMES_DASHBOARD_SESSION_TOKEN"] = sessionToken
	}
	cmd.Env = filteredEnvironment(overrides)
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	identity := processIdentity{
		PID:               cmd.Process.Pid,
		ProcessStartToken: processStartToken(cmd.Process.Pid),
		LaunchNonce:       launchNonce,
	}
	identity.ExecutablePath = processExecutablePath(cmd.Process.Pid)
	identity.CommandFingerprint = commandFingerprintForProcess(cmd.Process.Pid, executable, args)
	return &managedProcess{cmd: cmd, identity: identity, args: append([]string(nil), args...)}, nil
}

func filteredEnvironment(overrides map[string]string) []string {
	env := make([]string, 0, len(os.Environ())+len(overrides))
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if _, overridden := overrides[key]; overridden {
			continue
		}
		env = append(env, entry)
	}
	for key, value := range overrides {
		if value != "" {
			env = append(env, key+"="+value)
		}
	}
	return env
}

func commandFingerprintForProcess(pid int, executable string, args []string) string {
	if line := processCommandLine(pid); line != "" {
		return commandFingerprint([]string{line})
	}
	// Some Windows APIs do not expose argv without WMI/PowerShell. The
	// executable path and process start token remain the independent proof on
	// those platforms; do not invent a command fingerprint from expected args.
	return ""
}

func currentProcessIdentity(pid int, _ string) processIdentity {
	return processIdentity{
		PID:                pid,
		ProcessStartToken:  processStartToken(pid),
		ExecutablePath:     processExecutablePath(pid),
		CommandFingerprint: commandFingerprintForProcess(pid, "", nil),
		Endpoint:           processEnvironmentValue(pid, "MHG_SUPERVISOR_ENDPOINT"),
		LaunchNonce:        processEnvironmentValue(pid, "MHG_SUPERVISOR_LAUNCH_NONCE"),
	}
}

func validateProcessIdentity(identity processIdentity) error {
	if identity.PID <= 0 || !processAlive(identity.PID) {
		return nil
	}
	if !processIdentityCurrent(identity) {
		return fmt.Errorf("managed process identity changed for PID %d", identity.PID)
	}
	return nil
}

func processIdentityCurrent(identity processIdentity) bool {
	if identity.PID <= 0 || !processAlive(identity.PID) {
		return false
	}
	return identityMatches(identity, currentProcessIdentity(identity.PID, identity.Endpoint))
}

func (p *managedProcess) stop(grace time.Duration) error {
	if p == nil || p.identity.PID <= 0 {
		return nil
	}
	if !processAlive(p.identity.PID) {
		return nil
	}
	if err := validateProcessIdentity(p.identity); err != nil {
		return err
	}
	if p.cmd == nil {
		return stopPID(p.identity, grace)
	}
	if p.cmd.Process == nil {
		return nil
	}
	if err := stopProcess(p.cmd, int(grace/time.Second), p.identity); err != nil {
		return err
	}
	wait := make(chan error, 1)
	go func() { wait <- p.cmd.Wait() }()
	select {
	case err := <-wait:
		if !processAlive(p.identity.PID) {
			return nil
		}
		return err
	case <-time.After(grace):
		_ = p.cmd.Process.Kill()
		err := <-wait
		if !processAlive(p.identity.PID) {
			return nil
		}
		return err
	}
}

func reattachedProcess(identity processIdentity, args []string) *managedProcess {
	return &managedProcess{identity: identity, args: append([]string(nil), args...)}
}

func processArgs(record registryRuntime) []string {
	host := endpointHost(record.StatusURL)
	port, err := endpointPort(record.StatusURL)
	if err != nil {
		return nil
	}
	args := make([]string, 0, 9)
	if record.Scope == "isolated" && record.Profile != "" {
		args = append(args, "--profile", record.Profile)
	}
	args = append(args, "serve", "--host", host, "--port", strconv.Itoa(port), "--no-open")
	if record.Scope == "isolated" {
		args = append(args, "--isolated")
	}
	return args
}

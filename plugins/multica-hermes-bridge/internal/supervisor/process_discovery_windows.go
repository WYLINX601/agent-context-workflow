//go:build windows

package supervisor

// Windows recovery intentionally does not scan arbitrary processes. The
// managed Windows implementation will use Job Object membership as the
// authoritative ownership proof; until then a PID-less STARTING record fails
// closed instead of adopting a same-port process.
func findProcessByLaunchNonce(_, _, _ string) (processIdentity, bool) {
	return processIdentity{}, false
}

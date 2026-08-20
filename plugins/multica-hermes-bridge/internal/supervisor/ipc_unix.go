//go:build !windows

package supervisor

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"
)

func listenIPC(endpoint string) (net.Listener, error) {
	if err := os.MkdirAll(filepath.Dir(endpoint), 0o700); err != nil {
		return nil, err
	}
	if _, err := os.Stat(endpoint); err == nil {
		probe, probeErr := net.DialTimeout("unix", endpoint, 200*time.Millisecond)
		if probeErr == nil {
			_ = probe.Close()
			return nil, fmt.Errorf("supervisor socket is already in use: %s", endpoint)
		}
		if removeErr := os.Remove(endpoint); removeErr != nil && !os.IsNotExist(removeErr) {
			return nil, fmt.Errorf("remove stale supervisor socket: %w", removeErr)
		}
	}
	listener, err := net.Listen("unix", endpoint)
	if err != nil {
		return nil, fmt.Errorf("listen supervisor socket: %w", err)
	}
	_ = os.Chmod(endpoint, 0o600)
	return listener, nil
}

func dialIPC(ctx context.Context, endpoint string) (net.Conn, error) {
	return (&net.Dialer{}).DialContext(ctx, "unix", endpoint)
}

func closeIPC(endpoint string) { _ = os.Remove(endpoint) }

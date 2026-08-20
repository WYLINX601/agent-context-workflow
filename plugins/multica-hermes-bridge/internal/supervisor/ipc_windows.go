//go:build windows

package supervisor

import (
	"context"
	"fmt"
	"net"
	"strings"

	"github.com/Microsoft/go-winio"
)

func normalizePipeEndpoint(endpoint string) string {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return `\\.\pipe\multica-hermes-supervisor`
	}
	if strings.HasPrefix(strings.ToLower(endpoint), `\\.\pipe\`) {
		return endpoint
	}
	endpoint = strings.Trim(endpoint, `/\\`)
	if index := strings.LastIndexAny(endpoint, `/\\`); index >= 0 {
		endpoint = endpoint[index+1:]
	}
	if endpoint == "" {
		endpoint = "multica-hermes-supervisor"
	}
	return `\\.\pipe\` + endpoint
}

func listenIPC(endpoint string) (net.Listener, error) {
	path := normalizePipeEndpoint(endpoint)
	listener, err := winio.ListenPipe(path, &winio.PipeConfig{
		SecurityDescriptor: "D:P(A;;GA;;;OW)",
	})
	if err != nil {
		return nil, fmt.Errorf("listen supervisor named pipe: %w", err)
	}
	return listener, nil
}

func dialIPC(ctx context.Context, endpoint string) (net.Conn, error) {
	return winio.DialPipeContext(ctx, normalizePipeEndpoint(endpoint))
}

func closeIPC(_ string) {}

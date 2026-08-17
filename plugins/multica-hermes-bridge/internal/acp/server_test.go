package acp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

type testHandler struct{}

func (testHandler) Handle(_ context.Context, method string, _ json.RawMessage, _ Emitter) (any, *RPCError) {
	if method == "initialize" {
		return map[string]any{"protocolVersion": 1}, nil
	}
	return nil, MethodNotFound(method)
}

func (testHandler) Shutdown(context.Context) {}

func TestServerUsesLineDelimitedJSONRPCAndDoesNotWriteLogsToStdout(t *testing.T) {
	input := strings.NewReader("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"initialize\",\"params\":{}}\n")
	var output bytes.Buffer
	server := NewServer(input, &output, testHandler{}, nil)
	if err := server.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	var response map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(output.Bytes()), &response); err != nil {
		t.Fatalf("invalid response %q: %v", output.String(), err)
	}
	if response["jsonrpc"] != "2.0" || response["id"].(float64) != 1 {
		t.Fatalf("unexpected response: %v", response)
	}
	if !bytes.Contains(output.Bytes(), []byte(`"protocolVersion":1`)) {
		t.Fatalf("missing initialize result: %s", output.Bytes())
	}
}

func TestServerRoutesClientResponsesToPendingRequest(t *testing.T) {
	reader, writer := io.Pipe()
	var output bytes.Buffer
	server := NewServer(reader, &output, testHandler{}, nil)
	done := make(chan error, 1)
	go func() { done <- server.Run(context.Background()) }()
	if _, err := writer.Write([]byte("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"initialize\",\"params\":{}}\n")); err != nil {
		t.Fatal(err)
	}
	_ = writer.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(output.Bytes(), []byte(`"id":1`)) {
		t.Fatalf("response was not flushed: %s", output.Bytes())
	}
}

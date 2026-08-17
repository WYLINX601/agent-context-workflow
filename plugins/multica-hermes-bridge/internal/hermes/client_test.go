package hermes

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/WYLINX601/project-context-workflow/plugins/multica-hermes-bridge/internal/config"
)

func TestClientConnectsToGatewayAndCorrelatesRPC(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer conn.Close()
		_ = conn.WriteJSON(map[string]any{"jsonrpc": "2.0", "method": "event", "params": map[string]any{"type": "gateway.ready"}})
		for {
			var message map[string]any
			if err := conn.ReadJSON(&message); err != nil {
				return
			}
			id := message["id"]
			method, _ := message["method"].(string)
			switch method {
			case "model.options":
				_ = conn.WriteJSON(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{"current_model": "test:model"}})
			case "ping":
				_ = conn.WriteJSON(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{"ok": true}})
				_ = conn.WriteJSON(map[string]any{"jsonrpc": "2.0", "method": "event", "params": map[string]any{"type": "message.delta", "sid": "live-1", "payload": map[string]any{"text": "hi"}}})
			}
		}
	}))
	defer server.Close()

	cfg := config.Defaults()
	cfg.Gateway.URL = "ws" + strings.TrimPrefix(server.URL, "http")
	cfg.Gateway.StatusURL = server.URL + "/api/status"
	cfg.Gateway.ConnectTimeout = "1s"
	cfg.Gateway.RPCTimeout = "1s"
	client := NewClient(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := client.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	result, err := client.Call(ctx, "ping", map[string]any{})
	if err != nil || result["ok"] != true {
		t.Fatalf("ping failed: %v %v", result, err)
	}
	deadline := time.After(time.Second)
	for {
		select {
		case event := <-client.Events():
			if event.Type == "gateway.ready" {
				continue
			}
			if event.Type != "message.delta" || event.SessionID != "live-1" {
				t.Fatalf("unexpected event: %+v", event)
			}
			return
		case <-deadline:
			t.Fatal("message.delta event was not delivered")
		}
	}
}

func TestEventEnvelopeSupportsNestedPayload(t *testing.T) {
	client := &Client{ready: make(chan struct{}), events: make(chan Event, 1), done: make(chan struct{})}
	client.handleEvent(json.RawMessage(`{"type":"tool.start","sid":"live","payload":{"name":"read_file"}}`))
	event := <-client.events
	if event.Type != "tool.start" || event.Payload["name"] != "read_file" {
		t.Fatalf("unexpected event: %+v", event)
	}
}

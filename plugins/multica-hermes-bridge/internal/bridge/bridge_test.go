package bridge

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/WYLINX601/project-context-workflow/plugins/multica-hermes-bridge/internal/acp"
	"github.com/WYLINX601/project-context-workflow/plugins/multica-hermes-bridge/internal/config"
	"github.com/WYLINX601/project-context-workflow/plugins/multica-hermes-bridge/internal/hermes"
	"github.com/WYLINX601/project-context-workflow/plugins/multica-hermes-bridge/internal/state"
)

type fakeGateway struct {
	mu      sync.Mutex
	calls   []string
	params  map[string][]map[string]any
	events  chan hermes.Event
	running bool
	resumed bool
}

func newFakeGateway() *fakeGateway {
	return &fakeGateway{events: make(chan hermes.Event, 16), params: make(map[string][]map[string]any)}
}

func (f *fakeGateway) Connect(context.Context) error { return nil }
func (f *fakeGateway) Close()                        { close(f.events) }
func (f *fakeGateway) Events() <-chan hermes.Event   { return f.events }

func (f *fakeGateway) Call(_ context.Context, method string, params any) (map[string]any, error) {
	f.mu.Lock()
	f.calls = append(f.calls, method)
	if object, ok := params.(map[string]any); ok {
		copy := make(map[string]any, len(object))
		for key, value := range object {
			copy[key] = value
		}
		f.params[method] = append(f.params[method], copy)
	}
	if method == "session.resume" {
		f.resumed = true
	}
	f.mu.Unlock()
	switch method {
	case "session.create":
		return map[string]any{"session_id": "live-1", "stored_session_id": "stored-1", "model": "test:model"}, nil
	case "session.resume":
		return map[string]any{"session_id": "live-2", "stored_session_id": "stored-1"}, nil
	case "session.status":
		return map[string]any{"running": f.running, "cwd": ""}, nil
	case "model.options":
		return map[string]any{"current_model": "test:model", "models": []any{map[string]any{"id": "test:model", "name": "Test Model"}}}, nil
	case "prompt.submit":
		go func() {
			f.events <- hermes.Event{Type: "message.delta", SessionID: "live-2", Payload: map[string]any{"text": "hello"}}
			f.events <- hermes.Event{Type: "tool.start", SessionID: "live-2", Payload: map[string]any{"id": "tool-1", "name": "read_file"}}
			f.events <- hermes.Event{Type: "tool.complete", SessionID: "live-2", Payload: map[string]any{"id": "tool-1", "status": "completed"}}
			f.events <- hermes.Event{Type: "message.complete", SessionID: "live-2", Payload: map[string]any{"text": "hello"}}
		}()
		return map[string]any{"accepted": true}, nil
	case "session.cwd.set", "command.dispatch", "session.interrupt", "approval.respond":
		return map[string]any{}, nil
	default:
		return map[string]any{}, nil
	}
}

func (f *fakeGateway) lastParams(method string) map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	values := f.params[method]
	if len(values) == 0 {
		return nil
	}
	return values[len(values)-1]
}

type testEmitter struct {
	mu            sync.Mutex
	notifications []string
}

func (e *testEmitter) Notify(_ context.Context, method string, _ any) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.notifications = append(e.notifications, method)
	return nil
}
func (e *testEmitter) Request(context.Context, string, any) (json.RawMessage, error) {
	return json.RawMessage(`{"outcome":{"optionId":"allow_once"}}`), nil
}

func testConfig(t *testing.T, cwd string) config.Config {
	t.Helper()
	t.Setenv("MHG_PROFILE", "")
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := osWrite(path, []byte("gateway:\n  url: ws://127.0.0.1:9119/api/ws\nsession:\n  profile: product-solution\nstate_db: "+filepath.Join(filepath.Dir(path), "state.db")+"\n")); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestBridgePreservesStableIDAndMapsEvents(t *testing.T) {
	cwd := t.TempDir()
	cfg := testConfig(t, cwd)
	store, err := state.Open(cfg.StatePath())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	fake := newFakeGateway()
	b := New(cfg, store, fake, nil)
	result, rpcErr := b.Handle(context.Background(), "session/new", json.RawMessage(`{"cwd":"`+cwd+`"}`), &testEmitter{})
	if rpcErr != nil {
		t.Fatalf("session/new failed: %+v", rpcErr)
	}
	mhgID := result.(map[string]any)["sessionId"].(string)
	if len(mhgID) < 5 || mhgID[:4] != "mhg_" {
		t.Fatalf("not a stable MHG ID: %s", mhgID)
	}
	resumeRaw, _ := json.Marshal(map[string]any{"sessionId": mhgID, "cwd": cwd})
	if _, rpcErr := b.Handle(context.Background(), "session/resume", resumeRaw, &testEmitter{}); rpcErr != nil {
		t.Fatalf("session/resume failed: %+v", rpcErr)
	}
	emitter := &testEmitter{}
	promptRaw := json.RawMessage(`{"sessionId":"` + mhgID + `","prompt":[{"type":"text","text":"do it"}]}`)
	if _, rpcErr := b.Handle(context.Background(), "session/prompt", promptRaw, emitter); rpcErr != nil {
		t.Fatalf("session/prompt failed: %+v", rpcErr)
	}
	if len(emitter.notifications) < 3 {
		t.Fatalf("expected text and tool notifications, got %v", emitter.notifications)
	}
	stored, err := store.Get(mhgID)
	if err != nil || stored.HermesStoredID != "stored-1" {
		t.Fatalf("mapping was not persisted: %+v %v", stored, err)
	}
	if fake.resumed == false {
		t.Fatal("resume was not sent to Hermes")
	}
	if profile := fake.lastParams("session.create")["profile"]; profile != "product-solution" {
		t.Fatalf("session.create did not receive profile: %v", profile)
	}
	if profile := fake.lastParams("session.resume")["profile"]; profile != "product-solution" {
		t.Fatalf("session.resume did not receive profile: %v", profile)
	}
	if profile := fake.lastParams("prompt.submit")["profile"]; profile != "product-solution" {
		t.Fatalf("prompt.submit did not receive profile: %v", profile)
	}
	if stored.Profile != "product-solution" {
		t.Fatalf("mapping profile was not persisted: %q", stored.Profile)
	}
}

func TestBridgeRejectsResumeAcrossProfiles(t *testing.T) {
	cwd := t.TempDir()
	cfg := testConfig(t, cwd)
	store, err := state.Open(cfg.StatePath())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Create(state.Session{
		MHGSessionID:    "mhg_other_profile",
		HermesStoredID:  "stored-other",
		GatewayIdentity: cfg.GatewayIdentity(),
		Profile:         "work",
		CWD:             cwd,
	}); err != nil {
		t.Fatal(err)
	}
	b := New(cfg, store, newFakeGateway(), nil)
	resumeRaw, _ := json.Marshal(map[string]any{"sessionId": "mhg_other_profile", "cwd": cwd})
	if _, rpcErr := b.Handle(context.Background(), "session/resume", resumeRaw, &testEmitter{}); rpcErr == nil {
		t.Fatal("expected profile mismatch")
	} else if data, ok := rpcErr.Data.(map[string]any); !ok || data["mhg_code"] != CodeSessionProfileMismatch {
		t.Fatalf("unexpected profile mismatch error: %+v", rpcErr)
	}
}

func TestBridgeRejectsNonTextContent(t *testing.T) {
	cwd := t.TempDir()
	cfg := testConfig(t, cwd)
	store, err := state.Open(cfg.StatePath())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	fake := newFakeGateway()
	b := New(cfg, store, fake, nil)
	result, rpcErr := b.Handle(context.Background(), "session/new", json.RawMessage(`{"cwd":"`+cwd+`"}`), &testEmitter{})
	if rpcErr != nil {
		t.Fatal(rpcErr)
	}
	mhgID := result.(map[string]any)["sessionId"].(string)
	resumeRaw, _ := json.Marshal(map[string]any{"sessionId": mhgID, "cwd": cwd})
	if _, rpcErr := b.Handle(context.Background(), "session/resume", resumeRaw, &testEmitter{}); rpcErr != nil {
		t.Fatal(rpcErr)
	}
	promptRaw, _ := json.Marshal(map[string]any{"sessionId": mhgID, "prompt": []map[string]any{{"type": "image", "data": "..."}}})
	if _, rpcErr := b.Handle(context.Background(), "session/prompt", promptRaw, &testEmitter{}); rpcErr == nil {
		t.Fatal("expected unsupported content error")
	}
}

func osWrite(path string, data []byte) error {
	return os.WriteFile(path, data, 0o600)
}

var _ = acp.JSONRPCInvalidParams
var _ = time.Second

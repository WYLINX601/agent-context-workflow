package bridge

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/linx-workbench/multica-hermes-gateway/internal/acp"
	"github.com/linx-workbench/multica-hermes-gateway/internal/config"
	"github.com/linx-workbench/multica-hermes-gateway/internal/hermes"
	"github.com/linx-workbench/multica-hermes-gateway/internal/locking"
	"github.com/linx-workbench/multica-hermes-gateway/internal/state"
	"github.com/linx-workbench/multica-hermes-gateway/internal/supervisor"
)

const (
	Version     = "0.3.0"
	quietWindow = 250 * time.Millisecond
)

const (
	CodeGatewayUnavailable     = "MHG1001 GATEWAY_UNAVAILABLE"
	CodeGatewayProtocol        = "MHG1002 GATEWAY_PROTOCOL_ERROR"
	CodeGatewayUnsupported     = "MHG1003 UNSUPPORTED_GATEWAY_METHOD"
	CodeSessionMappingNotFound = "MHG2001 SESSION_MAPPING_NOT_FOUND"
	CodeHermesSessionNotFound  = "MHG2002 HERMES_SESSION_NOT_FOUND"
	CodeSessionBusy            = "MHG2003 SESSION_BUSY"
	CodeAmbiguousPreviousTurn  = "MHG2004 AMBIGUOUS_PREVIOUS_TURN"
	CodeSessionProfileMismatch = "MHG2005 SESSION_PROFILE_MISMATCH"
	CodeInvalidCWD             = "MHG3001 INVALID_CWD"
	CodeUnsupportedContent     = "MHG3002 UNSUPPORTED_CONTENT"
	CodeProfileNotFound        = "MHG3004 PROFILE_NOT_FOUND"
	CodeProfileLookupFailed    = "MHG3005 PROFILE_LOOKUP_FAILED"
	CodeApprovalDenied         = "MHG4001 APPROVAL_DENIED"
	CodeClarificationRequired  = "MHG4002 CLARIFICATION_REQUIRED"
	CodeSudoRequired           = "MHG4003 SUDO_REQUIRED"
	CodeSecretRequired         = "MHG4004 SECRET_REQUIRED"
	CodeModelSwitchFailed      = "MHG5001 MODEL_SWITCH_FAILED"
	CodeModelNotFound          = "MHG5002 MODEL_NOT_FOUND"
	CodeInternal               = "MHG9001 INTERNAL_ERROR"
)

type Gateway interface {
	Connect(context.Context) error
	Call(context.Context, string, any) (map[string]any, error)
	ProfileExists(context.Context, string) (bool, error)
	Events() <-chan hermes.Event
	Close()
}

type RuntimeController interface {
	AcquireRuntime(context.Context, supervisor.AcquireParams) (supervisor.RuntimeInfo, error)
	AcquireRuntimeWithRequestID(context.Context, string, supervisor.AcquireParams) (supervisor.RuntimeInfo, error)
	HeartbeatLease(context.Context, string, uint64) error
	PinTurn(context.Context, string, string, uint64) error
	UnpinTurn(context.Context, string, string, uint64) error
	ReleaseRuntime(context.Context, string, uint64) error
}

type runtimeConfigurer interface {
	ConfigureRuntime(string, string)
}

type runtimeTokenConfigurer interface {
	ConfigureTokenRef(string)
}

type logger func(format string, args ...any)

type Bridge struct {
	cfg     config.Config
	store   *state.Store
	gateway Gateway
	runtime RuntimeController
	log     logger

	mu               sync.Mutex
	runtimeAcquireMu sync.Mutex
	sessions         map[string]*runtimeSession
	// gatewayFactory is available for the production Hermes client so a
	// generation change can replace a dead WebSocket without replaying a turn.
	// Test doubles and custom Gateway implementations remain connect-once.
	gatewayFactory   func(config.Config) Gateway
	closed           bool
	runtimeInfo      supervisor.RuntimeInfo
	runtimeRequestID string
	runtimeStale     bool
	runtimeCancel    context.CancelFunc
	runtimeWG        sync.WaitGroup
	requestWG        sync.WaitGroup
	promptWG         sync.WaitGroup
}

type runtimeSession struct {
	mhgID    string
	storedID string
	liveID   string
	cwd      string
	busy     bool
	cancel   context.CancelFunc
}

type sessionNewParams struct {
	CWD   string `json:"cwd"`
	Model string `json:"model"`
}

type sessionResumeParams struct {
	SessionID string `json:"sessionId"`
	CWD       string `json:"cwd"`
}

type sessionModelParams struct {
	SessionID string `json:"sessionId"`
	ModelID   string `json:"modelId"`
	Model     string `json:"model"`
}

type sessionPromptParams struct {
	SessionID string          `json:"sessionId"`
	Prompt    []PromptContent `json:"prompt"`
}

type PromptContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type modelsResponse struct {
	CurrentModel    string        `json:"currentModel,omitempty"`
	CurrentModelID  string        `json:"currentModelId,omitempty"`
	AvailableModels []modelOption `json:"availableModels,omitempty"`
}

type modelOption struct {
	ModelID     string `json:"modelId"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
}

func New(cfg config.Config, store *state.Store, gateway Gateway, logf func(string, ...any)) *Bridge {
	return newBridge(cfg, store, gateway, nil, logf)
}

func NewWithRuntime(cfg config.Config, store *state.Store, gateway Gateway, runtime RuntimeController, logf func(string, ...any)) *Bridge {
	return newBridge(cfg, store, gateway, runtime, logf)
}

func newBridge(cfg config.Config, store *state.Store, gateway Gateway, runtime RuntimeController, logf func(string, ...any)) *Bridge {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	var gatewayFactory func(config.Config) Gateway
	if gateway == nil {
		gateway = hermes.NewClient(cfg)
		gatewayFactory = func(runtimeCfg config.Config) Gateway { return hermes.NewClient(runtimeCfg) }
	} else if _, ok := gateway.(*hermes.Client); ok {
		gatewayFactory = func(runtimeCfg config.Config) Gateway { return hermes.NewClient(runtimeCfg) }
	}
	runtimeRequestID, err := newSessionID()
	if err != nil {
		runtimeRequestID = fmt.Sprintf("runtime-%d", time.Now().UnixNano())
	}
	return &Bridge{
		cfg:              cfg,
		store:            store,
		gateway:          gateway,
		gatewayFactory:   gatewayFactory,
		runtime:          runtime,
		log:              logf,
		sessions:         make(map[string]*runtimeSession),
		runtimeRequestID: runtimeRequestID,
	}
}

func (b *Bridge) withProfile(params map[string]any) map[string]any {
	if profile := b.cfg.Profile(); profile != "" {
		params["profile"] = profile
	}
	return params
}

func (b *Bridge) ensureProfile(ctx context.Context) *acp.RPCError {
	if err := b.ensureRuntime(ctx); err != nil {
		return mapSupervisorError(err)
	}
	profile := b.cfg.Profile()
	if profile == "" {
		return nil
	}
	checkCtx, cancel := context.WithTimeout(ctx, b.cfg.RPCTimeoutDuration())
	defer cancel()
	exists, err := b.gateway.ProfileExists(checkCtx, profile)
	if err != nil {
		return rpcError(acp.JSONRPCInternalError, CodeProfileLookupFailed,
			fmt.Sprintf("could not verify Hermes profile %q: %v", profile, err))
	}
	if !exists {
		return rpcError(acp.JSONRPCInvalidParams, CodeProfileNotFound,
			fmt.Sprintf("Hermes profile %q does not exist", profile))
	}
	return nil
}

func (b *Bridge) ensureRuntime(ctx context.Context) error {
	if b.runtime == nil {
		return nil
	}
	b.runtimeAcquireMu.Lock()
	defer b.runtimeAcquireMu.Unlock()
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return errors.New("adapter is shutting down")
	}
	if b.runtimeInfo.LeaseID != "" && !b.runtimeStale {
		b.mu.Unlock()
		return nil
	}
	hadRuntime := b.runtimeInfo.LeaseID != ""
	wasStale := b.runtimeStale
	b.mu.Unlock()
	request := supervisor.AcquireParams{GatewayIdentity: b.cfg.GatewayIdentity(), Profile: b.cfg.Profile(), Scope: b.cfg.Supervisor.Scope}
	info, err := b.acquireRuntime(ctx, request)
	if err != nil {
		return err
	}
	if hadRuntime && wasStale {
		if err := b.replaceGateway(ctx, info); err != nil {
			if info.LeaseID != b.currentRuntime().LeaseID {
				_ = b.runtime.ReleaseRuntime(context.Background(), info.LeaseID, info.Generation)
			}
			return err
		}
	} else {
		b.configureGateway(b.gateway, info)
	}
	if !hadRuntime {
		connectCtx, cancel := context.WithTimeout(ctx, b.cfg.ConnectTimeoutDuration())
		err = b.gateway.Connect(connectCtx)
		cancel()
		if err != nil {
			_ = b.runtime.ReleaseRuntime(context.Background(), info.LeaseID, info.Generation)
			return err
		}
	}
	b.mu.Lock()
	b.runtimeInfo = info
	b.runtimeStale = false
	b.mu.Unlock()
	if !hadRuntime {
		heartbeatCtx, heartbeatCancel := context.WithCancel(context.Background())
		b.mu.Lock()
		if b.closed {
			b.mu.Unlock()
			heartbeatCancel()
			_ = b.runtime.ReleaseRuntime(context.Background(), info.LeaseID, info.Generation)
			return errors.New("adapter is shutting down")
		}
		b.runtimeCancel = heartbeatCancel
		b.runtimeWG.Add(1)
		b.mu.Unlock()
		go b.heartbeatRuntime(heartbeatCtx, info)
	}
	return nil
}

func (b *Bridge) acquireRuntime(ctx context.Context, params supervisor.AcquireParams) (supervisor.RuntimeInfo, error) {
	return b.runtime.AcquireRuntimeWithRequestID(ctx, b.runtimeRequestID, params)
}

func (b *Bridge) reacquireRuntime(ctx context.Context, previous supervisor.RuntimeInfo) (supervisor.RuntimeInfo, error) {
	b.runtimeAcquireMu.Lock()
	defer b.runtimeAcquireMu.Unlock()
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return supervisor.RuntimeInfo{}, errors.New("adapter is shutting down")
	}
	if b.runtimeInfo.LeaseID != previous.LeaseID || b.runtimeInfo.Generation != previous.Generation {
		info := b.runtimeInfo
		b.mu.Unlock()
		return info, nil
	}
	b.runtimeStale = true
	b.mu.Unlock()
	params := supervisor.AcquireParams{GatewayIdentity: b.cfg.GatewayIdentity(), Profile: b.cfg.Profile(), Scope: b.cfg.Supervisor.Scope}
	info, err := b.acquireRuntime(ctx, params)
	if err != nil {
		return supervisor.RuntimeInfo{}, err
	}
	b.mu.Lock()
	closed := b.closed
	b.mu.Unlock()
	if closed {
		_ = b.runtime.ReleaseRuntime(context.Background(), info.LeaseID, info.Generation)
		return supervisor.RuntimeInfo{}, errors.New("adapter is shutting down")
	}
	if err := b.replaceGateway(ctx, info); err != nil {
		if info.LeaseID != previous.LeaseID {
			_ = b.runtime.ReleaseRuntime(context.Background(), info.LeaseID, info.Generation)
		}
		return supervisor.RuntimeInfo{}, err
	}
	b.mu.Lock()
	b.runtimeInfo = info
	b.runtimeStale = false
	b.mu.Unlock()
	return info, nil
}

func (b *Bridge) currentRuntime() supervisor.RuntimeInfo {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.runtimeInfo
}

func (b *Bridge) configureGateway(gateway Gateway, info supervisor.RuntimeInfo) {
	if configurer, ok := gateway.(runtimeConfigurer); ok {
		configurer.ConfigureRuntime(info.GatewayURL, info.StatusURL)
	}
	if configurer, ok := gateway.(runtimeTokenConfigurer); ok {
		configurer.ConfigureTokenRef(info.TokenRef)
	}
}

func (b *Bridge) replaceGateway(ctx context.Context, info supervisor.RuntimeInfo) error {
	b.mu.Lock()
	for _, session := range b.sessions {
		if session.busy {
			b.mu.Unlock()
			return fmt.Errorf("%s: active turn prevents gateway reconnect", CodeAmbiguousPreviousTurn)
		}
	}
	oldGateway := b.gateway
	factory := b.gatewayFactory
	b.mu.Unlock()
	if factory == nil {
		b.configureGateway(oldGateway, info)
		return nil
	}
	runtimeCfg := b.cfg
	runtimeCfg.Gateway.URL = info.GatewayURL
	runtimeCfg.Gateway.StatusURL = info.StatusURL
	nextGateway := factory(runtimeCfg)
	b.configureGateway(nextGateway, info)
	connectCtx, cancel := context.WithTimeout(ctx, b.cfg.ConnectTimeoutDuration())
	err := nextGateway.Connect(connectCtx)
	cancel()
	if err != nil {
		nextGateway.Close()
		return err
	}
	oldGateway.Close()
	b.mu.Lock()
	b.gateway = nextGateway
	b.mu.Unlock()
	return nil
}

func (b *Bridge) heartbeatRuntime(ctx context.Context, info supervisor.RuntimeInfo) {
	defer b.runtimeWG.Done()
	ticker := time.NewTicker(b.cfg.SupervisorHeartbeatDuration())
	defer ticker.Stop()
	current := info
	for {
		select {
		case <-ticker.C:
			heartbeatCtx, cancel := context.WithTimeout(ctx, b.cfg.RPCTimeoutDuration())
			err := b.runtime.HeartbeatLease(heartbeatCtx, current.LeaseID, current.Generation)
			cancel()
			if err != nil {
				b.log("supervisor lease heartbeat failed: %v", err)
				var coded *supervisor.SupervisorError
				if errors.As(err, &coded) && coded != nil && (coded.Code == supervisor.CodeGenerationChanged || coded.Code == supervisor.CodeLeaseExpired || coded.Code == supervisor.CodeUnavailable || coded.Code == supervisor.CodeRuntimeUnavailable) {
					recoverCtx, recoverCancel := context.WithTimeout(context.Background(), b.cfg.RPCTimeoutDuration())
					if recovered, recoverErr := b.reacquireRuntime(recoverCtx, current); recoverErr == nil && recovered.LeaseID != "" {
						current = recovered
					}
					recoverCancel()
				}
			}
		case <-ctx.Done():
			return
		}
	}
}

func (b *Bridge) Start(ctx context.Context) error {
	if err := b.cfg.Validate(); err != nil {
		return err
	}
	if b.store == nil {
		store, err := state.Open(b.cfg.StatePath())
		if err != nil {
			return fmt.Errorf("%s: %w", CodeInternal, err)
		}
		b.store = store
	}
	if b.runtime == nil {
		if err := hermes.HTTPStatus(ctx, b.cfg); err != nil {
			return err
		}
		if err := b.gateway.Connect(ctx); err != nil {
			return err
		}
		b.log("gateway connected endpoint=%s", b.cfg.GatewayIdentity())
	} else {
		b.log("supervisor lazy runtime mode enabled endpoint=%s", b.cfg.GatewayIdentity())
	}
	return nil
}

func (b *Bridge) Handle(ctx context.Context, method string, params json.RawMessage, emitter acp.Emitter) (any, *acp.RPCError) {
	if !b.beginRequest() {
		return nil, rpcError(acp.JSONRPCInternalError, CodeInternal, "adapter is shutting down")
	}
	defer b.requestWG.Done()
	switch method {
	case "initialize":
		return b.initialize(), nil
	case "session/new":
		result, err := b.sessionNew(ctx, params)
		return result, err
	case "session/resume":
		result, err := b.sessionResume(ctx, params)
		return result, err
	case "session/load":
		result, err := b.sessionResume(ctx, params)
		return result, err
	case "session/set_model":
		result, err := b.setModel(ctx, params)
		return result, err
	case "session/prompt":
		result, err := b.prompt(ctx, params, emitter)
		return result, err
	case "session/cancel":
		return b.cancelPrompt(params), nil
	default:
		return nil, acp.NewRPCError(acp.JSONRPCMethodNotFound, "method not found: "+method, map[string]any{"mhg_code": CodeGatewayUnsupported})
	}
}

func (b *Bridge) initialize() map[string]any {
	return map[string]any{
		"protocolVersion": 1,
		"agentInfo": map[string]any{
			"name":    "multica-hermes-gateway",
			"title":   "Persistent Hermes Gateway Adapter",
			"version": Version,
		},
		"agentCapabilities": map[string]any{
			"loadSession": true,
			"promptCapabilities": map[string]any{
				"image":           false,
				"audio":           false,
				"embeddedContext": false,
			},
			"mcpCapabilities":     map[string]any{"http": false, "sse": false},
			"sessionCapabilities": map[string]any{"resume": true},
		},
		// Intentionally empty: the persistent Hermes profile owns its native MCP.
		"mcpCapabilities": map[string]any{},
		"authMethods":     []any{},
	}
}

func (b *Bridge) sessionNew(ctx context.Context, raw json.RawMessage) (map[string]any, *acp.RPCError) {
	params := sessionNewParams{}
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, rpcError(acp.JSONRPCInvalidParams, CodeInvalidCWD, "invalid session/new params")
	}
	cwd, err := validateCWD(params.CWD)
	if err != nil {
		return nil, rpcError(acp.JSONRPCInvalidParams, CodeInvalidCWD, err.Error())
	}
	if err := b.ensureProfile(ctx); err != nil {
		return nil, err
	}
	req := map[string]any{
		"cwd":                 cwd,
		"source":              b.cfg.Session.Source,
		"close_on_disconnect": false,
	}
	b.withProfile(req)
	if params.Model != "" {
		req["model"] = params.Model
	}
	callCtx, cancel := context.WithTimeout(ctx, b.cfg.RPCTimeoutDuration())
	defer cancel()
	result, err := b.gateway.Call(callCtx, "session.create", req)
	if err != nil {
		return nil, mapGatewayRPCError(err, CodeGatewayUnavailable)
	}
	liveID := firstString(result, "session_id", "sessionId", "live_session_id", "liveSessionId", "live_id", "id")
	storedID := firstString(result, "stored_session_id", "storedSessionId", "hermes_stored_id", "session_key", "sessionKey", "stored_id")
	if liveID == "" || storedID == "" {
		return nil, rpcError(acp.JSONRPCInternalError, CodeGatewayProtocol, "session.create returned no live/stored session identity")
	}
	mhgID, err := newSessionID()
	if err != nil {
		return nil, rpcError(acp.JSONRPCInternalError, CodeInternal, "generate stable session ID")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := b.store.Create(state.Session{
		MHGSessionID: mhgID, HermesStoredID: storedID, GatewayIdentity: b.cfg.GatewayIdentity(),
		Profile: b.cfg.Profile(), CWD: cwd, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		return nil, rpcError(acp.JSONRPCInternalError, CodeInternal, err.Error())
	}
	b.mu.Lock()
	b.sessions[mhgID] = &runtimeSession{mhgID: mhgID, storedID: storedID, liveID: liveID, cwd: cwd}
	b.mu.Unlock()
	models := b.modelDiscovery(ctx, liveID, params.Model, result)
	b.log("session mapped mhg_session_id=%s", mhgID)
	return map[string]any{"sessionId": mhgID, "models": models}, nil
}

func (b *Bridge) sessionResume(ctx context.Context, raw json.RawMessage) (map[string]any, *acp.RPCError) {
	params := sessionResumeParams{}
	if err := json.Unmarshal(raw, &params); err != nil || strings.TrimSpace(params.SessionID) == "" {
		return nil, rpcError(acp.JSONRPCInvalidParams, CodeSessionMappingNotFound, "sessionId is required")
	}
	if err := b.ensureProfile(ctx); err != nil {
		return nil, err
	}
	stored, err := b.store.Get(params.SessionID)
	if err != nil {
		return nil, rpcError(acp.JSONRPCInvalidParams, CodeSessionMappingNotFound, err.Error())
	}
	if stored.Profile != b.cfg.Profile() {
		return nil, rpcError(acp.JSONRPCInvalidParams, CodeSessionProfileMismatch,
			fmt.Sprintf("session belongs to Hermes profile %q, adapter is configured for %q", stored.Profile, b.cfg.Profile()))
	}
	cwd, err := validateCWD(params.CWD)
	if err != nil {
		return nil, rpcError(acp.JSONRPCInvalidParams, CodeInvalidCWD, err.Error())
	}
	callCtx, cancel := context.WithTimeout(ctx, b.cfg.RPCTimeoutDuration())
	defer cancel()
	result, err := b.gateway.Call(callCtx, "session.resume", b.withProfile(map[string]any{
		"session_id":     stored.HermesStoredID,
		"source":         b.cfg.Session.Source,
		"omit_messages":  true,
		"replay_history": false,
	}))
	if err != nil {
		return nil, mapGatewayRPCError(err, CodeHermesSessionNotFound)
	}
	liveID := firstString(result, "session_id", "sessionId", "live_session_id", "liveSessionId", "live_id", "id")
	if liveID == "" {
		return nil, rpcError(acp.JSONRPCInternalError, CodeGatewayProtocol, "session.resume returned no live session ID")
	}
	status, statusErr := b.sessionStatus(ctx, liveID)
	if statusErr != nil {
		return nil, mapGatewayRPCError(statusErr, CodeGatewayProtocol)
	}
	if status.Running || boolValue(result, "running") {
		return nil, rpcError(acp.JSONRPCInvalidParams, CodeAmbiguousPreviousTurn, "Hermes session is still running; prompt was not replayed")
	}
	currentCWD := firstString(result, "cwd", "currentCwd", "current_cwd", "working_directory", "workingDirectory")
	if currentCWD == "" {
		currentCWD = status.CWD
	}
	if currentCWD == "" {
		currentCWD = stored.CWD
	}
	if filepath.Clean(currentCWD) != filepath.Clean(cwd) {
		setCtx, setCancel := context.WithTimeout(ctx, b.cfg.RPCTimeoutDuration())
		_, setErr := b.gateway.Call(setCtx, "session.cwd.set", b.withProfile(map[string]any{"session_id": liveID, "cwd": cwd}))
		setCancel()
		if setErr != nil {
			return nil, mapGatewayRPCError(setErr, CodeInvalidCWD)
		}
	}
	newStored := firstString(result, "stored_session_id", "storedSessionId", "hermes_stored_id", "session_key", "sessionKey", "stored_id")
	if newStored == "" {
		newStored = stored.HermesStoredID
	}
	if err := b.store.UpdateLive(params.SessionID, newStored, cwd); err != nil {
		return nil, rpcError(acp.JSONRPCInternalError, CodeInternal, err.Error())
	}
	b.mu.Lock()
	b.sessions[params.SessionID] = &runtimeSession{mhgID: params.SessionID, storedID: newStored, liveID: liveID, cwd: cwd}
	b.mu.Unlock()
	b.log("session resumed mhg_session_id=%s", params.SessionID)
	return map[string]any{"sessionId": params.SessionID, "models": b.modelDiscovery(ctx, liveID, "", result)}, nil
}

func (b *Bridge) setModel(ctx context.Context, raw json.RawMessage) (map[string]any, *acp.RPCError) {
	params := sessionModelParams{}
	if err := json.Unmarshal(raw, &params); err != nil || params.SessionID == "" {
		return nil, rpcError(acp.JSONRPCInvalidParams, CodeSessionMappingNotFound, "sessionId is required")
	}
	model := strings.TrimSpace(params.ModelID)
	if model == "" {
		model = strings.TrimSpace(params.Model)
	}
	if model == "" {
		return nil, rpcError(acp.JSONRPCInvalidParams, CodeModelNotFound, "modelId is required")
	}
	runtimeSession, err := b.lookupRuntime(params.SessionID)
	if err != nil {
		return nil, rpcError(acp.JSONRPCInvalidParams, CodeSessionMappingNotFound, err.Error())
	}
	callCtx, cancel := context.WithTimeout(ctx, b.cfg.RPCTimeoutDuration())
	defer cancel()
	if _, err := b.gateway.Call(callCtx, "command.dispatch", b.withProfile(map[string]any{
		"session_id": runtimeSession.liveID,
		"command":    "/model " + model,
	})); err != nil {
		return nil, mapGatewayRPCError(err, CodeModelSwitchFailed)
	}
	return map[string]any{"modelId": model}, nil
}

func (b *Bridge) prompt(ctx context.Context, raw json.RawMessage, emitter acp.Emitter) (map[string]any, *acp.RPCError) {
	if !b.beginPrompt() {
		return nil, rpcError(acp.JSONRPCInternalError, CodeInternal, "adapter is shutting down")
	}
	defer b.promptWG.Done()
	params := sessionPromptParams{}
	if err := json.Unmarshal(raw, &params); err != nil || params.SessionID == "" {
		return nil, rpcError(acp.JSONRPCInvalidParams, CodeSessionMappingNotFound, "sessionId is required")
	}
	text, err := textPrompt(params.Prompt)
	if err != nil {
		return nil, rpcError(acp.JSONRPCInvalidParams, CodeUnsupportedContent, err.Error())
	}
	if b.runtime != nil {
		if err := b.ensureRuntime(ctx); err != nil {
			return nil, mapSupervisorError(err)
		}
	}
	runtimeSession, err := b.lookupRuntime(params.SessionID)
	if err != nil {
		return nil, rpcError(acp.JSONRPCInvalidParams, CodeSessionMappingNotFound, err.Error())
	}
	if err := b.markBusy(params.SessionID, true); err != nil {
		return nil, rpcError(acp.JSONRPCInvalidParams, CodeSessionBusy, err.Error())
	}
	promptCtx, promptCancel := context.WithCancel(ctx)
	b.setCancel(params.SessionID, promptCancel)
	defer func() {
		promptCancel()
		b.setCancel(params.SessionID, nil)
		_ = b.markBusy(params.SessionID, false)
	}()
	b.mu.Lock()
	runtimeInfo := b.runtimeInfo
	b.mu.Unlock()
	if b.runtime != nil && runtimeInfo.LeaseID != "" {
		pinCtx, pinCancel := context.WithTimeout(ctx, b.cfg.RPCTimeoutDuration())
		if err := b.runtime.PinTurn(pinCtx, runtimeInfo.LeaseID, params.SessionID, runtimeInfo.Generation); err != nil {
			pinCancel()
			return nil, rpcError(acp.JSONRPCInternalError, supervisor.CodeUnavailable, err.Error())
		}
		pinCancel()
		defer func() {
			unpinCtx, unpinCancel := context.WithTimeout(context.Background(), b.cfg.RPCTimeoutDuration())
			_ = b.runtime.UnpinTurn(unpinCtx, runtimeInfo.LeaseID, params.SessionID, runtimeInfo.Generation)
			unpinCancel()
		}()
	}
	lock, err := locking.New(b.cfg.StatePath(), b.cfg.GatewayLockKey())
	if err != nil {
		return nil, rpcError(acp.JSONRPCInternalError, CodeInternal, err.Error())
	}
	lockCtx, lockCancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer lockCancel()
	if err := lock.TryAcquire(lockCtx); err != nil {
		if errors.Is(err, locking.ErrBusy) {
			return nil, rpcError(acp.JSONRPCInvalidParams, CodeSessionBusy, err.Error())
		}
		return nil, rpcError(acp.JSONRPCInternalError, CodeInternal, err.Error())
	}
	defer lock.Release()

	status, statusErr := b.sessionStatus(promptCtx, runtimeSession.liveID)
	if statusErr == nil && status.Running {
		return nil, rpcError(acp.JSONRPCInvalidParams, CodeAmbiguousPreviousTurn, "Hermes session is still running; prompt was not replayed")
	}
	submitCtx, submitCancel := context.WithTimeout(promptCtx, b.cfg.RPCTimeoutDuration())
	_, submitErr := b.gateway.Call(submitCtx, "prompt.submit", b.withProfile(map[string]any{"session_id": runtimeSession.liveID, "text": text}))
	submitTimedOut := submitCtx.Err() != nil
	submitCancel()
	if submitErr != nil {
		if errors.Is(promptCtx.Err(), context.Canceled) {
			return map[string]any{"stopReason": "cancelled"}, nil
		}
		if submitTimedOut {
			return nil, rpcError(acp.JSONRPCInvalidParams, CodeAmbiguousPreviousTurn, "prompt.submit outcome is unknown; automatic replay is disabled")
		}
		return nil, mapGatewayRPCError(submitErr, CodeGatewayProtocol)
	}
	b.log("prompt started mhg_session_id=%s", params.SessionID)
	if err := b.waitTurn(promptCtx, runtimeSession, params.SessionID, emitter); err != nil {
		if errors.Is(promptCtx.Err(), context.Canceled) {
			return map[string]any{"stopReason": "cancelled"}, nil
		}
		return nil, err
	}
	b.log("prompt settled mhg_session_id=%s", params.SessionID)
	return map[string]any{"stopReason": "end_turn"}, nil
}

func (b *Bridge) waitTurn(ctx context.Context, session *runtimeSession, mhgID string, emitter acp.Emitter) *acp.RPCError {
	turnCtx, cancel := context.WithTimeout(ctx, b.cfg.TurnTimeoutDuration())
	defer cancel()
	completeSeen := false
	emittedText := ""
	quietUntil := time.Time{}
	for {
		if completeSeen && !quietUntil.IsZero() {
			timer := time.NewTimer(time.Until(quietUntil))
			select {
			case <-timer.C:
			case <-turnCtx.Done():
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				return rpcError(acp.JSONRPCInvalidParams, CodeAmbiguousPreviousTurn, "turn did not settle; automatic replay is disabled")
			case event, ok := <-b.gateway.Events():
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				if !ok {
					return rpcError(acp.JSONRPCInternalError, CodeGatewayUnavailable, "Hermes Gateway disconnected during turn")
				}
				if err := b.handleEvent(turnCtx, event, session, mhgID, emitter, &completeSeen, &emittedText, &quietUntil); err != nil {
					return err
				}
				continue
			}
			status, err := b.sessionStatus(turnCtx, session.liveID)
			if err != nil {
				return mapGatewayRPCError(err, CodeGatewayProtocol)
			}
			if !status.Running && time.Now().After(quietUntil) {
				if completeSeen {
					return nil
				}
			}
			quietUntil = time.Now().Add(quietWindow)
			continue
		}
		select {
		case <-turnCtx.Done():
			return rpcError(acp.JSONRPCInvalidParams, CodeAmbiguousPreviousTurn, "turn did not settle; automatic replay is disabled")
		case event, ok := <-b.gateway.Events():
			if !ok {
				return rpcError(acp.JSONRPCInternalError, CodeGatewayUnavailable, "Hermes Gateway disconnected during turn")
			}
			if err := b.handleEvent(turnCtx, event, session, mhgID, emitter, &completeSeen, &emittedText, &quietUntil); err != nil {
				return err
			}
		}
	}
}

func (b *Bridge) handleEvent(ctx context.Context, event hermes.Event, session *runtimeSession, mhgID string, emitter acp.Emitter, completeSeen *bool, emittedText *string, quietUntil *time.Time) *acp.RPCError {
	if event.SessionID != "" && event.SessionID != session.liveID {
		return nil
	}
	*quietUntil = time.Now().Add(quietWindow)
	switch event.Type {
	case "message.delta", "message.chunk":
		text := eventText(event.Payload)
		if text != "" {
			*emittedText += text
			if err := emitter.Notify(ctx, "session/update", map[string]any{
				"sessionId": mhgID,
				"update": map[string]any{
					"sessionUpdate": "agent_message_chunk",
					"content":       map[string]any{"type": "text", "text": text},
				},
			}); err != nil {
				return rpcError(acp.JSONRPCInternalError, CodeGatewayUnavailable, "write ACP message delta failed")
			}
		}
	case "message.complete":
		if status := strings.ToLower(firstString(event.Payload, "status", "state")); status == "failed" || status == "error" || firstString(event.Payload, "error") != "" {
			return rpcError(acp.JSONRPCInternalError, CodeGatewayProtocol, firstString(event.Payload, "error", "message", "text"))
		}
		*completeSeen = true
		finalText := eventText(event.Payload)
		if finalText != "" {
			missing := finalText
			if strings.HasPrefix(finalText, *emittedText) {
				missing = strings.TrimPrefix(finalText, *emittedText)
			}
			if missing != "" {
				*emittedText += missing
				if err := emitter.Notify(ctx, "session/update", map[string]any{
					"sessionId": mhgID,
					"update": map[string]any{
						"sessionUpdate": "agent_message_chunk",
						"content":       map[string]any{"type": "text", "text": missing},
					},
				}); err != nil {
					return rpcError(acp.JSONRPCInternalError, CodeGatewayUnavailable, "write ACP completion text failed")
				}
			}
		}
	case "tool.start":
		toolID := firstString(event.Payload, "tool_call_id", "call_id", "id")
		if toolID == "" {
			toolID = "tool-unknown"
		}
		title := firstString(event.Payload, "title", "name", "tool")
		if title == "" {
			title = "Hermes tool call"
		}
		if err := emitter.Notify(ctx, "session/update", map[string]any{
			"sessionId": mhgID,
			"update": map[string]any{
				"sessionUpdate": "tool_call",
				"toolCallId":    toolID,
				"title":         title,
				"status":        "pending",
			},
		}); err != nil {
			return rpcError(acp.JSONRPCInternalError, CodeGatewayUnavailable, "write ACP tool event failed")
		}
	case "tool.progress":
		if err := emitter.Notify(ctx, "session/update", toolUpdate(mhgID, event.Payload, "in_progress")); err != nil {
			return rpcError(acp.JSONRPCInternalError, CodeGatewayUnavailable, "write ACP tool progress failed")
		}
	case "tool.complete":
		status := "completed"
		if strings.EqualFold(firstString(event.Payload, "status", "result"), "failed") || boolValue(event.Payload, "failed") {
			status = "failed"
		}
		if err := emitter.Notify(ctx, "session/update", toolUpdate(mhgID, event.Payload, status)); err != nil {
			return rpcError(acp.JSONRPCInternalError, CodeGatewayUnavailable, "write ACP tool completion failed")
		}
	case "approval.request":
		return b.handleApproval(ctx, event, session, mhgID, emitter)
	case "clarify.request":
		b.interrupt(session.liveID)
		return rpcError(acp.JSONRPCInvalidParams, CodeClarificationRequired, "Hermes requested interactive clarification; MHG v0.1 cannot surface clarification")
	case "sudo.request":
		b.interrupt(session.liveID)
		return rpcError(acp.JSONRPCInvalidParams, CodeSudoRequired, "Hermes requested sudo; MHG v0.1 fails closed")
	case "secret.request":
		b.interrupt(session.liveID)
		return rpcError(acp.JSONRPCInvalidParams, CodeSecretRequired, "Hermes requested a secret; MHG never forwards credentials")
	case "error":
		return rpcError(acp.JSONRPCInternalError, CodeGatewayProtocol, firstString(event.Payload, "message", "error", "text"))
	}
	return nil
}

func (b *Bridge) handleApproval(ctx context.Context, event hermes.Event, session *runtimeSession, mhgID string, emitter acp.Emitter) *acp.RPCError {
	requestID := firstString(event.Payload, "request_id", "requestId", "id")
	if requestID == "" {
		return rpcError(acp.JSONRPCInternalError, CodeGatewayProtocol, "approval.request has no request ID")
	}
	if b.cfg.Permissions.Mode == "deny" {
		if err := b.respondApproval(ctx, session.liveID, requestID, "deny"); err != nil {
			return mapGatewayRPCError(err, CodeApprovalDenied)
		}
		return rpcError(acp.JSONRPCInvalidParams, CodeApprovalDenied, "approval denied by permissions.mode=deny")
	}
	requestCtx, cancel := context.WithTimeout(ctx, b.cfg.RPCTimeoutDuration())
	defer cancel()
	toolTitle := firstString(event.Payload, "title", "name", "command")
	if toolTitle == "" {
		toolTitle = "Hermes tool call requires permission"
	}
	response, err := emitter.Request(requestCtx, "session/request_permission", map[string]any{
		"sessionId": mhgID,
		"toolCall": map[string]any{
			"toolCallId": firstString(event.Payload, "tool_call_id", "call_id"),
			"title":      toolTitle,
		},
		"options": permissionOptions(event.Payload),
	})
	if err != nil {
		_ = b.respondApproval(context.Background(), session.liveID, requestID, "deny")
		return rpcError(acp.JSONRPCInvalidParams, CodeApprovalDenied, "Multica did not provide an approval decision")
	}
	choice := approvalChoice(response)
	if choice == "" {
		choice = "deny"
	}
	if err := b.respondApproval(ctx, session.liveID, requestID, choice); err != nil {
		return mapGatewayRPCError(err, CodeApprovalDenied)
	}
	if choice == "deny" {
		return rpcError(acp.JSONRPCInvalidParams, CodeApprovalDenied, "approval denied")
	}
	return nil
}

func (b *Bridge) respondApproval(ctx context.Context, liveID, requestID, choice string) error {
	callCtx, cancel := context.WithTimeout(ctx, b.cfg.RPCTimeoutDuration())
	defer cancel()
	_, err := b.gateway.Call(callCtx, "approval.respond", b.withProfile(map[string]any{
		"session_id": liveID,
		"request_id": requestID,
		"choice":     choice,
	}))
	return err
}

func (b *Bridge) sessionStatus(ctx context.Context, liveID string) (statusInfo, error) {
	callCtx, cancel := context.WithTimeout(ctx, b.cfg.RPCTimeoutDuration())
	defer cancel()
	result, err := b.gateway.Call(callCtx, "session.status", b.withProfile(map[string]any{"session_id": liveID}))
	if err != nil {
		return statusInfo{}, err
	}
	status := strings.ToLower(firstString(result, "status", "state"))
	running := boolValue(result, "running") || status == "running" || status == "busy" || status == "streaming" || status == "queued"
	return statusInfo{Running: running, CWD: firstString(result, "cwd", "currentCwd", "current_cwd", "working_directory", "workingDirectory")}, nil
}

type statusInfo struct {
	Running bool
	CWD     string
}

func (b *Bridge) modelDiscovery(ctx context.Context, liveID, requested string, created map[string]any) modelsResponse {
	current := requested
	if current == "" {
		current = firstString(created, "current_model", "currentModel", "current_model_id", "currentModelId", "model_id", "model")
	}
	modelCtx, cancel := context.WithTimeout(ctx, b.cfg.RPCTimeoutDuration())
	defer cancel()
	result, err := b.gateway.Call(modelCtx, "model.options", b.withProfile(map[string]any{"session_id": liveID}))
	if err != nil {
		result, err = b.gateway.Call(modelCtx, "model.options", b.withProfile(map[string]any{}))
	}
	if err == nil {
		if discovered := firstString(result, "current_model", "currentModel", "current_model_id", "currentModelId", "current", "model_id", "model"); discovered != "" {
			current = discovered
		}
	}
	response := modelsResponse{CurrentModel: current, CurrentModelID: current}
	options := anyList(result, "models", "options", "available_models", "availableModels")
	for _, option := range options {
		id := firstString(option, "model_id", "modelId", "id", "name")
		if id == "" {
			continue
		}
		response.AvailableModels = append(response.AvailableModels, modelOption{ModelID: id, Name: firstString(option, "name", "title"), Description: firstString(option, "description")})
	}
	return response
}

func (b *Bridge) lookupRuntime(mhgID string) (*runtimeSession, error) {
	b.mu.Lock()
	runtimeSession := b.sessions[mhgID]
	b.mu.Unlock()
	if runtimeSession != nil && runtimeSession.liveID != "" {
		return runtimeSession, nil
	}
	return nil, fmt.Errorf("%s: call session/resume before session/prompt", CodeSessionMappingNotFound)
}

func (b *Bridge) markBusy(mhgID string, busy bool) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	runtimeSession := b.sessions[mhgID]
	if runtimeSession == nil {
		return fmt.Errorf("session mapping not found: %s", mhgID)
	}
	if busy && runtimeSession.busy {
		return fmt.Errorf("Hermes session already has an active turn")
	}
	runtimeSession.busy = busy
	return nil
}

func (b *Bridge) setCancel(mhgID string, cancel context.CancelFunc) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if runtimeSession := b.sessions[mhgID]; runtimeSession != nil {
		runtimeSession.cancel = cancel
	}
}

func (b *Bridge) cancelPrompt(raw json.RawMessage) map[string]any {
	var params struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.Unmarshal(raw, &params); err != nil || params.SessionID == "" {
		return map[string]any{"cancelled": false}
	}
	b.mu.Lock()
	runtimeSession := b.sessions[params.SessionID]
	var cancel context.CancelFunc
	var liveID string
	if runtimeSession != nil {
		cancel = runtimeSession.cancel
		liveID = runtimeSession.liveID
	}
	b.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if liveID != "" {
		b.interrupt(liveID)
	}
	return map[string]any{"cancelled": cancel != nil}
}

func (b *Bridge) interrupt(liveID string) {
	if liveID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, _ = b.gateway.Call(ctx, "session.interrupt", b.withProfile(map[string]any{"session_id": liveID}))
}

func (b *Bridge) Shutdown(ctx context.Context) {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	b.closed = true
	type activeSession struct {
		liveID string
		cancel context.CancelFunc
	}
	active := make([]activeSession, 0)
	for _, session := range b.sessions {
		if session.busy {
			active = append(active, activeSession{liveID: session.liveID, cancel: session.cancel})
		}
	}
	b.mu.Unlock()
	b.mu.Lock()
	runtimeCancel := b.runtimeCancel
	b.runtimeCancel = nil
	b.mu.Unlock()
	if runtimeCancel != nil {
		runtimeCancel()
	}
	b.runtimeWG.Wait()
	var wait sync.WaitGroup
	for _, session := range active {
		if session.cancel != nil {
			session.cancel()
		}
		liveID := session.liveID
		wait.Add(1)
		go func() {
			defer wait.Done()
			callCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			defer cancel()
			_, _ = b.gateway.Call(callCtx, "session.interrupt", b.withProfile(map[string]any{"session_id": liveID}))
		}()
	}
	wait.Wait()
	// ACP Server invokes Shutdown while request handlers may still be draining.
	// Do not release the lease or close the Gateway until every prompt has run
	// its UnpinTurn/markBusy cleanup. If a handler ignores cancellation, close
	// the Gateway once to force its blocking I/O to return, then still wait for
	// the cleanup boundary.
	promptDone := make(chan struct{})
	gatewayClosed := false
	go func() {
		b.requestWG.Wait()
		b.promptWG.Wait()
		close(promptDone)
	}()
	select {
	case <-promptDone:
	case <-ctx.Done():
		b.log("prompt shutdown exceeded grace period; closing Gateway to unblock active handlers")
		b.gateway.Close()
		gatewayClosed = true
		<-promptDone
	}
	b.mu.Lock()
	runtimeInfo := b.runtimeInfo
	b.runtimeInfo = supervisor.RuntimeInfo{}
	b.mu.Unlock()
	if b.runtime != nil && runtimeInfo.LeaseID != "" {
		releaseCtx, releaseCancel := context.WithTimeout(context.Background(), b.cfg.RPCTimeoutDuration())
		_ = b.runtime.ReleaseRuntime(releaseCtx, runtimeInfo.LeaseID, runtimeInfo.Generation)
		releaseCancel()
	}
	if !gatewayClosed {
		b.gateway.Close()
	}
	if b.store != nil {
		_ = b.store.Close()
	}
}

func (b *Bridge) beginRequest() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return false
	}
	b.requestWG.Add(1)
	return true
}

// beginPrompt closes the admission race between ACP dispatch and Shutdown:
// once closed is set, no new prompt can increment promptWG after Shutdown has
// started waiting on it.
func (b *Bridge) beginPrompt() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return false
	}
	b.promptWG.Add(1)
	return true
}

func validateCWD(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", fmt.Errorf("%s: cwd is required", CodeInvalidCWD)
	}
	cwd, err := filepath.Abs(filepath.Clean(raw))
	if err != nil {
		return "", fmt.Errorf("%s: invalid cwd: %w", CodeInvalidCWD, err)
	}
	info, err := os.Stat(cwd)
	if err != nil {
		return "", fmt.Errorf("%s: cwd does not exist: %w", CodeInvalidCWD, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s: cwd is not a directory", CodeInvalidCWD)
	}
	handle, err := os.Open(cwd)
	if err != nil {
		return "", fmt.Errorf("%s: cwd is not readable: %w", CodeInvalidCWD, err)
	}
	_ = handle.Close()
	return cwd, nil
}

func textPrompt(content []PromptContent) (string, error) {
	if len(content) == 0 {
		return "", errors.New("prompt must contain at least one text block")
	}
	var parts []string
	for _, block := range content {
		if block.Type != "text" || block.Text == "" {
			return "", fmt.Errorf("only non-empty text prompt blocks are supported")
		}
		parts = append(parts, block.Text)
	}
	return strings.Join(parts, "\n"), nil
}

func newSessionID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return "mhg_" + hex.EncodeToString(bytes), nil
}

func rpcError(code int, mhgCode, message string) *acp.RPCError {
	return acp.NewRPCError(code, message, map[string]any{"mhg_code": mhgCode})
}

func mapGatewayRPCError(err error, fallback string) *acp.RPCError {
	if err == nil {
		return rpcError(acp.JSONRPCInternalError, fallback, fallback)
	}
	if strings.Contains(err.Error(), "method not found") {
		return rpcError(acp.JSONRPCMethodNotFound, CodeGatewayUnsupported, err.Error())
	}
	return rpcError(acp.JSONRPCInternalError, fallback, err.Error())
}

func mapSupervisorError(err error) *acp.RPCError {
	var coded *supervisor.SupervisorError
	if errors.As(err, &coded) {
		switch coded.Code {
		case supervisor.CodeProfileNotFound:
			return rpcError(acp.JSONRPCInvalidParams, CodeProfileNotFound, coded.Message)
		case supervisor.CodeProfileLookup:
			return rpcError(acp.JSONRPCInternalError, CodeProfileLookupFailed, coded.Message)
		default:
			return rpcError(acp.JSONRPCInternalError, coded.Code, coded.Message)
		}
	}
	return rpcError(acp.JSONRPCInternalError, supervisor.CodeUnavailable, err.Error())
}

func firstString(value map[string]any, keys ...string) string {
	for _, key := range keys {
		if raw, ok := value[key]; ok {
			if text, ok := raw.(string); ok && strings.TrimSpace(text) != "" {
				return text
			}
			if nested, ok := raw.(map[string]any); ok {
				if text := firstString(nested, "id", "value", "name"); text != "" {
					return text
				}
			}
		}
	}
	for _, raw := range value {
		if nested, ok := raw.(map[string]any); ok {
			if text := firstString(nested, keys...); text != "" {
				return text
			}
		}
	}
	return ""
}

func boolValue(value map[string]any, keys ...string) bool {
	for _, key := range keys {
		if raw, ok := value[key]; ok {
			switch typed := raw.(type) {
			case bool:
				return typed
			case string:
				return strings.EqualFold(typed, "true") || strings.EqualFold(typed, "running")
			}
		}
	}
	return false
}

func anyList(value map[string]any, keys ...string) []map[string]any {
	for _, key := range keys {
		if raw, ok := value[key].([]any); ok {
			result := make([]map[string]any, 0, len(raw))
			for _, item := range raw {
				if object, ok := item.(map[string]any); ok {
					result = append(result, object)
				}
			}
			return result
		}
	}
	return nil
}

func eventText(payload map[string]any) string {
	for _, key := range []string{"text", "delta", "content", "message"} {
		switch value := payload[key].(type) {
		case string:
			return value
		case map[string]any:
			if text := firstString(value, "text", "value"); text != "" {
				return text
			}
		}
	}
	return ""
}

func toolUpdate(mhgID string, payload map[string]any, status string) map[string]any {
	toolID := firstString(payload, "tool_call_id", "call_id", "id")
	if toolID == "" {
		toolID = "tool-unknown"
	}
	update := map[string]any{
		"sessionUpdate": "tool_call_update",
		"toolCallId":    toolID,
		"status":        status,
	}
	if content := firstString(payload, "text", "output", "result"); content != "" {
		update["content"] = []any{map[string]any{
			"type":    "content",
			"content": map[string]any{"type": "text", "text": content},
		}}
	}
	return map[string]any{
		"sessionId": mhgID,
		"update":    update,
	}
}

func permissionOptions(payload map[string]any) []map[string]any {
	if raw, ok := payload["options"].([]any); ok {
		result := make([]map[string]any, 0, len(raw))
		for _, item := range raw {
			if object, ok := item.(map[string]any); ok {
				optionID := firstString(object, "optionId", "option_id", "id", "name")
				name := firstString(object, "name", "title", "label", "id")
				if optionID == "" {
					continue
				}
				result = append(result, map[string]any{"optionId": optionID, "name": name})
			}
		}
		if len(result) > 0 {
			return result
		}
	}
	return []map[string]any{
		{"optionId": "allow_once", "name": "Allow once"},
		{"optionId": "allow_session", "name": "Allow for session"},
		{"optionId": "deny", "name": "Deny"},
	}
}

func approvalChoice(raw json.RawMessage) string {
	var value map[string]any
	if json.Unmarshal(raw, &value) != nil {
		return "deny"
	}
	if outcome, ok := value["outcome"].(map[string]any); ok {
		if choice := firstString(outcome, "optionId", "option_id", "choice", "outcome"); choice != "" {
			return normalizeChoice(choice)
		}
	}
	return normalizeChoice(firstString(value, "optionId", "option_id", "choice", "outcome"))
}

func normalizeChoice(choice string) string {
	choice = strings.ToLower(strings.TrimSpace(choice))
	switch choice {
	case "allow_once", "allow-once", "once":
		return "once"
	case "allow_session", "allow-session", "session":
		return "session"
	case "allow_always", "allow-always", "always":
		return "always"
	case "deny", "denied", "reject", "":
		return "deny"
	default:
		return "deny"
	}
}

var _ acp.Handler = (*Bridge)(nil)

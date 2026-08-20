package hermes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/linx-workbench/multica-hermes-gateway/internal/config"
	"github.com/linx-workbench/multica-hermes-gateway/internal/protocol"
)

const (
	MaxMessageSize = 16 * 1024 * 1024
)

type RPCError struct {
	Code    int
	Message string
	Data    json.RawMessage
}

func (e *RPCError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("Hermes RPC error %d: %s", e.Code, e.Message)
}

type Event struct {
	Type      string
	SessionID string
	Payload   map[string]any
}

type GatewayTopology struct {
	Profile        string   `json:"profile"`
	ServedProfiles []string `json:"served_profiles"`
}

type ProfileTopology struct {
	Profiles    []ProfileInfo     `json:"profiles"`
	GatewayMode string            `json:"gateway_mode"`
	Gateways    []GatewayTopology `json:"gateways"`
}

type ProfileInfo struct {
	Name string `json:"name"`
}

type Client struct {
	config               *config.Config
	baseGatewayToken     string
	baseGatewayTokenFile string
	conn                 *websocket.Conn

	writeMu   sync.Mutex
	pendingMu sync.Mutex
	pending   map[string]chan protocol.Message
	nextID    atomic.Uint64
	events    chan Event
	ready     chan struct{}
	readyOnce sync.Once
	done      chan struct{}
	closeOnce sync.Once
	errMu     sync.Mutex
	err       error
}

func NewClient(cfg config.Config) *Client {
	return &Client{
		config:               &cfg,
		baseGatewayToken:     cfg.Gateway.Token,
		baseGatewayTokenFile: cfg.Gateway.TokenFile,
		pending:              make(map[string]chan protocol.Message),
		events:               make(chan Event, 128),
		ready:                make(chan struct{}),
		done:                 make(chan struct{}),
	}
}

func (c *Client) ConfigureRuntime(gatewayURL, statusURL string) {
	if strings.TrimSpace(gatewayURL) != "" {
		c.config.Gateway.URL = gatewayURL
	}
	if strings.TrimSpace(statusURL) != "" {
		c.config.Gateway.StatusURL = statusURL
	}
}

// ConfigureTokenRef switches the client to the opaque token reference issued
// by Supervisor. The token value never crosses the Supervisor IPC boundary.
// local-config restores the adapter's original credential source for adopted
// external Gateways.
func (c *Client) ConfigureTokenRef(tokenRef string) {
	if strings.TrimSpace(tokenRef) == "" || strings.TrimSpace(tokenRef) == "local-config" {
		c.config.Gateway.Token = c.baseGatewayToken
		c.config.Gateway.TokenFile = c.baseGatewayTokenFile
		return
	}
	c.config.Gateway.Token = ""
	c.config.Gateway.TokenFile = tokenRef
}

func (c *Client) Connect(ctx context.Context) error {
	dialer := websocket.Dialer{HandshakeTimeout: c.config.ConnectTimeoutDuration()}
	conn, response, err := dialer.DialContext(ctx, c.config.WebSocketURL(), http.Header{})
	if err != nil {
		if response != nil {
			return fmt.Errorf("MHG1001 GATEWAY_UNAVAILABLE: WebSocket HTTP %s", response.Status)
		}
		return fmt.Errorf("MHG1001 GATEWAY_UNAVAILABLE: %w", err)
	}
	conn.SetReadLimit(MaxMessageSize)
	c.conn = conn
	go c.readLoop()
	select {
	case <-c.ready:
		return nil
	case <-ctx.Done():
		c.Close()
		return fmt.Errorf("MHG1001 GATEWAY_UNAVAILABLE: waiting for gateway.ready: %w", ctx.Err())
	case <-c.done:
		return fmt.Errorf("MHG1001 GATEWAY_UNAVAILABLE: %w", c.Err())
	}
}

func (c *Client) Events() <-chan Event {
	return c.events
}

func (c *Client) Call(ctx context.Context, method string, params any) (map[string]any, error) {
	if c.conn == nil {
		return nil, errors.New("Hermes Gateway is not connected")
	}
	id := c.nextID.Add(1)
	message, err := protocol.NewRequest(id, method, params)
	if err != nil {
		return nil, err
	}
	key := strconv.FormatUint(id, 10)
	response := make(chan protocol.Message, 1)
	c.pendingMu.Lock()
	c.pending[key] = response
	c.pendingMu.Unlock()
	defer func() {
		c.pendingMu.Lock()
		delete(c.pending, key)
		c.pendingMu.Unlock()
	}()
	if err := c.write(ctx, message); err != nil {
		return nil, err
	}
	select {
	case reply, ok := <-response:
		if !ok {
			return nil, fmt.Errorf("MHG1001 GATEWAY_UNAVAILABLE: %w", c.Err())
		}
		if reply.Error != nil {
			return nil, &RPCError{Code: reply.Error.Code, Message: reply.Error.Message, Data: reply.Error.Data}
		}
		if len(reply.Result) == 0 {
			return map[string]any{}, nil
		}
		var result map[string]any
		if err := json.Unmarshal(reply.Result, &result); err != nil {
			return nil, fmt.Errorf("MHG1002 GATEWAY_PROTOCOL_ERROR: decode %s result: %w", method, err)
		}
		if result == nil {
			result = map[string]any{}
		}
		return result, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("Hermes %s timed out: %w", method, ctx.Err())
	case <-c.done:
		return nil, fmt.Errorf("MHG1001 GATEWAY_UNAVAILABLE: %w", c.Err())
	}
}

func (c *Client) Close() {
	c.closeOnce.Do(func() {
		if c.conn != nil {
			_ = c.conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(500*time.Millisecond))
			_ = c.conn.Close()
		}
		close(c.done)
		c.pendingMu.Lock()
		for key, channel := range c.pending {
			select {
			case channel <- protocol.Message{Error: &protocol.Error{Code: -32001, Message: "Hermes Gateway connection closed"}}:
			default:
			}
			delete(c.pending, key)
		}
		c.pendingMu.Unlock()
	})
}

func (c *Client) Err() error {
	c.errMu.Lock()
	defer c.errMu.Unlock()
	if c.err == nil {
		return errors.New("Hermes Gateway connection closed")
	}
	return c.err
}

func (c *Client) readLoop() {
	defer func() {
		if c.conn != nil {
			_ = c.conn.Close()
		}
		c.Close()
		close(c.events)
	}()
	for {
		_, raw, err := c.conn.ReadMessage()
		if err != nil {
			c.errMu.Lock()
			c.err = err
			c.errMu.Unlock()
			return
		}
		var message protocol.Message
		if err := json.Unmarshal(raw, &message); err != nil {
			c.errMu.Lock()
			c.err = fmt.Errorf("MHG1002 GATEWAY_PROTOCOL_ERROR: invalid JSON: %w", err)
			c.errMu.Unlock()
			continue
		}
		if message.Method == "event" {
			c.handleEvent(message.Params)
			continue
		}
		if message.Method != "" && len(message.ID) == 0 {
			// Hermes versions in the wild have emitted typed notifications as
			// methods as well as event envelopes. Normalize both forms.
			c.handleTypedEvent(message.Method, message.Params)
			continue
		}
		if len(message.ID) > 0 {
			key := protocol.IDKey(message.ID)
			c.pendingMu.Lock()
			channel := c.pending[key]
			c.pendingMu.Unlock()
			if channel != nil {
				select {
				case channel <- message:
				default:
				}
			}
		}
	}
}

func (c *Client) handleTypedEvent(typeName string, raw json.RawMessage) {
	var params map[string]any
	if err := json.Unmarshal(raw, &params); err != nil || params == nil {
		params = map[string]any{}
	}
	params["type"] = typeName
	data, err := json.Marshal(params)
	if err == nil {
		c.handleEvent(data)
	}
}

func (c *Client) handleEvent(raw json.RawMessage) {
	var params map[string]any
	if err := json.Unmarshal(raw, &params); err != nil || params == nil {
		return
	}
	typeName := stringValue(params, "type", "event", "name")
	if typeName == "" {
		return
	}
	sessionID := stringValue(params, "sid", "session_id", "sessionId")
	payload := mapValue(params, "payload")
	if len(payload) == 0 {
		payload = params
	}
	if typeName == "gateway.ready" {
		c.readyOnce.Do(func() { close(c.ready) })
	}
	select {
	case c.events <- Event{Type: typeName, SessionID: sessionID, Payload: payload}:
	case <-c.done:
	}
}

func (c *Client) write(ctx context.Context, message protocol.Message) error {
	data, err := json.Marshal(message)
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	deadline := time.Now().Add(c.config.RPCTimeoutDuration())
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	if err := c.conn.SetWriteDeadline(deadline); err != nil {
		return err
	}
	if err := c.conn.WriteMessage(websocket.TextMessage, data); err != nil {
		return fmt.Errorf("Hermes Gateway write failed: %w", err)
	}
	return nil
}

func HTTPStatus(ctx context.Context, cfg config.Config) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.StatusURLWithToken(), nil)
	if err != nil {
		return fmt.Errorf("MHG1001 GATEWAY_UNAVAILABLE: build status request: %w", err)
	}
	client := &http.Client{Timeout: cfg.ConnectTimeoutDuration()}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("MHG1001 GATEWAY_UNAVAILABLE: GET /api/status: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("MHG1001 GATEWAY_UNAVAILABLE: GET /api/status returned %s", response.Status)
	}
	return nil
}

func (c *Client) ProfileExists(ctx context.Context, profile string) (bool, error) {
	profile = strings.TrimSpace(profile)
	if profile == "" {
		return true, nil
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.config.ProfilesURL(), nil)
	if err != nil {
		return false, fmt.Errorf("build profile lookup request: %w", err)
	}
	if token := c.config.Token(); token != "" {
		request.Header.Set("X-Hermes-Session-Token", token)
	}
	client := &http.Client{Timeout: c.config.ConnectTimeoutDuration()}
	response, err := client.Do(request)
	if err != nil {
		return false, fmt.Errorf("GET /api/profiles: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return false, fmt.Errorf("GET /api/profiles returned %s", response.Status)
	}
	var payload struct {
		Profiles *[]struct {
			Name string `json:"name"`
		} `json:"profiles"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return false, fmt.Errorf("decode /api/profiles response: %w", err)
	}
	if payload.Profiles == nil {
		return false, errors.New("decode /api/profiles response: profiles field is missing")
	}
	for _, item := range *payload.Profiles {
		if strings.TrimSpace(item.Name) == profile {
			return true, nil
		}
	}
	return false, nil
}

func (c *Client) ProfileTopology(ctx context.Context) (ProfileTopology, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.config.ProfilesURL(), nil)
	if err != nil {
		return ProfileTopology{}, fmt.Errorf("build profile topology request: %w", err)
	}
	if token := c.config.Token(); token != "" {
		request.Header.Set("X-Hermes-Session-Token", token)
	}
	client := &http.Client{Timeout: c.config.ConnectTimeoutDuration()}
	response, err := client.Do(request)
	if err != nil {
		return ProfileTopology{}, fmt.Errorf("GET /api/profiles: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return ProfileTopology{}, fmt.Errorf("GET /api/profiles returned %s", response.Status)
	}
	var payload ProfileTopology
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return ProfileTopology{}, fmt.Errorf("decode /api/profiles response: %w", err)
	}
	if payload.GatewayMode == "" || payload.Gateways == nil {
		return ProfileTopology{}, errors.New("profile topology fields are missing")
	}
	return payload, nil
}

func (topology ProfileTopology) Serves(profile string) bool {
	profile = strings.TrimSpace(profile)
	if profile == "" || topology.GatewayMode != "multiplex" {
		return false
	}
	exists := false
	for _, candidate := range topology.Profiles {
		if strings.TrimSpace(candidate.Name) == profile {
			exists = true
			break
		}
	}
	if !exists {
		return false
	}
	for _, gateway := range topology.Gateways {
		for _, served := range gateway.ServedProfiles {
			if strings.TrimSpace(served) == profile {
				return true
			}
		}
	}
	return false
}

func stringValue(value map[string]any, keys ...string) string {
	for _, key := range keys {
		if raw, ok := value[key]; ok {
			if text, ok := raw.(string); ok && strings.TrimSpace(text) != "" {
				return text
			}
		}
	}
	return ""
}

func mapValue(value map[string]any, key string) map[string]any {
	if raw, ok := value[key].(map[string]any); ok {
		return raw
	}
	return nil
}

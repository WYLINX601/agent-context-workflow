package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	DefaultVersion        = 1
	DefaultConnectTimeout = 5 * time.Second
	DefaultRPCTimeout     = 30 * time.Second
	DefaultTurnTimeout    = 2 * time.Hour
	profilePattern        = `^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`
)

var validProfileName = regexp.MustCompile(profilePattern)

type Config struct {
	Version      int         `yaml:"version" json:"version"`
	Gateway      Gateway     `yaml:"gateway" json:"gateway"`
	Session      Session     `yaml:"session" json:"session"`
	Concurrency  Concurrency `yaml:"concurrency" json:"concurrency"`
	Permissions  Permissions `yaml:"permissions" json:"permissions"`
	MCP          MCP         `yaml:"mcp" json:"mcp"`
	Logging      Logging     `yaml:"logging" json:"logging"`
	Supervisor   Supervisor  `yaml:"supervisor" json:"supervisor"`
	StateDB      string      `yaml:"state_db,omitempty" json:"state_db,omitempty"`
	ConfigPath   string      `yaml:"-" json:"config_path"`
	UsedDefaults bool        `yaml:"-" json:"used_defaults"`
}

type Supervisor struct {
	Enabled             bool   `yaml:"enabled" json:"enabled"`
	Endpoint            string `yaml:"endpoint" json:"endpoint"`
	RuntimeDB           string `yaml:"runtime_db,omitempty" json:"runtime_db,omitempty"`
	HermesExecutable    string `yaml:"hermes_executable" json:"hermes_executable"`
	Scope               string `yaml:"scope" json:"scope"`
	HeartbeatInterval   string `yaml:"heartbeat_interval" json:"heartbeat_interval"`
	LeaseTTL            string `yaml:"lease_ttl" json:"lease_ttl"`
	HealthInterval      string `yaml:"health_interval" json:"health_interval"`
	StartTimeout        string `yaml:"start_timeout" json:"start_timeout"`
	ShutdownGrace       string `yaml:"shutdown_grace" json:"shutdown_grace"`
	IsolatedIdleTimeout string `yaml:"isolated_idle_timeout" json:"isolated_idle_timeout"`
}

type Gateway struct {
	URL            string `yaml:"url" json:"url"`
	StatusURL      string `yaml:"status_url" json:"status_url"`
	ConnectTimeout string `yaml:"connect_timeout" json:"connect_timeout"`
	RPCTimeout     string `yaml:"rpc_timeout" json:"rpc_timeout"`
	TurnTimeout    string `yaml:"turn_timeout" json:"turn_timeout"`
	AllowRemote    bool   `yaml:"allow_remote" json:"allow_remote"`
	Token          string `yaml:"token,omitempty" json:"token,omitempty"`
	TokenFile      string `yaml:"token_file,omitempty" json:"token_file,omitempty"`
}

type Session struct {
	Source        string `yaml:"source" json:"source"`
	Profile       string `yaml:"profile,omitempty" json:"profile,omitempty"`
	CWDPolicy     string `yaml:"cwd_policy" json:"cwd_policy"`
	IDStrategy    string `yaml:"id_strategy" json:"id_strategy"`
	ReplayHistory bool   `yaml:"replay_history" json:"replay_history"`
}

type Concurrency struct {
	// ExclusiveTurnScope controls the unit serialized by the local turn lock.
	// "profile" keeps different Hermes profiles independent; "endpoint"
	// serializes all profiles sharing one Gateway endpoint.
	ExclusiveTurnScope string `yaml:"exclusive_turn_scope" json:"exclusive_turn_scope"`
	// ExclusiveTurnPerGateway is retained for configuration compatibility. It
	// is only consulted when ExclusiveTurnScope is empty; new configurations
	// should use ExclusiveTurnScope explicitly.
	ExclusiveTurnPerGateway bool `yaml:"exclusive_turn_per_gateway" json:"exclusive_turn_per_gateway"`
}

type Permissions struct {
	Mode string `yaml:"mode" json:"mode"`
}

type MCP struct {
	Mode string `yaml:"mode" json:"mode"`
}

type Logging struct {
	Level         string `yaml:"level" json:"level"`
	LogPrompts    bool   `yaml:"log_prompts" json:"log_prompts"`
	LogToolOutput bool   `yaml:"log_tool_outputs" json:"log_tool_outputs"`
}

func Defaults() Config {
	return Config{
		Version: DefaultVersion,
		Gateway: Gateway{
			URL:            "ws://127.0.0.1:9119/api/ws",
			StatusURL:      "http://127.0.0.1:9119/api/status",
			ConnectTimeout: DefaultConnectTimeout.String(),
			RPCTimeout:     DefaultRPCTimeout.String(),
			TurnTimeout:    DefaultTurnTimeout.String(),
			AllowRemote:    false,
		},
		Session: Session{
			Source:        "multica",
			CWDPolicy:     "multica",
			IDStrategy:    "mapped",
			ReplayHistory: false,
		},
		Concurrency: Concurrency{ExclusiveTurnScope: "profile", ExclusiveTurnPerGateway: true},
		Permissions: Permissions{Mode: "multica"},
		MCP:         MCP{Mode: "hermes_native"},
		Logging:     Logging{Level: "info"},
		Supervisor: Supervisor{
			Enabled:             true,
			HermesExecutable:    "hermes",
			Scope:               "shared",
			HeartbeatInterval:   "10s",
			LeaseTTL:            "30s",
			HealthInterval:      "5s",
			StartTimeout:        "20s",
			ShutdownGrace:       "5s",
			IsolatedIdleTimeout: "10m",
		},
	}
}

func DefaultConfigPath() string {
	if runtime.GOOS == "windows" {
		if dir, err := os.UserConfigDir(); err == nil {
			return filepath.Join(dir, "multica-hermes-gateway", "config.yaml")
		}
	}
	if xdg := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); xdg != "" {
		return filepath.Join(xdg, "multica-hermes-gateway", "config.yaml")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", ".config", "multica-hermes-gateway", "config.yaml")
	}
	return filepath.Join(home, ".config", "multica-hermes-gateway", "config.yaml")
}

func DefaultStatePath() string {
	if runtime.GOOS == "windows" {
		if dir, err := os.UserConfigDir(); err == nil {
			return filepath.Join(dir, "multica-hermes-gateway", "state.db")
		}
	}
	if xdg := strings.TrimSpace(os.Getenv("XDG_STATE_HOME")); xdg != "" {
		return filepath.Join(xdg, "multica-hermes-gateway", "state.db")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", ".local", "state", "multica-hermes-gateway", "state.db")
	}
	return filepath.Join(home, ".local", "state", "multica-hermes-gateway", "state.db")
}

func Load(path string) (Config, error) {
	if strings.TrimSpace(path) == "" {
		path = strings.TrimSpace(os.Getenv("MHG_CONFIG"))
	}
	if path == "" {
		path = DefaultConfigPath()
	}
	path = expand(path)
	cfg := Defaults()
	cfg.ConfigPath = path
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			cfg.UsedDefaults = true
		} else {
			return Config{}, fmt.Errorf("read config %s: %w", path, err)
		}
	} else if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config %s: %w", path, err)
	}
	if strings.TrimSpace(cfg.Gateway.StatusURL) == "" {
		cfg.Gateway.StatusURL = deriveStatusURL(cfg.Gateway.URL)
	}
	applyEnvironment(&cfg)
	cfg.Session.Profile = strings.TrimSpace(cfg.Session.Profile)
	if strings.TrimSpace(cfg.Gateway.StatusURL) == "" {
		cfg.Gateway.StatusURL = deriveStatusURL(cfg.Gateway.URL)
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func applyEnvironment(cfg *Config) {
	if value := strings.TrimSpace(os.Getenv("MHG_GATEWAY_URL")); value != "" {
		cfg.Gateway.URL = value
	}
	if value := strings.TrimSpace(os.Getenv("MHG_STATUS_URL")); value != "" {
		cfg.Gateway.StatusURL = value
	}
	if value := strings.TrimSpace(os.Getenv("MHG_LOG_LEVEL")); value != "" {
		cfg.Logging.Level = value
	}
	if value := strings.TrimSpace(os.Getenv("MHG_PROFILE")); value != "" {
		cfg.Session.Profile = value
	}
	if value := strings.TrimSpace(os.Getenv("MHG_ALLOW_REMOTE")); value != "" {
		if parsed, err := strconv.ParseBool(value); err == nil {
			cfg.Gateway.AllowRemote = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv("MHG_GATEWAY_TOKEN")); value != "" {
		cfg.Gateway.Token = value
	}
	if value := strings.TrimSpace(os.Getenv("MHG_GATEWAY_TOKEN_FILE")); value != "" {
		cfg.Gateway.TokenFile = value
	}
	if value := strings.TrimSpace(os.Getenv("MHG_SUPERVISOR_ENDPOINT")); value != "" {
		cfg.Supervisor.Endpoint = value
	}
	if value := strings.TrimSpace(os.Getenv("MHG_SUPERVISOR_RUNTIME_DB")); value != "" {
		cfg.Supervisor.RuntimeDB = value
	}
	if value := strings.TrimSpace(os.Getenv("MHG_HERMES_EXECUTABLE")); value != "" {
		cfg.Supervisor.HermesExecutable = value
	}
	if value := strings.TrimSpace(os.Getenv("MHG_SUPERVISOR_SCOPE")); value != "" {
		cfg.Supervisor.Scope = value
	}
	if value := strings.TrimSpace(os.Getenv("MHG_EXCLUSIVE_TURN_SCOPE")); value != "" {
		cfg.Concurrency.ExclusiveTurnScope = value
	}
}

func (c Config) Validate() error {
	if c.Version == 0 {
		c.Version = DefaultVersion
	}
	if strings.TrimSpace(c.Gateway.URL) == "" {
		return fmt.Errorf("MHG1001: gateway.url is required")
	}
	u, err := url.Parse(c.Gateway.URL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("MHG1001: invalid gateway.url %q", c.Gateway.URL)
	}
	if u.Scheme != "ws" && u.Scheme != "wss" {
		return fmt.Errorf("MHG1001: gateway.url must use ws or wss")
	}
	if !c.Gateway.AllowRemote && !isLoopbackHost(u.Hostname()) {
		return fmt.Errorf("MHG1001: remote Hermes Gateway disabled for host %q", u.Hostname())
	}
	if c.Gateway.StatusURL == "" {
		c.Gateway.StatusURL = deriveStatusURL(c.Gateway.URL)
	}
	status, err := url.Parse(c.Gateway.StatusURL)
	if err != nil || status.Scheme == "" || status.Host == "" || (status.Scheme != "http" && status.Scheme != "https") {
		return fmt.Errorf("MHG1001: invalid gateway.status_url %q", c.Gateway.StatusURL)
	}
	if !c.Gateway.AllowRemote && !isLoopbackHost(status.Hostname()) {
		return fmt.Errorf("MHG1001: remote Hermes status endpoint disabled for host %q", status.Hostname())
	}
	if c.Session.Source == "" {
		return fmt.Errorf("MHG9001: session.source must not be empty")
	}
	if profile := strings.TrimSpace(c.Session.Profile); profile != "" && !validProfileName.MatchString(profile) {
		return fmt.Errorf("MHG3003: invalid session.profile %q; use 1-64 ASCII letters, digits, dot, dash, or underscore", c.Session.Profile)
	}
	if c.Session.CWDPolicy != "" && c.Session.CWDPolicy != "multica" {
		return fmt.Errorf("MHG3001: session.cwd_policy must be multica")
	}
	if c.Session.IDStrategy != "" && c.Session.IDStrategy != "mapped" {
		return fmt.Errorf("MHG9001: session.id_strategy must be mapped")
	}
	if c.Permissions.Mode != "multica" && c.Permissions.Mode != "deny" {
		return fmt.Errorf("MHG4001: permissions.mode must be multica or deny")
	}
	if c.MCP.Mode == "" {
		return fmt.Errorf("MHG9001: mcp.mode must be hermes_native")
	}
	if c.Supervisor.Enabled {
		if scope := strings.TrimSpace(c.Supervisor.Scope); scope != "shared" && scope != "isolated" {
			return fmt.Errorf("MHG1105: supervisor.scope must be shared or isolated")
		}
		if strings.TrimSpace(c.Supervisor.HermesExecutable) == "" {
			return fmt.Errorf("MHG1105: supervisor.hermes_executable is required")
		}
	}
	if scope := c.turnLockScope(); scope != "endpoint" && scope != "profile" {
		return fmt.Errorf("MHG9001: concurrency.exclusive_turn_scope must be endpoint or profile")
	}
	return nil
}

func (c Config) ConnectTimeoutDuration() time.Duration {
	return parseDuration(c.Gateway.ConnectTimeout, DefaultConnectTimeout)
}

func (c Config) RPCTimeoutDuration() time.Duration {
	return parseDuration(c.Gateway.RPCTimeout, DefaultRPCTimeout)
}

func (c Config) TurnTimeoutDuration() time.Duration {
	return parseDuration(c.Gateway.TurnTimeout, DefaultTurnTimeout)
}

func (c Config) StatePath() string {
	if strings.TrimSpace(c.StateDB) != "" {
		return expand(c.StateDB)
	}
	return DefaultStatePath()
}

func (c Config) SupervisorEndpoint() string {
	if endpoint := strings.TrimSpace(c.Supervisor.Endpoint); endpoint != "" {
		return expand(endpoint)
	}
	return filepath.Join(filepath.Dir(c.StatePath()), "supervisor.sock")
}

func (c Config) SupervisorRuntimeDB() string {
	if path := strings.TrimSpace(c.Supervisor.RuntimeDB); path != "" {
		return expand(path)
	}
	return filepath.Join(filepath.Dir(c.StatePath()), "runtime.db")
}

func (c Config) SupervisorHeartbeatDuration() time.Duration {
	return parseDuration(c.Supervisor.HeartbeatInterval, 10*time.Second)
}

func (c Config) SupervisorLeaseTTL() time.Duration {
	return parseDuration(c.Supervisor.LeaseTTL, 30*time.Second)
}

func (c Config) SupervisorHealthDuration() time.Duration {
	return parseDuration(c.Supervisor.HealthInterval, 5*time.Second)
}

func (c Config) SupervisorStartTimeout() time.Duration {
	return parseDuration(c.Supervisor.StartTimeout, 20*time.Second)
}

func (c Config) SupervisorShutdownGrace() time.Duration {
	return parseDuration(c.Supervisor.ShutdownGrace, 5*time.Second)
}

func (c Config) SupervisorIdleTimeout() time.Duration {
	return parseDuration(c.Supervisor.IsolatedIdleTimeout, 10*time.Minute)
}

func (c Config) Token() string {
	if strings.TrimSpace(c.Gateway.Token) != "" {
		return strings.TrimSpace(c.Gateway.Token)
	}
	if file := strings.TrimSpace(c.Gateway.TokenFile); file != "" {
		if data, err := os.ReadFile(expand(file)); err == nil {
			return strings.TrimSpace(string(data))
		}
	}
	return ""
}

func (c Config) GatewayIdentity() string {
	u, err := url.Parse(c.Gateway.URL)
	if err != nil {
		return c.Gateway.URL
	}
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

func (c Config) Profile() string {
	return strings.TrimSpace(c.Session.Profile)
}

func (c Config) GatewayLockKey() string {
	identity := c.GatewayIdentity()
	if c.turnLockScope() == "profile" {
		identity += "\x00" + c.Profile()
	}
	digest := sha256.Sum256([]byte(identity))
	return hex.EncodeToString(digest[:])
}

func (c Config) turnLockScope() string {
	scope := strings.ToLower(strings.TrimSpace(c.Concurrency.ExclusiveTurnScope))
	if scope != "" {
		return scope
	}
	if c.Concurrency.ExclusiveTurnPerGateway {
		return "endpoint"
	}
	return "profile"
}

func (c Config) WebSocketURL() string {
	u, err := url.Parse(c.Gateway.URL)
	if err != nil {
		return c.Gateway.URL
	}
	if token := c.Token(); token != "" {
		query := u.Query()
		query.Set("token", token)
		u.RawQuery = query.Encode()
	}
	return u.String()
}

func (c Config) StatusURLWithToken() string {
	u, err := url.Parse(c.Gateway.StatusURL)
	if err != nil {
		return c.Gateway.StatusURL
	}
	if token := c.Token(); token != "" {
		query := u.Query()
		query.Set("token", token)
		u.RawQuery = query.Encode()
	}
	return u.String()
}

func (c Config) ProfilesURL() string {
	u, err := url.Parse(c.Gateway.StatusURL)
	if err != nil {
		return c.Gateway.StatusURL
	}
	u.Path = "/api/profiles"
	u.RawPath = ""
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

func (c Config) SafeJSON() ([]byte, error) {
	copy := c
	copy.Gateway.Token = ""
	copy.Gateway.TokenFile = ""
	return json.MarshalIndent(copy, "", "  ")
}

func parseDuration(value string, fallback time.Duration) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func deriveStatusURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "http://127.0.0.1:9119/api/status"
	}
	if u.Scheme == "ws" {
		u.Scheme = "http"
	} else if u.Scheme == "wss" {
		u.Scheme = "https"
	}
	u.Path = "/api/status"
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

func isLoopbackHost(host string) bool {
	host = strings.TrimSpace(strings.ToLower(host))
	if host == "localhost" || host == "localhost.localdomain" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func expand(path string) string {
	path = strings.TrimSpace(path)
	if strings.HasPrefix(path, "~"+string(os.PathSeparator)) || path == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			path = filepath.Join(home, strings.TrimPrefix(path, "~"+string(os.PathSeparator)))
		}
	}
	return filepath.Clean(path)
}

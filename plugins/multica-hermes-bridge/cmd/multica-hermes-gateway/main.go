package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/WYLINX601/project-context-workflow/plugins/multica-hermes-bridge/internal/acp"
	"github.com/WYLINX601/project-context-workflow/plugins/multica-hermes-bridge/internal/bridge"
	"github.com/WYLINX601/project-context-workflow/plugins/multica-hermes-bridge/internal/config"
	"github.com/WYLINX601/project-context-workflow/plugins/multica-hermes-bridge/internal/hermes"
	"github.com/WYLINX601/project-context-workflow/plugins/multica-hermes-bridge/internal/state"
)

var version = bridge.Version

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, in io.Reader, out, errOut io.Writer) error {
	command := "acp"
	if len(args) > 0 {
		command = args[0]
	}
	switch command {
	case "version", "--version", "-v":
		_, err := fmt.Fprintf(out, "multica-hermes-gateway %s\n", version)
		return err
	case "config":
		return runConfig(args[1:], out)
	case "doctor":
		return runDoctor(args[1:], errOut)
	case "acp":
		return runACP(args[1:], in, out, errOut)
	default:
		return fmt.Errorf("unknown command %q; use acp, doctor, config show, or version", command)
	}
}

func runConfig(args []string, out io.Writer) error {
	if len(args) == 0 || args[0] != "show" {
		return errors.New("usage: multica-hermes-gateway config show")
	}
	path := ""
	if len(args) > 1 {
		path = args[1]
	}
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}
	data, err := cfg.SafeJSON()
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(out, string(data))
	return err
}

func loadConfig(path, profileOverride string) (config.Config, error) {
	cfg, err := config.Load(path)
	if err != nil {
		return config.Config{}, err
	}
	if profile := strings.TrimSpace(profileOverride); profile != "" {
		cfg.Session.Profile = profile
		if err := cfg.Validate(); err != nil {
			return config.Config{}, err
		}
	}
	return cfg, nil
}

func runDoctor(args []string, errOut io.Writer) error {
	flags := flag.NewFlagSet("doctor", flag.ContinueOnError)
	flags.SetOutput(errOut)
	configPath := flags.String("config", "", "path to config.yaml")
	profile := flags.String("profile", "", "Hermes profile override")
	if err := flags.Parse(args); err != nil {
		return err
	}
	cfg, err := loadConfig(*configPath, *profile)
	if err != nil {
		return err
	}
	logger := log.New(errOut, "", log.LstdFlags)
	check := func(name string, action func() error) error {
		if err := action(); err != nil {
			_, _ = fmt.Fprintf(errOut, "✗ %s: %s\n", name, redact(err.Error()))
			return err
		}
		_, _ = fmt.Fprintf(errOut, "✓ %s\n", name)
		return nil
	}
	if err := check("config loaded", func() error { return cfg.Validate() }); err != nil {
		return err
	}
	if err := check("gateway endpoint is loopback or explicitly allowed", func() error { return cfg.Validate() }); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), cfg.ConnectTimeoutDuration())
	defer cancel()
	if err := check("GET /api/status", func() error { return hermes.HTTPStatus(ctx, cfg) }); err != nil {
		return err
	}
	client := hermes.NewClient(cfg)
	defer client.Close()
	if err := check("WebSocket /api/ws and gateway.ready", func() error { return client.Connect(ctx) }); err != nil {
		return err
	}
	withProfile := func(params map[string]any) map[string]any {
		if value := cfg.Profile(); value != "" {
			params["profile"] = value
		}
		return params
	}
	probes := []struct {
		method string
		params map[string]any
	}{
		{"session.create", withProfile(map[string]any{"cwd": "", "source": "multica"})},
		{"session.resume", withProfile(map[string]any{"session_id": "mhg-doctor-probe", "source": "multica"})},
		{"prompt.submit", withProfile(map[string]any{"session_id": "mhg-doctor-probe", "text": ""})},
		{"session.status", withProfile(map[string]any{"session_id": "mhg-doctor-probe"})},
		{"session.interrupt", withProfile(map[string]any{"session_id": "mhg-doctor-probe"})},
		{"session.cwd.set", withProfile(map[string]any{"session_id": "mhg-doctor-probe", "cwd": ""})},
	}
	for _, probe := range probes {
		probeCtx, probeCancel := context.WithTimeout(context.Background(), cfg.RPCTimeoutDuration())
		_, probeErr := client.Call(probeCtx, probe.method, probe.params)
		probeCancel()
		if probeErr != nil && strings.Contains(strings.ToLower(probeErr.Error()), "method not found") {
			return fmt.Errorf("MHG1003 UNSUPPORTED_GATEWAY_METHOD: %s", probe.method)
		}
		_, _ = fmt.Fprintf(errOut, "✓ %s supported\n", probe.method)
	}
	modelCtx, modelCancel := context.WithTimeout(context.Background(), cfg.RPCTimeoutDuration())
	_, modelErr := client.Call(modelCtx, "model.options", withProfile(map[string]any{}))
	modelCancel()
	if modelErr != nil {
		logger.Printf("model.options probe unavailable: %s", redact(modelErr.Error()))
		_, _ = fmt.Fprintln(errOut, "⚠ model.options unavailable; session/new will still use the current model returned by Hermes")
	} else {
		_, _ = fmt.Fprintln(errOut, "✓ model.options available")
	}
	return nil
}

func runACP(args []string, in io.Reader, out, errOut io.Writer) error {
	flags := flag.NewFlagSet("acp", flag.ContinueOnError)
	flags.SetOutput(errOut)
	configPath := flags.String("config", "", "path to config.yaml")
	profile := flags.String("profile", "", "Hermes profile override")
	if err := flags.Parse(args); err != nil {
		return err
	}
	cfg, err := loadConfig(*configPath, *profile)
	if err != nil {
		return err
	}
	store, err := state.Open(cfg.StatePath())
	if err != nil {
		return err
	}
	defer store.Close()
	logf := func(format string, values ...any) {
		if strings.EqualFold(cfg.Logging.Level, "silent") || strings.EqualFold(cfg.Logging.Level, "off") {
			return
		}
		log.New(errOut, "", log.LstdFlags).Printf(format, values...)
	}
	client := hermes.NewClient(cfg)
	bridgeHandler := bridge.New(cfg, store, client, logf)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	startCtx, startCancel := context.WithTimeout(ctx, cfg.ConnectTimeoutDuration())
	if err := bridgeHandler.Start(startCtx); err != nil {
		startCancel()
		return err
	}
	startCancel()
	server := acp.NewServer(in, out, bridgeHandler, logf)
	go func() {
		<-ctx.Done()
		server.Close()
	}()
	return server.Run(ctx)
}

func redact(value string) string {
	for _, key := range []string{"token=", "Bearer ", "api_key=", "password="} {
		if index := strings.Index(strings.ToLower(value), strings.ToLower(key)); index >= 0 {
			return value[:index] + key + "[REDACTED]"
		}
	}
	return value
}

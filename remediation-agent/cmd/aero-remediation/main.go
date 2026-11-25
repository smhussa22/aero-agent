// Command aero-remediation is the agent that runs on the monitored server. It dials out to the
// orchestrator, investigates alerts with your own AI provider, and runs only allowlisted fixes
// within the limits you set here.
//
// Configuration is by environment variable:
//
//	AERO_ORCH_ADDR         orchestrator host:port (required)
//	AERO_TOKEN             this host's token from `hostctl add` (required)
//	AERO_CA_CERT           PEM file for a private CA; empty uses system roots
//	AERO_INSECURE=1        no TLS, local development only
//	AERO_HOST_ID           defaults to the hostname; must match the token's host
//	AERO_MAX_MODE          observe (default), recommend or autonomous: a ceiling the server cannot exceed
//	AERO_ALLOWED_SERVICES  comma-separated services restart_service may touch (default none)
//	AERO_SUDO=1            run fixes through `sudo -n --` (pair with deploy/aero-remediation.sudoers)
//	AERO_DRY_RUN=1         log what would run, run nothing
//	AERO_AI_PROVIDER, AERO_AI_KEY, AERO_AI_MODEL, AERO_AI_BASE_URL   your AI provider
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/smhussa22/aero-agent/remediation-agent/internal/actions"
	"github.com/smhussa22/aero-agent/remediation-agent/internal/ai"
	"github.com/smhussa22/aero-agent/remediation-agent/internal/audit"
	"github.com/smhussa22/aero-agent/remediation-agent/internal/executor"
	"github.com/smhussa22/aero-agent/remediation-agent/internal/gather"
	"github.com/smhussa22/aero-agent/remediation-agent/internal/investigate"
	"github.com/smhussa22/aero-agent/remediation-agent/internal/link"
)

func die(msg string, err error) {
	slog.Error(msg, "err", err)
	os.Exit(1)
}

func main() {
	addr, token := os.Getenv("AERO_ORCH_ADDR"), os.Getenv("AERO_TOKEN")
	if addr == "" || token == "" {
		slog.Error("AERO_ORCH_ADDR and AERO_TOKEN are required")
		os.Exit(2)
	}
	host := os.Getenv("AERO_HOST_ID")
	if host == "" {
		host, _ = os.Hostname()
	}

	// the ceiling defaults to observe so a fresh install can never change anything by accident
	ceiling := executor.Mode(strings.ToLower(os.Getenv("AERO_MAX_MODE")))
	if ceiling == "" {
		ceiling = executor.Observe
	}
	if _, ok := map[executor.Mode]bool{executor.Observe: true, executor.Recommend: true, executor.Autonomous: true}[ceiling]; !ok {
		slog.Error("AERO_MAX_MODE must be observe, recommend or autonomous")
		os.Exit(2)
	}

	provider, err := ai.New(ai.Config{
		Kind: os.Getenv("AERO_AI_PROVIDER"), Key: os.Getenv("AERO_AI_KEY"),
		Model: os.Getenv("AERO_AI_MODEL"), BaseURL: os.Getenv("AERO_AI_BASE_URL"),
	}, nil)
	if err != nil {
		die("ai config", err)
	}

	// the local audit file is the durable record and the one that must never fail
	stateDir := os.Getenv("STATE_DIRECTORY")
	if stateDir == "" {
		stateDir = "."
	}
	file, err := audit.OpenFile(filepath.Join(stateDir, "audit.log"))
	if err != nil {
		die("audit log", err)
	}
	defer file.Close()

	cfg := executor.DefaultConfig()
	cfg.Mode = executor.Observe // until the orchestrator says otherwise (and never above the ceiling)
	cfg.DryRun = os.Getenv("AERO_DRY_RUN") == "1"
	cfg.Sudo = os.Getenv("AERO_SUDO") == "1"
	cfg.ProtectedPIDs = []int{os.Getpid(), os.Getppid()}
	for _, s := range strings.Split(os.Getenv("AERO_ALLOWED_SERVICES"), ",") {
		if s = strings.TrimSpace(s); s != "" {
			cfg.AllowedServices = append(cfg.AllowedServices, s)
		}
	}

	l := link.New(link.Config{
		Addr: addr, Token: token, CACert: os.Getenv("AERO_CA_CERT"),
		Insecure: os.Getenv("AERO_INSECURE") == "1", HostID: host, MaxMode: ceiling,
	})
	ex := executor.New(cfg, executor.Deps{
		Runner:   executor.ExecRunner{MaxBytes: 64 << 10},
		Approver: l,
		Audit:    audit.Tee{Primary: file, Secondary: l},
	})
	inv := &investigate.Investigator{
		AI:       provider,
		Gather:   gather.Gatherer{Runner: gather.ExecRunner{MaxBytes: 64 << 10}, Timeout: 5 * time.Second},
		Catalog:  actions.Default(),
		Services: cfg.AllowedServices,
	}
	l.Bind(inv, ex, file)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	slog.Info("aero-remediation starting", "host", host, "ceiling", ceiling, "dry_run", cfg.DryRun, "sudo", cfg.Sudo,
		"services", cfg.AllowedServices)
	if err := l.Run(ctx); err != nil {
		die("link", err)
	}
}

// Command investigate runs one investigation against the local machine and prints the report.
// It is read-only: it gathers diagnostics and asks your AI provider, but runs no fixes.
//
//	AERO_AI_PROVIDER=anthropic AERO_AI_KEY=... AERO_AI_MODEL=... investigate -host web-1 -metric cpu.usage_percent -value 96 -threshold 90
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/smhussa22/aero-agent/remediation-agent/internal/actions"
	"github.com/smhussa22/aero-agent/remediation-agent/internal/ai"
	"github.com/smhussa22/aero-agent/remediation-agent/internal/gather"
	"github.com/smhussa22/aero-agent/remediation-agent/internal/investigate"
)

func main() {
	var a investigate.Alert
	flag.StringVar(&a.HostID, "host", "localhost", "host the alert is about")
	flag.StringVar(&a.Metric, "metric", "cpu.usage_percent", "metric that fired")
	flag.Float64Var(&a.Value, "value", 0, "observed value")
	flag.Float64Var(&a.Threshold, "threshold", 0, "rule threshold")
	flag.StringVar(&a.Severity, "severity", "warning", "info, warning or critical")
	flag.Parse()

	provider, err := ai.New(ai.Config{
		Kind:    os.Getenv("AERO_AI_PROVIDER"),
		Key:     os.Getenv("AERO_AI_KEY"),
		Model:   os.Getenv("AERO_AI_MODEL"),
		BaseURL: os.Getenv("AERO_AI_BASE_URL"),
	}, nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, "config:", err)
		os.Exit(2)
	}

	inv := &investigate.Investigator{
		AI:      provider,
		Gather:  gather.Gatherer{Runner: gather.ExecRunner{MaxBytes: 64 << 10}, Timeout: 5 * time.Second},
		Catalog: actions.Default(),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	report, err := inv.Investigate(ctx, a)
	if err != nil {
		fmt.Fprintln(os.Stderr, "investigation failed:", err)
		os.Exit(1)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.Encode(report)
}

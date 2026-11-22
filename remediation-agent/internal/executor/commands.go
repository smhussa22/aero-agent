package executor

import (
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strings"

	"github.com/smhussa22/aero-agent/remediation-agent/internal/actions"
)

func canonicalParams(p actions.Proposal) string {
	keys := make([]string, 0, len(p.Params))
	for k := range p.Params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = k + "=" + p.Params[k]
	}
	return strings.Join(parts, ",")
}

func formatParams(p actions.Proposal) string {
	return p.ActionID + " " + canonicalParams(p)
}

// policy applies the machine owner's limits on top of the catalog's shape checks.
func (e *Executor) policy(p actions.Proposal) string {
	switch p.ActionID {
	case "restart_service":
		svc := strings.TrimSuffix(p.Params["service"], ".service")
		for _, allowed := range e.cfg.AllowedServices {
			if strings.TrimSuffix(allowed, ".service") == svc {
				return ""
			}
		}
		return fmt.Sprintf("service %q is not in the allowed services list", p.Params["service"])
	case "kill_process":
		var pid int
		fmt.Sscanf(p.Params["pid"], "%d", &pid)
		for _, protected := range e.cfg.ProtectedPIDs {
			if pid == protected {
				return fmt.Sprintf("pid %d is protected", pid)
			}
		}
	}
	return ""
}

// argv builds the exact command. Parameters were validated against strict patterns, and they
// are passed as separate arguments (never joined into a shell string), after "--" where it matters.
func (e *Executor) argv(p actions.Proposal) ([]string, error) {
	var argv []string
	switch p.ActionID {
	case "restart_service":
		argv = []string{"systemctl", "restart", "--", p.Params["service"]}
	case "kill_process":
		sig := p.Params["signal"]
		if sig == "" {
			sig = "TERM"
		}
		argv = []string{"kill", "-s", sig, "--", p.Params["pid"]}
	case "vacuum_journal":
		argv = []string{"journalctl", "--vacuum-time=" + p.Params["days"] + "d"}
	default:
		return nil, fmt.Errorf("no command is defined for action %q", p.ActionID)
	}
	if e.cfg.Sudo {
		argv = append([]string{"sudo", "-n", "--"}, argv...)
	}
	return argv, nil
}

// alreadyDone treats "the target is already gone" as success so retries stay idempotent.
func alreadyDone(p actions.Proposal, output string) bool {
	return p.ActionID == "kill_process" && strings.Contains(output, "No such process")
}

// ExecRunner runs commands with os/exec. Output is capped so a chatty command cannot exhaust memory.
type ExecRunner struct {
	MaxBytes int
}

type capped struct {
	b   strings.Builder
	max int
}

func (c *capped) Write(p []byte) (int, error) {
	if room := c.max - c.b.Len(); room > 0 {
		if len(p) > room {
			c.b.Write(p[:room])
		} else {
			c.b.Write(p)
		}
	}
	return len(p), nil
}

func (r ExecRunner) Run(ctx context.Context, argv []string) (string, error) {
	out := &capped{max: r.MaxBytes}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Stdout, cmd.Stderr = out, out
	err := cmd.Run()
	return out.b.String(), err
}

// Package gather collects read-only diagnostics about the host for an investigation.
// Everything it returns is redacted and size-capped, and is untrusted: process names and
// log lines are controlled by whoever can write to this machine.
package gather

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/smhussa22/aero-agent/remediation-agent/internal/redact"
)

type Section struct {
	Title string
	Body  string
}

type Context struct {
	Sections []Section
}

// Runner runs one fixed command (no shell) and returns its stdout.
type Runner interface {
	Run(ctx context.Context, name string, args ...string) (string, error)
}

type limitedBuffer struct {
	b   strings.Builder
	max int
}

func (l *limitedBuffer) Write(p []byte) (int, error) {
	if room := l.max - l.b.Len(); room > 0 {
		if len(p) > room {
			l.b.Write(p[:room])
		} else {
			l.b.Write(p)
		}
	}
	return len(p), nil // swallow the rest so the child never blocks on a full pipe
}

// ExecRunner runs commands directly with os/exec.
type ExecRunner struct {
	MaxBytes int
}

func (e ExecRunner) Run(ctx context.Context, name string, args ...string) (string, error) {
	out := &limitedBuffer{max: e.MaxBytes}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = out
	err := cmd.Run()
	return out.b.String(), err
}

type command struct {
	title string
	name  string
	args  []string
	lines int // keep at most this many lines
}

// the fixed set of diagnostics; nothing here is influenced by alert content
var commands = []command{
	{"Top processes by CPU", "ps", []string{"-eo", "pid,user,comm,%cpu,%mem", "--sort=-%cpu"}, 15},
	{"Load", "uptime", nil, 3},
	{"Memory (MB)", "free", []string{"-m"}, 5},
	{"Disk usage", "df", []string{"-h", "-x", "tmpfs", "-x", "devtmpfs"}, 15},
	{"Recent warnings and errors", "journalctl", []string{"-p", "warning", "-n", "40", "--no-pager"}, 40},
	{"Failed units", "systemctl", []string{"--failed", "--no-pager", "--plain"}, 15},
}

type Gatherer struct {
	Runner  Runner
	Timeout time.Duration // per command
}

func clip(s string, lines int) string {
	all := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(all) > lines {
		all = append(all[:lines], fmt.Sprintf("... (%d more lines omitted)", len(all)-lines))
	}
	return strings.Join(all, "\n")
}

// Gather runs every diagnostic. A command that fails or is missing becomes a note instead of an error.
func (g Gatherer) Gather(ctx context.Context) Context {
	var out Context
	for _, c := range commands {
		cctx, cancel := context.WithTimeout(ctx, g.Timeout)
		text, err := g.Runner.Run(cctx, c.name, c.args...)
		cancel()

		body := clip(redact.Redact(text), c.lines)
		if err != nil && strings.TrimSpace(text) == "" {
			body = "unavailable: " + err.Error()
		}
		out.Sections = append(out.Sections, Section{Title: c.title, Body: body})
	}
	return out
}

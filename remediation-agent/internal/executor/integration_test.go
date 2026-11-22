package executor

import (
	"context"
	"os/exec"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/smhussa22/aero-agent/remediation-agent/internal/actions"
)

// These tests run real processes, so they only run on Linux.
func needLinux(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("needs linux")
	}
}

func TestRealKillThroughTheExecutor(t *testing.T) {
	needLinux(t)

	victim := exec.Command("sleep", "300")
	if err := victim.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan error, 1)
	go func() { exited <- victim.Wait() }()
	t.Cleanup(func() { victim.Process.Kill() })

	cfg := DefaultConfig()
	cfg.Mode = Autonomous
	ex := New(cfg, Deps{Runner: ExecRunner{MaxBytes: 4096}, Approver: &fakeApprover{approved: true}, Audit: &memAudit{}})

	out, err := ex.Handle(context.Background(), Task{
		IncidentID: "i1",
		Proposal: actions.Proposal{ActionID: "kill_process", Dangerous: true,
			Params: map[string]string{"pid": strconv.Itoa(victim.Process.Pid)}},
	})
	if err != nil || out.Status != StatusSucceeded {
		t.Fatalf("out=%+v err=%v", out, err)
	}
	select {
	case <-exited:
	case <-time.After(3 * time.Second):
		t.Fatal("the process is still alive")
	}
}

func TestRealCommandIsCutOffByTheTimeout(t *testing.T) {
	needLinux(t)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := ExecRunner{MaxBytes: 1024}.Run(ctx, []string{"sleep", "30"})
	if err == nil || time.Since(start) > 5*time.Second {
		t.Fatalf("err=%v after %v: a hung command must be killed at the deadline", err, time.Since(start))
	}
}

func TestRealOutputIsCapped(t *testing.T) {
	needLinux(t)
	out, err := ExecRunner{MaxBytes: 100}.Run(context.Background(), []string{"sh", "-c", "yes | head -c 100000"})
	if err != nil || len(out) != 100 {
		t.Fatalf("len=%d err=%v", len(out), err)
	}
}

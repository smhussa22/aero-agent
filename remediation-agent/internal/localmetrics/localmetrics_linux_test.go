//go:build linux

package localmetrics

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseCPULine(t *testing.T) {
	total, idle, err := parseCPULine("cpu  100 0 50 800 50 0 0 0 0 0")
	if err != nil || total != 1000 || idle != 850 {
		t.Fatalf("total=%d idle=%d err=%v", total, idle, err)
	}
	if _, _, err := parseCPULine("cpu0 1 2"); err == nil {
		t.Fatal("garbage must be rejected")
	}
	if got := usage(200, 50); got != 75 {
		t.Fatalf("usage=%v", got)
	}
}

func TestParseMeminfo(t *testing.T) {
	p := filepath.Join(t.TempDir(), "meminfo")
	os.WriteFile(p, []byte("MemTotal: 1000 kB\nMemFree: 100 kB\nMemAvailable: 400 kB\n"), 0o600)
	f, _ := os.Open(p)
	defer f.Close()
	if v, err := parseMeminfo(f); err != nil || v != 60 {
		t.Fatalf("v=%v err=%v", v, err)
	}
}

func TestRealReadsAreSane(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, m := range []string{"cpu.usage_percent", "memory.used_percent", "disk.used_percent"} {
		v, err := Read(ctx, m, "")
		if err != nil || v < 0 || v > 100 {
			t.Errorf("%s = %v, err=%v", m, v, err)
		}
	}
	if _, err := Read(ctx, "net.rx_bytes", ""); err == nil {
		t.Error("unsupported metrics must error, not return 0")
	}
}

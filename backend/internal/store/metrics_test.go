package store

import (
	"testing"

	aerov1 "github.com/smhussa22/aero-agent/backend/gen/aerov1"
)

func TestFlatten(t *testing.T) {
	rows := Flatten(&aerov1.MetricBatch{
		Cpu:          &aerov1.CpuMetrics{UsagePercent: 50, Cores: 4},
		Memory:       &aerov1.MemoryMetrics{TotalBytes: 1000, UsedBytes: 250},
		Disks:        []*aerov1.DiskMetrics{{Mount: "/", TotalBytes: 200, UsedBytes: 100}},
		Interfaces:   []*aerov1.NetworkMetrics{{Name: "eth0", RxBytes: 7}},
		TopProcesses: []*aerov1.ProcessMetrics{{Pid: 1234, Name: "nginx", CpuPercent: 9}},
	})

	got := map[string]float64{}
	for _, r := range rows {
		got[r.Metric+"|"+r.Label] = r.Value
	}

	want := map[string]float64{
		"cpu.usage_percent|":              50,
		"cpu.cores|":                      4,
		"memory.used_percent|":            25,
		"disk.used_percent|/":             50,
		"net.rx_bytes|eth0":               7,
		"process.cpu_percent|nginx[1234]": 9,
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %v, want %v", k, got[k], v)
		}
	}
}

func TestFlattenEmptyBatchAndZeroTotals(t *testing.T) {
	if rows := Flatten(&aerov1.MetricBatch{}); len(rows) != 0 {
		t.Fatalf("empty batch gave %d rows", len(rows))
	}
	// zero total memory must not divide by zero
	for _, r := range Flatten(&aerov1.MetricBatch{Memory: &aerov1.MemoryMetrics{UsedBytes: 5}}) {
		if r.Metric == "memory.used_percent" && r.Value != 0 {
			t.Fatalf("used_percent = %v", r.Value)
		}
	}
}

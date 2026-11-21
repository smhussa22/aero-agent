// Package store writes agent metrics into TimescaleDB.
package store

import (
	"context"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	aerov1 "github.com/smhussa22/aero-agent/backend/gen/aerov1"
)

// Row is one value of the narrow metrics table.
type Row struct {
	Metric string
	Label  string
	Value  float64
}

func percent(used, total uint64) float64 {
	if total == 0 {
		return 0
	}
	return 100 * float64(used) / float64(total)
}

// Flatten turns one batch into metric rows. Counters (bytes, errors) stay cumulative.
func Flatten(b *aerov1.MetricBatch) []Row {
	var rows []Row
	add := func(metric, label string, v float64) { rows = append(rows, Row{metric, label, v}) }

	if c := b.GetCpu(); c != nil {
		add("cpu.usage_percent", "", c.UsagePercent)
		add("cpu.load_1m", "", c.Load_1M)
		add("cpu.load_5m", "", c.Load_5M)
		add("cpu.load_15m", "", c.Load_15M)
		add("cpu.cores", "", float64(c.Cores))
	}
	if m := b.GetMemory(); m != nil {
		add("memory.total_bytes", "", float64(m.TotalBytes))
		add("memory.used_bytes", "", float64(m.UsedBytes))
		add("memory.used_percent", "", percent(m.UsedBytes, m.TotalBytes))
		add("memory.swap_total_bytes", "", float64(m.SwapTotalBytes))
		add("memory.swap_used_bytes", "", float64(m.SwapUsedBytes))
	}
	for _, d := range b.GetDisks() {
		add("disk.total_bytes", d.Mount, float64(d.TotalBytes))
		add("disk.used_bytes", d.Mount, float64(d.UsedBytes))
		add("disk.used_percent", d.Mount, percent(d.UsedBytes, d.TotalBytes))
		add("disk.read_bytes", d.Mount, float64(d.ReadBytes))
		add("disk.write_bytes", d.Mount, float64(d.WriteBytes))
	}
	for _, n := range b.GetInterfaces() {
		add("net.rx_bytes", n.Name, float64(n.RxBytes))
		add("net.tx_bytes", n.Name, float64(n.TxBytes))
		add("net.rx_errors", n.Name, float64(n.RxErrors))
		add("net.tx_errors", n.Name, float64(n.TxErrors))
	}
	for _, p := range b.GetTopProcesses() {
		label := p.Name + "[" + strconv.FormatUint(uint64(p.Pid), 10) + "]"
		add("process.cpu_percent", label, p.CpuPercent)
		add("process.rss_bytes", label, float64(p.RssBytes))
	}
	return rows
}

// InsertBatch stores a batch's rows and marks the host as seen, atomically.
func InsertBatch(ctx context.Context, pool *pgxpool.Pool, b *aerov1.MetricBatch) error {
	ts := b.GetCollectedAt().AsTime()
	rows := Flatten(b)
	copyRows := make([][]any, len(rows))
	for i, r := range rows {
		copyRows[i] = []any{ts, b.GetHostId(), r.Metric, r.Label, r.Value}
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `INSERT INTO hosts (id, last_seen) VALUES ($1, now())
		ON CONFLICT (id) DO UPDATE SET last_seen = now()`, b.GetHostId()); err != nil {
		return err
	}
	if _, err := tx.CopyFrom(ctx, pgx.Identifier{"metrics"},
		[]string{"time", "host_id", "metric", "label", "value"}, pgx.CopyFromRows(copyRows)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

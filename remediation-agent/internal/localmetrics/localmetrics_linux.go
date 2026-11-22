//go:build linux

// Package localmetrics re-reads a few metrics straight from the kernel so the agent can check
// whether a fix worked, without depending on the backend being reachable.
package localmetrics

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Read returns the current value of metric (and label, for per-mount metrics).
func Read(ctx context.Context, metric, label string) (float64, error) {
	switch metric {
	case "cpu.usage_percent":
		return cpuUsage(ctx)
	case "memory.used_percent":
		return memUsedPercent()
	case "disk.used_percent":
		if label == "" {
			label = "/"
		}
		return diskUsedPercent(label)
	}
	return 0, fmt.Errorf("metric %q cannot be re-checked locally", metric)
}

// Supported reports whether Read can handle metric.
func Supported(metric string) bool {
	switch metric {
	case "cpu.usage_percent", "memory.used_percent", "disk.used_percent":
		return true
	}
	return false
}

func cpuTimes() (total, idle uint64, err error) {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()
	line, err := bufio.NewReader(f).ReadString('\n')
	if err != nil {
		return 0, 0, err
	}
	return parseCPULine(line)
}

func parseCPULine(line string) (total, idle uint64, err error) {
	fields := strings.Fields(line)
	if len(fields) < 9 || fields[0] != "cpu" {
		return 0, 0, errors.New("unexpected /proc/stat format")
	}
	for i, f := range fields[1:9] {
		v, err := strconv.ParseUint(f, 10, 64)
		if err != nil {
			return 0, 0, err
		}
		total += v
		if i == 3 || i == 4 { // idle + iowait
			idle += v
		}
	}
	return total, idle, nil
}

// cpuUsage samples twice, one second apart, because usage is a rate.
func cpuUsage(ctx context.Context) (float64, error) {
	t1, i1, err := cpuTimes()
	if err != nil {
		return 0, err
	}
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	case <-time.After(time.Second):
	}
	t2, i2, err := cpuTimes()
	if err != nil {
		return 0, err
	}
	if t2 <= t1 {
		return 0, errors.New("cpu counters did not advance")
	}
	return usage(t2-t1, i2-i1), nil
}

func usage(total, idle uint64) float64 {
	return 100 * (1 - float64(idle)/float64(total))
}

func memUsedPercent() (float64, error) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, err
	}
	defer f.Close()
	return parseMeminfo(f)
}

func parseMeminfo(f *os.File) (float64, error) {
	var total, avail float64
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 {
			continue
		}
		v, _ := strconv.ParseFloat(fields[1], 64)
		switch fields[0] {
		case "MemTotal:":
			total = v
		case "MemAvailable:":
			avail = v
		}
	}
	if total == 0 {
		return 0, errors.New("could not read MemTotal")
	}
	return 100 * (total - avail) / total, nil
}

func diskUsedPercent(mount string) (float64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(mount, &st); err != nil {
		return 0, err
	}
	if st.Blocks == 0 {
		return 0, errors.New("filesystem reports no blocks")
	}
	return 100 * float64(st.Blocks-st.Bfree) / float64(st.Blocks), nil
}

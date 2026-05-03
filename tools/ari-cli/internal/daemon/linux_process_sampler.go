package daemon

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
)

var procRoot = "/proc"

var linuxClockTicks = cachedLinuxClockTicks

var linuxClockTicksCache struct {
	once  sync.Once
	value int64
	err   error
}

type linuxProcStat struct {
	PID      int64
	PPID     int64
	Pgrp     int64
	Session  int64
	UTime    int64
	STime    int64
	CUTime   int64
	CSTime   int64
	RSSPages int64
}

func sampleLinuxProcessMetrics(ctx context.Context, run AgentSession) ProcessMetricsSample {
	_ = ctx
	if runtime.GOOS != "linux" {
		return unsupportedProcessMetrics("unsupported")
	}
	if run.PID <= 0 {
		return unsupportedProcessMetrics("unavailable")
	}
	statPath := filepath.Join(procRoot, strconv.Itoa(run.PID), "stat")
	statBytes, err := os.ReadFile(statPath)
	if err != nil {
		return unsupportedProcessMetrics("unavailable")
	}
	stat, err := parseLinuxProcStat(string(statBytes))
	if err != nil {
		return unsupportedProcessMetrics("unavailable")
	}
	ticks, err := linuxClockTicks()
	if err != nil || ticks <= 0 {
		return ProcessMetricsSample{OwnedByAri: true, PID: ProcessMetricValue{Known: true, Value: int64Value(int64(run.PID)), Confidence: "sampled"}, CPUTimeMS: unknownProcessMetric("unsupported"), MemoryRSSBytesPeak: unknownProcessMetric("unsupported"), ChildProcessesPeak: unknownProcessMetric("unsupported"), Ports: sampleLinuxListeningPorts(run.PID), OrphanState: "unknown", ExitCode: exitCodeMetric(run)}
	}
	pageSize := int64(os.Getpagesize())
	cpuTimeMS := ((stat.UTime + stat.STime + stat.CUTime + stat.CSTime) * 1000) / ticks
	rssBytes := stat.RSSPages * pageSize
	ports := sampleLinuxListeningPorts(run.PID)
	childCount := sampleLinuxDirectChildCount(run.PID)
	orphanState := "not_orphaned"
	if stat.PPID == 1 {
		orphanState = "adopted_by_init"
	}
	return ProcessMetricsSample{OwnedByAri: true, PID: ProcessMetricValue{Known: true, Value: int64Value(int64(run.PID)), Confidence: "sampled"}, CPUTimeMS: ProcessMetricValue{Known: true, Value: &cpuTimeMS, Confidence: "sampled"}, MemoryRSSBytesPeak: ProcessMetricValue{Known: true, Value: &rssBytes, Confidence: "sampled"}, ChildProcessesPeak: childCount, Ports: ports, OrphanState: orphanState, ExitCode: exitCodeMetric(run)}
}

func cachedLinuxClockTicks() (int64, error) {
	linuxClockTicksCache.once.Do(func() {
		output, err := exec.Command("getconf", "CLK_TCK").Output()
		if err != nil {
			linuxClockTicksCache.err = fmt.Errorf("getconf CLK_TCK: %w", err)
			return
		}
		value, err := strconv.ParseInt(strings.TrimSpace(string(output)), 10, 64)
		if err != nil || value <= 0 {
			linuxClockTicksCache.err = fmt.Errorf("parse CLK_TCK %q: %w", strings.TrimSpace(string(output)), err)
			return
		}
		linuxClockTicksCache.value = value
	})
	return linuxClockTicksCache.value, linuxClockTicksCache.err
}

func int64Value(value int64) *int64 { return &value }

func unsupportedProcessMetrics(confidence string) ProcessMetricsSample {
	return ProcessMetricsSample{OwnedByAri: false, PID: unknownProcessMetric(confidence), CPUTimeMS: unknownProcessMetric(confidence), MemoryRSSBytesPeak: unknownProcessMetric(confidence), ChildProcessesPeak: unknownProcessMetric(confidence), Ports: []ProcessPortObservation{}, OrphanState: confidence, ExitCode: unknownProcessMetric(confidence)}
}

func exitCodeMetric(run AgentSession) ProcessMetricValue {
	if run.ExitCode == nil {
		return unknownProcessMetric("unavailable")
	}
	value := int64(*run.ExitCode)
	return ProcessMetricValue{Known: true, Value: &value, Confidence: "sampled"}
}

func parseLinuxProcStat(raw string) (linuxProcStat, error) {
	raw = strings.TrimSpace(raw)
	open := strings.Index(raw, "(")
	close := strings.LastIndex(raw, ")")
	if open <= 0 || close <= open {
		return linuxProcStat{}, fmt.Errorf("invalid proc stat comm field")
	}
	pid, err := strconv.ParseInt(strings.TrimSpace(raw[:open]), 10, 64)
	if err != nil {
		return linuxProcStat{}, fmt.Errorf("parse pid: %w", err)
	}
	fields := strings.Fields(strings.TrimSpace(raw[close+1:]))
	if len(fields) < 22 {
		return linuxProcStat{}, fmt.Errorf("proc stat has %d fields after comm, want at least 22", len(fields))
	}
	parse := func(index int, name string) (int64, error) {
		value, err := strconv.ParseInt(fields[index], 10, 64)
		if err != nil {
			return 0, fmt.Errorf("parse %s: %w", name, err)
		}
		return value, nil
	}
	ppid, err := parse(1, "ppid")
	if err != nil {
		return linuxProcStat{}, err
	}
	pgrp, err := parse(2, "pgrp")
	if err != nil {
		return linuxProcStat{}, err
	}
	session, err := parse(3, "session")
	if err != nil {
		return linuxProcStat{}, err
	}
	utime, err := parse(11, "utime")
	if err != nil {
		return linuxProcStat{}, err
	}
	stime, err := parse(12, "stime")
	if err != nil {
		return linuxProcStat{}, err
	}
	cutime, err := parse(13, "cutime")
	if err != nil {
		return linuxProcStat{}, err
	}
	cstime, err := parse(14, "cstime")
	if err != nil {
		return linuxProcStat{}, err
	}
	rss, err := parse(21, "rss")
	if err != nil {
		return linuxProcStat{}, err
	}
	return linuxProcStat{PID: pid, PPID: ppid, Pgrp: pgrp, Session: session, UTime: utime, STime: stime, CUTime: cutime, CSTime: cstime, RSSPages: rss}, nil
}

func sampleLinuxListeningPorts(pid int) []ProcessPortObservation {
	inodes := socketInodesForPID(pid)
	if len(inodes) == 0 {
		return []ProcessPortObservation{}
	}
	ports := make([]ProcessPortObservation, 0)
	for _, name := range []string{"tcp", "tcp6"} {
		path := filepath.Join(procRoot, strconv.Itoa(pid), "net", name)
		content, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		ports = append(ports, parseLinuxTCPListeningPorts(string(content), inodes)...)
	}
	return ports
}

func socketInodesForPID(pid int) map[string]bool {
	entries, err := os.ReadDir(filepath.Join(procRoot, strconv.Itoa(pid), "fd"))
	if err != nil {
		return nil
	}
	inodes := map[string]bool{}
	for _, entry := range entries {
		target, err := os.Readlink(filepath.Join(procRoot, strconv.Itoa(pid), "fd", entry.Name()))
		if err != nil || !strings.HasPrefix(target, "socket:[") || !strings.HasSuffix(target, "]") {
			continue
		}
		inode := strings.TrimSuffix(strings.TrimPrefix(target, "socket:["), "]")
		inodes[inode] = true
	}
	return inodes
}

func parseLinuxTCPListeningPorts(raw string, inodes map[string]bool) []ProcessPortObservation {
	lines := strings.Split(raw, "\n")
	ports := make([]ProcessPortObservation, 0)
	seen := map[int]bool{}
	for _, line := range lines[1:] {
		fields := strings.Fields(line)
		if len(fields) < 10 || fields[3] != "0A" || !inodes[fields[9]] {
			continue
		}
		parts := strings.Split(fields[1], ":")
		if len(parts) != 2 {
			continue
		}
		port64, err := strconv.ParseInt(parts[1], 16, 32)
		if err != nil {
			continue
		}
		port := int(port64)
		if seen[port] {
			continue
		}
		seen[port] = true
		ports = append(ports, ProcessPortObservation{Port: port, Protocol: "tcp", Confidence: "detected"})
	}
	return ports
}

func sampleLinuxDirectChildCount(pid int) ProcessMetricValue {
	entries, err := os.ReadDir(procRoot)
	if err != nil {
		return unknownProcessMetric("unavailable")
	}
	count := int64(0)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, err := strconv.Atoi(entry.Name()); err != nil {
			continue
		}
		content, err := os.ReadFile(filepath.Join(procRoot, entry.Name(), "stat"))
		if err != nil {
			continue
		}
		stat, err := parseLinuxProcStat(string(content))
		if err != nil {
			continue
		}
		if stat.PPID == int64(pid) {
			count++
		}
	}
	return ProcessMetricValue{Known: true, Value: &count, Confidence: "sampled"}
}

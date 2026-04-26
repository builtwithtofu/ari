package daemon

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestParseLinuxProcStatHandlesSpacesInCommand(t *testing.T) {
	stat, err := parseLinuxProcStat("123 (agent worker) S 7 8 9 0 -1 4194560 10 20 30 40 50 60 70 80 20 0 1 0 100 4096 42 0 0 0")
	if err != nil {
		t.Fatalf("parseLinuxProcStat returned error: %v", err)
	}
	if stat.PID != 123 || stat.PPID != 7 || stat.Pgrp != 8 || stat.Session != 9 || stat.UTime != 50 || stat.STime != 60 || stat.CUTime != 70 || stat.CSTime != 80 || stat.RSSPages != 42 {
		t.Fatalf("stat = %#v, want parsed process fields", stat)
	}
}

func TestParseLinuxTCPListeningPortsMatchesSocketInodes(t *testing.T) {
	raw := `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 0100007F:1435 00000000:0000 0A 00000000:00000000 00:00000000 00000000 1000 0 55555 1 0000000000000000 100 0 0 10 0
   1: 0100007F:1436 00000000:0000 01 00000000:00000000 00:00000000 00000000 1000 0 66666 1 0000000000000000 100 0 0 10 0
`
	ports := parseLinuxTCPListeningPorts(raw, map[string]bool{"55555": true, "66666": true})
	if len(ports) != 1 || ports[0].Port != 5173 || ports[0].Protocol != "tcp" || ports[0].Confidence != "detected" {
		t.Fatalf("ports = %#v, want one detected listen port 5173", ports)
	}
}

func TestSampleLinuxProcessMetricsReadsProcFixture(t *testing.T) {
	root := t.TempDir()
	originalRoot := procRoot
	originalClockTicks := linuxClockTicks
	procRoot = root
	linuxClockTicks = func() (int64, error) { return 100, nil }
	t.Cleanup(func() { procRoot = originalRoot; linuxClockTicks = originalClockTicks })
	pidDir := filepath.Join(root, "123")
	childDir := filepath.Join(root, "124")
	if err := os.MkdirAll(filepath.Join(pidDir, "fd"), 0o755); err != nil {
		t.Fatalf("mkdir fd: %v", err)
	}
	if err := os.MkdirAll(childDir, 0o755); err != nil {
		t.Fatalf("mkdir child: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(pidDir, "net"), 0o755); err != nil {
		t.Fatalf("mkdir net: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pidDir, "stat"), []byte("123 (agent worker) S 7 8 9 0 -1 4194560 10 20 30 40 50 60 70 80 20 0 1 0 100 4096 42 0 0 0"), 0o644); err != nil {
		t.Fatalf("write stat: %v", err)
	}
	if err := os.WriteFile(filepath.Join(childDir, "stat"), []byte("124 (agent child) S 123 8 9 0 -1 4194560 10 20 30 40 1 2 3 4 20 0 1 0 100 4096 2 0 0 0"), 0o644); err != nil {
		t.Fatalf("write child stat: %v", err)
	}
	if err := os.Symlink("socket:[55555]", filepath.Join(pidDir, "fd", "3")); err != nil {
		t.Fatalf("symlink socket fd: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pidDir, "net", "tcp"), []byte("  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode\n   0: 0100007F:1435 00000000:0000 0A 00000000:00000000 00:00000000 00000000 1000 0 55555 1 0000000000000000 100 0 0 10 0\n"), 0o644); err != nil {
		t.Fatalf("write tcp: %v", err)
	}
	exitCode := 0
	sample := sampleLinuxProcessMetrics(context.Background(), AgentRun{PID: 123, ExitCode: &exitCode})
	if !sample.PID.Known || sample.PID.Value == nil || *sample.PID.Value != 123 {
		t.Fatalf("pid sample = %#v, want known pid", sample.PID)
	}
	if !sample.CPUTimeMS.Known || sample.CPUTimeMS.Value == nil || *sample.CPUTimeMS.Value != 2600 {
		t.Fatalf("cpu sample = %#v, want 2600ms from fixture ticks", sample.CPUTimeMS)
	}
	if !sample.MemoryRSSBytesPeak.Known || sample.MemoryRSSBytesPeak.Value == nil || *sample.MemoryRSSBytesPeak.Value <= 0 {
		t.Fatalf("rss sample = %#v, want known rss bytes", sample.MemoryRSSBytesPeak)
	}
	if len(sample.Ports) != 1 || sample.Ports[0].Port != 5173 {
		t.Fatalf("ports = %#v, want detected port 5173", sample.Ports)
	}
	if !sample.ChildProcessesPeak.Known || sample.ChildProcessesPeak.Value == nil || *sample.ChildProcessesPeak.Value != 1 {
		t.Fatalf("child count = %#v, want one direct child", sample.ChildProcessesPeak)
	}
	if !sample.ExitCode.Known || sample.ExitCode.Value == nil || *sample.ExitCode.Value != 0 {
		t.Fatalf("exit sample = %#v, want known exit code 0", sample.ExitCode)
	}
}

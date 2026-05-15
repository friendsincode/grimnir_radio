/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package playout

import (
	"context"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// reaperInterval is how often the orphan reaper scans /proc. A leaked
// broadcast pipeline must be observed as untracked across two consecutive
// scans before it is killed, so the effective grace period for a brand-new
// pipeline that hasn't been registered with the Manager yet is reaperInterval.
const reaperInterval = 60 * time.Second

// StartOrphanReaper launches a goroutine that periodically scans for
// gst-launch-1.0 broadcast pipelines parented to this process that the
// Manager is no longer tracking, and kills them. Production evidence in
// issue #220 shows ~30 such orphans per hour on a busy station even after
// v1.40.1 and v1.40.2 — the audio echo is two pipelines feeding the same
// broadcast mount simultaneously. This is belt-and-suspenders cleanup; the
// upstream spawn path that creates the orphans still needs to be found.
//
// The reaper only kills processes whose cmdline contains
// "udpsink ... host=127.0.0.1", which is the unique signature of the
// per-track broadcast pipeline in pipeline.go. Crossfade decoders
// (fdsink fd=1), webstream relays (souphttpsrc), analyzer probes, and
// mediaengine pipelines have different signatures and are left alone.
func (m *Manager) StartOrphanReaper(ctx context.Context) {
	debug := os.Getenv("GRIMNIR_REAPER_DEBUG") == "1"
	m.logger.Info().Bool("debug", debug).Dur("interval", reaperInterval).Int("self_pid", os.Getpid()).Msg("orphan reaper started")
	go m.reaperLoop(ctx, debug)
}

func (m *Manager) reaperLoop(ctx context.Context, debug bool) {
	selfPID := os.Getpid()
	ticker := time.NewTicker(reaperInterval)
	defer ticker.Stop()

	var prev map[int]struct{}
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		owned := m.OwnedPIDs()
		candidates := scanBroadcastOrphans(selfPID, owned)

		if debug {
			// Per-scan snapshot. Tells us whether the reaper sees the pipelines
			// at all and whether OwnedPIDs is over-counting. Reproduces issue
			// #220's "kills never fire" symptom without needing a debugger.
			m.logger.Info().
				Int("owned_count", len(owned)).
				Ints("owned_pids", pidsSorted(owned)).
				Int("candidate_count", len(candidates)).
				Ints("candidate_pids", pidsSorted(candidates)).
				Int("prev_count", len(prev)).
				Msg("orphan reaper scan")
		}

		// Only kill PIDs that were also candidates in the previous scan. This
		// gives a full reaperInterval grace window between cmd.Start and the
		// moment Pipeline.cmd is assigned, so a brand-new tracked pipeline is
		// never killed.
		for pid := range candidates {
			if _, was := prev[pid]; !was {
				continue
			}
			// Re-check OwnedPIDs right before kill in case the pipeline was
			// registered between scan and kill (small window, but cheap to check).
			if _, isOwned := m.OwnedPIDs()[pid]; isOwned {
				continue
			}
			m.logger.Warn().Int("pid", pid).Msg("reaping orphan broadcast gst-launch pipeline")
			if err := syscall.Kill(pid, syscall.SIGKILL); err != nil {
				m.logger.Debug().Err(err).Int("pid", pid).Msg("orphan kill failed (likely already exited)")
			}
		}
		prev = candidates
	}
}

// pidsSorted returns the keys of a pid set as a deterministic sorted slice for
// log readability. Only used by the debug logger.
func pidsSorted(s map[int]struct{}) []int {
	out := make([]int, 0, len(s))
	for pid := range s {
		out = append(out, pid)
	}
	sortInts(out)
	return out
}

func sortInts(a []int) {
	// Manual insertion sort to avoid pulling in sort just for log formatting.
	for i := 1; i < len(a); i++ {
		v := a[i]
		j := i - 1
		for j >= 0 && a[j] > v {
			a[j+1] = a[j]
			j--
		}
		a[j+1] = v
	}
}

// OwnedPIDs returns the set of OS pids currently associated with
// Manager-tracked pipelines. A pid of 0 means the Pipeline has no live
// process and is excluded.
func (m *Manager) OwnedPIDs() map[int]struct{} {
	m.mu.Lock()
	defer m.mu.Unlock()
	owned := make(map[int]struct{}, len(m.pipelines))
	for _, p := range m.pipelines {
		if pid := p.CurrentPID(); pid > 0 {
			owned[pid] = struct{}{}
		}
	}
	return owned
}

// scanBroadcastOrphans returns the set of pids that are gst-launch-1.0
// children of selfPID, look like a broadcast pipeline, and are NOT in the
// owned set. Exposed at package level (rather than as a Manager method) so
// it can be unit-tested with a fake /proc layout — see reaper_test.go.
func scanBroadcastOrphans(selfPID int, owned map[int]struct{}) map[int]struct{} {
	return scanBroadcastOrphansIn("/proc", selfPID, owned)
}

func scanBroadcastOrphansIn(procRoot string, selfPID int, owned map[int]struct{}) map[int]struct{} {
	entries, err := os.ReadDir(procRoot)
	if err != nil {
		return nil
	}
	out := make(map[int]struct{})
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		base := procRoot + "/" + e.Name()

		comm, err := os.ReadFile(base + "/comm")
		if err != nil || strings.TrimSpace(string(comm)) != "gst-launch-1.0" {
			continue
		}

		ppid, ok := readPPID(base + "/stat")
		if !ok || ppid != selfPID {
			continue
		}

		if _, isOwned := owned[pid]; isOwned {
			continue
		}

		cmdline, err := os.ReadFile(base + "/cmdline")
		if err != nil {
			continue
		}
		// Per-track broadcast pipelines uniquely contain "udpsink" with the
		// loopback host. The host=127.0.0.1 check guards against accidentally
		// matching any future pipeline that uses udpsink to a remote target.
		c := string(cmdline)
		if !strings.Contains(c, "udpsink") || !strings.Contains(c, "host=127.0.0.1") {
			continue
		}

		out[pid] = struct{}{}
	}
	return out
}

// readPPID parses the ppid (field 4) from /proc/<pid>/stat. The comm field
// (field 2) is wrapped in parentheses and may contain spaces, so we anchor on
// the LAST ") " and parse from there.
func readPPID(statPath string) (int, bool) {
	data, err := os.ReadFile(statPath)
	if err != nil {
		return 0, false
	}
	idx := strings.LastIndex(string(data), ") ")
	if idx < 0 {
		return 0, false
	}
	fields := strings.Fields(string(data)[idx+2:])
	// After "(comm) ", fields are: state ppid pgrp session ...
	if len(fields) < 2 {
		return 0, false
	}
	ppid, err := strconv.Atoi(fields[1])
	if err != nil {
		return 0, false
	}
	return ppid, true
}

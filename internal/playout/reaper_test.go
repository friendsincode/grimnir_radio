/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package playout

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// fakeProc writes a minimal /proc/<pid> layout (comm, stat, cmdline) into root
// so scanBroadcastOrphansIn can be exercised without spawning real processes.
func fakeProc(t *testing.T, root string, pid, ppid int, comm, cmdline string) {
	t.Helper()
	dir := filepath.Join(root, strconv.Itoa(pid))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "comm"), []byte(comm+"\n"), 0o644); err != nil {
		t.Fatalf("write comm: %v", err)
	}
	// /proc/<pid>/stat format: pid (comm) state ppid pgrp session tty_nr ...
	// We only need fields up to ppid (field 4) to parse correctly.
	stat := strconv.Itoa(pid) + " (" + comm + ") S " + strconv.Itoa(ppid) + " 0 0 0 -1 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0\n"
	if err := os.WriteFile(filepath.Join(dir, "stat"), []byte(stat), 0o644); err != nil {
		t.Fatalf("write stat: %v", err)
	}
	// /proc/<pid>/cmdline uses NUL separators; for matching by strings.Contains
	// we just need the bytes present, NULs don't break substring matching.
	if err := os.WriteFile(filepath.Join(dir, "cmdline"), []byte(cmdline), 0o644); err != nil {
		t.Fatalf("write cmdline: %v", err)
	}
}

// broadcastCmdline returns a cmdline that mimics the per-track broadcast
// pipeline shape produced by pipeline.go (filesrc -> tee -> udpsink).
func broadcastCmdline() string {
	return "gst-launch-1.0\x00-e\x00filesrc location=/var/lib/grimnir/media/x.audio ! decodebin ! audioconvert ! tee name=t t. ! queue ! lamemp3enc ! fdsink fd=3 t. ! queue ! lamemp3enc ! fdsink fd=4 t. ! queue ! opusenc ! rtpopuspay ! udpsink host=127.0.0.1 port=5004"
}

func TestScanBroadcastOrphans_FlagsUntrackedBroadcastPipeline(t *testing.T) {
	root := t.TempDir()
	const selfPID = 100

	// PID 200: gst-launch-1.0, parented to us, broadcast shape — should flag.
	fakeProc(t, root, 200, selfPID, "gst-launch-1.0", broadcastCmdline())

	got := scanBroadcastOrphansIn(root, selfPID, map[int]struct{}{})
	if _, ok := got[200]; !ok {
		t.Fatalf("expected pid 200 in orphan set, got %v", got)
	}
	if len(got) != 1 {
		t.Fatalf("expected exactly one orphan, got %v", got)
	}
}

func TestScanBroadcastOrphans_SkipsOwnedPID(t *testing.T) {
	root := t.TempDir()
	const selfPID = 100
	fakeProc(t, root, 201, selfPID, "gst-launch-1.0", broadcastCmdline())

	owned := map[int]struct{}{201: {}}
	got := scanBroadcastOrphansIn(root, selfPID, owned)
	if _, ok := got[201]; ok {
		t.Fatalf("owned pid 201 must not be flagged as orphan: %v", got)
	}
}

func TestScanBroadcastOrphans_SkipsWebstreamRelay(t *testing.T) {
	root := t.TempDir()
	const selfPID = 100
	// Webstream relays use souphttpsrc and do NOT contain the udpsink loopback
	// signature; the reaper must leave them alone.
	relay := "gst-launch-1.0\x00-e\x00souphttpsrc location=https://example.com/live ! decodebin ! audioconvert ! lamemp3enc ! fdsink fd=3"
	fakeProc(t, root, 202, selfPID, "gst-launch-1.0", relay)

	got := scanBroadcastOrphansIn(root, selfPID, map[int]struct{}{})
	if _, ok := got[202]; ok {
		t.Fatalf("webstream relay must not be flagged: %v", got)
	}
}

func TestScanBroadcastOrphans_SkipsCrossfadeDecoder(t *testing.T) {
	root := t.TempDir()
	const selfPID = 100
	// Crossfade decoders write raw PCM to fdsink fd=1 and never reference udpsink.
	decoder := "gst-launch-1.0\x00-e\x00filesrc location=/var/lib/grimnir/media/x.mp3 ! decodebin ! audioconvert ! audioresample ! audio/x-raw,format=S16LE,rate=44100,channels=2 ! identity sync=true ! fdsink fd=1"
	fakeProc(t, root, 203, selfPID, "gst-launch-1.0", decoder)

	got := scanBroadcastOrphansIn(root, selfPID, map[int]struct{}{})
	if _, ok := got[203]; ok {
		t.Fatalf("crossfade decoder must not be flagged: %v", got)
	}
}

func TestScanBroadcastOrphans_SkipsForeignParent(t *testing.T) {
	root := t.TempDir()
	const selfPID = 100
	// gst-launch parented to something other than us (some other process in
	// the same pid namespace) must not be touched.
	fakeProc(t, root, 204 /*ppid*/, 999, "gst-launch-1.0", broadcastCmdline())

	got := scanBroadcastOrphansIn(root, selfPID, map[int]struct{}{})
	if _, ok := got[204]; ok {
		t.Fatalf("pid with foreign parent must not be flagged: %v", got)
	}
}

func TestScanBroadcastOrphans_SkipsNonGstProcesses(t *testing.T) {
	root := t.TempDir()
	const selfPID = 100
	// An ffmpeg process parented to us with udpsink-ish args still shouldn't
	// match because comm != "gst-launch-1.0".
	fakeProc(t, root, 205, selfPID, "ffmpeg", "ffmpeg\x00-i\x00x.mp3\x00udpsink host=127.0.0.1")

	got := scanBroadcastOrphansIn(root, selfPID, map[int]struct{}{})
	if _, ok := got[205]; ok {
		t.Fatalf("ffmpeg must not be flagged by gst-launch reaper: %v", got)
	}
}

func TestReadPPID_HandlesCommWithSpacesAndParens(t *testing.T) {
	// /proc/PID/stat's comm field can contain spaces and parens, e.g. a
	// process named "weird ) process". readPPID must anchor on the LAST ") ".
	dir := t.TempDir()
	statPath := filepath.Join(dir, "stat")
	if err := os.WriteFile(statPath, []byte("123 (weird ) process) S 42 0 0 0 -1 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	ppid, ok := readPPID(statPath)
	if !ok || ppid != 42 {
		t.Fatalf("readPPID = (%d, %v), want (42, true)", ppid, ok)
	}
}

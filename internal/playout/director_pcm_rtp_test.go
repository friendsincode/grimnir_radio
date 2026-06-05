/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package playout

import (
	"strings"
	"testing"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

// TestBuildDualBroadcastPipeline_NoHAModeOmitsRTPBranch verifies the
// single-instance default: when HAPCMRTPEnabled is false the pipeline must not
// contain the rtpL16pay / multiudpsink branch at all. This preserves the
// pre-edge-encoder behavior for everyone who isn't running the HA topology.
func TestBuildDualBroadcastPipeline_NoHAModeOmitsRTPBranch(t *testing.T) {
	d, _ := newMockDirector(t)
	d.cfg.HAPCMRTPEnabled = false
	d.cfg.HAPCMRTPTargets = nil

	mount := models.Mount{
		Name:       "test",
		Format:     "mp3",
		Bitrate:    128,
		SampleRate: 44100,
		Channels:   2,
	}
	seekFile, pipeline, err := d.buildDualBroadcastPipeline("/some/file.mp3", mount, 128, 64, 0, 0, 0)
	if err != nil {
		t.Fatalf("buildDualBroadcastPipeline returned error: %v", err)
	}
	if seekFile != nil {
		seekFile.Close()
	}

	if strings.Contains(pipeline, "rtpL16pay") {
		t.Errorf("HA disabled but pipeline contains rtpL16pay:\n%s", pipeline)
	}
	if strings.Contains(pipeline, "multiudpsink") {
		t.Errorf("HA disabled but pipeline contains multiudpsink:\n%s", pipeline)
	}
}

// TestBuildDualBroadcastPipeline_HAModeAddsRTPBranch verifies the additive
// PCM-over-RTP tee branch appears when HA mode is enabled, with the legacy
// HQ/LQ fdsink outputs still intact.
func TestBuildDualBroadcastPipeline_HAModeAddsRTPBranch(t *testing.T) {
	d, _ := newMockDirector(t)
	d.cfg.HAPCMRTPEnabled = true
	d.cfg.HAPCMRTPTargets = []string{"<node-a-ip>:5004", "<node-b-ip>:5004"}

	mount := models.Mount{
		Name:       "test",
		Format:     "mp3",
		Bitrate:    128,
		SampleRate: 44100,
		Channels:   2,
	}
	seekFile, pipeline, err := d.buildDualBroadcastPipeline("/some/file.mp3", mount, 128, 64, 0, 0, 0)
	if err != nil {
		t.Fatalf("buildDualBroadcastPipeline returned error: %v", err)
	}
	if seekFile != nil {
		seekFile.Close()
	}

	for _, expected := range []string{
		"rtpL16pay",
		"multiudpsink",
		"format=S16BE",
		"clients=<node-a-ip>:5004,<node-b-ip>:5004",
	} {
		if !strings.Contains(pipeline, expected) {
			t.Errorf("HA enabled but pipeline missing %q in:\n%s", expected, pipeline)
		}
	}

	for _, legacy := range []string{
		"fdsink fd=3",
		"fdsink fd=4",
		"lamemp3enc",
	} {
		if !strings.Contains(pipeline, legacy) {
			t.Errorf("HA enabled but legacy output missing %q in:\n%s", legacy, pipeline)
		}
	}
}

// TestBuildDualBroadcastPipeline_HAEnabledButNoTargetsOmitsRTPBranch covers
// the safety check: if the operator flips the flag on but forgets to populate
// targets, the pipeline must not emit a malformed multiudpsink (which would
// fail to link). The config loader already rejects this combo at startup but
// the builder defends in depth.
func TestBuildDualBroadcastPipeline_HAEnabledButNoTargetsOmitsRTPBranch(t *testing.T) {
	d, _ := newMockDirector(t)
	d.cfg.HAPCMRTPEnabled = true
	d.cfg.HAPCMRTPTargets = nil

	mount := models.Mount{Name: "test", Format: "mp3", SampleRate: 44100, Channels: 2}
	seekFile, pipeline, err := d.buildDualBroadcastPipeline("/some/file.mp3", mount, 128, 64, 0, 0, 0)
	if err != nil {
		t.Fatalf("buildDualBroadcastPipeline returned error: %v", err)
	}
	if seekFile != nil {
		seekFile.Close()
	}

	if strings.Contains(pipeline, "rtpL16pay") || strings.Contains(pipeline, "multiudpsink") {
		t.Errorf("HA enabled with no targets must not emit RTP branch:\n%s", pipeline)
	}
}

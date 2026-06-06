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

// buildWebstreamPipelineForTest wraps buildWebstreamBroadcastPipeline with
// canonical args so the HA tests below stay focused on the tee branch.
func buildWebstreamPipelineForTest(t *testing.T, d *Director) string {
	t.Helper()
	mount := models.Mount{
		Name:       "test",
		Format:     "mp3",
		Bitrate:    128,
		SampleRate: 44100,
		Channels:   2,
	}
	ws := &models.Webstream{
		Name: "test-relay",
		URLs: []string{"http://upstream.example.com/stream.mp3"},
	}
	pipeline, err := d.buildWebstreamBroadcastPipeline("http://upstream.example.com/stream.mp3", mount, ws, 128, 64, 0)
	if err != nil {
		t.Fatalf("buildWebstreamBroadcastPipeline returned error: %v", err)
	}
	return pipeline
}

// TestBuildWebstreamBroadcastPipeline_NoHAModeOmitsRTPBranch mirrors the
// dual-pipeline test: default single-instance webstream relays must not emit
// the PCM-RTP tee branch.
func TestBuildWebstreamBroadcastPipeline_NoHAModeOmitsRTPBranch(t *testing.T) {
	d, _ := newMockDirector(t)
	d.cfg.HAPCMRTPEnabled = false
	pipeline := buildWebstreamPipelineForTest(t, d)
	if strings.Contains(pipeline, "rtpL16pay") {
		t.Errorf("HA disabled but pipeline contains rtpL16pay:\n%s", pipeline)
	}
	if strings.Contains(pipeline, "multiudpsink") {
		t.Errorf("HA disabled but pipeline contains multiudpsink:\n%s", pipeline)
	}
}

// TestBuildDualBroadcastPipeline_LiveInputDisabledOmitsMixer verifies the
// default: when LiveInputEnabled is false the pipeline must not contain the
// audiomixer or live-input udpsrc branch. Single-instance shape preserved.
func TestBuildDualBroadcastPipeline_LiveInputDisabledOmitsMixer(t *testing.T) {
	d, _ := newMockDirector(t)
	d.cfg.LiveInputEnabled = false

	mount := models.Mount{Name: "test", Format: "mp3", SampleRate: 44100, Channels: 2}
	seekFile, pipeline, err := d.buildDualBroadcastPipeline("/some/file.mp3", mount, 128, 64, 0, 0, 0)
	if err != nil {
		t.Fatalf("buildDualBroadcastPipeline returned error: %v", err)
	}
	if seekFile != nil {
		seekFile.Close()
	}

	for _, banned := range []string{"audiomixer", "rtpL16depay", "rtpjitterbuffer"} {
		if strings.Contains(pipeline, banned) {
			t.Errorf("LiveInput disabled but pipeline contains %q:\n%s", banned, pipeline)
		}
	}
}

// TestBuildDualBroadcastPipeline_LiveInputEnabledAddsMixerBranch verifies the
// audiomixer + udpsrc/rtpL16depay branch appears when LiveInputEnabled is
// true. Scheduled content lands on mixer.sink_0; the live RTP feed lands on
// mixer.sink_1. The tee then fans the mixed output to HQ / LQ / (optional) HA.
func TestBuildDualBroadcastPipeline_LiveInputEnabledAddsMixerBranch(t *testing.T) {
	d, _ := newMockDirector(t)
	d.cfg.LiveInputEnabled = true
	d.cfg.LiveInputPort = 5008
	d.cfg.LiveInputFanoutAddr = "10.10.0.7:9100"

	mount := models.Mount{Name: "test", Format: "mp3", SampleRate: 44100, Channels: 2}
	seekFile, pipeline, err := d.buildDualBroadcastPipeline("/some/file.mp3", mount, 128, 64, 0, 0, 0)
	if err != nil {
		t.Fatalf("buildDualBroadcastPipeline returned error: %v", err)
	}
	if seekFile != nil {
		seekFile.Close()
	}

	for _, expected := range []string{
		"audiomixer name=mix",
		"udpsrc port=5008",
		"rtpjitterbuffer",
		"rtpL16depay",
		"mix.sink_1",
	} {
		if !strings.Contains(pipeline, expected) {
			t.Errorf("LiveInput enabled but pipeline missing %q in:\n%s", expected, pipeline)
		}
	}

	// HQ + LQ outputs must still be present.
	for _, legacy := range []string{"fdsink fd=3", "fdsink fd=4", "lamemp3enc"} {
		if !strings.Contains(pipeline, legacy) {
			t.Errorf("LiveInput enabled but legacy output missing %q in:\n%s", legacy, pipeline)
		}
	}
}

// TestBuildWebstreamBroadcastPipeline_LiveInputEnabledAddsMixerBranch mirrors
// the file-playout case for relayed webstreams.
func TestBuildWebstreamBroadcastPipeline_LiveInputEnabledAddsMixerBranch(t *testing.T) {
	d, _ := newMockDirector(t)
	d.cfg.LiveInputEnabled = true
	d.cfg.LiveInputPort = 5008
	d.cfg.LiveInputFanoutAddr = "10.10.0.7:9100"

	pipeline := buildWebstreamPipelineForTest(t, d)
	for _, expected := range []string{
		"audiomixer name=mix",
		"udpsrc port=5008",
		"rtpjitterbuffer",
		"rtpL16depay",
		"mix.sink_1",
	} {
		if !strings.Contains(pipeline, expected) {
			t.Errorf("LiveInput enabled but webstream pipeline missing %q in:\n%s", expected, pipeline)
		}
	}
}

// TestBuildWebstreamBroadcastPipeline_HAModeAddsRTPBranch verifies the
// additive PCM-over-RTP tee branch appears for webstream relays when HA mode
// is enabled, matching buildDualBroadcastPipeline's behavior.
func TestBuildWebstreamBroadcastPipeline_HAModeAddsRTPBranch(t *testing.T) {
	d, _ := newMockDirector(t)
	d.cfg.HAPCMRTPEnabled = true
	d.cfg.HAPCMRTPTargets = []string{"<node-a-ip>:5004"}
	pipeline := buildWebstreamPipelineForTest(t, d)
	for _, expected := range []string{
		"rtpL16pay",
		"multiudpsink",
		"format=S16BE",
		"clients=<node-a-ip>:5004",
	} {
		if !strings.Contains(pipeline, expected) {
			t.Errorf("HA enabled but pipeline missing %q in:\n%s", expected, pipeline)
		}
	}
}

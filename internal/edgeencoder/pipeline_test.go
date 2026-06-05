/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package edgeencoder

import (
	"testing"
)

func TestNewPipeline_BuildsAndReachesPlayingState(t *testing.T) {
	Init()
	cfg := &Config{
		RTPPortA:          15004,
		RTPPortB:          15005,
		OutputFormat:      "mp3",
		OutputBitrateKbps: 128,
	}
	p, err := NewPipeline(cfg)
	if err != nil {
		t.Fatalf("NewPipeline: %v", err)
	}
	defer p.Close()

	if err := p.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if p.ActiveInput() != "A" {
		t.Errorf("ActiveInput initial = %q, want A", p.ActiveInput())
	}

	if err := p.SetActiveInput("B"); err != nil {
		t.Fatalf("SetActiveInput(B): %v", err)
	}
	if p.ActiveInput() != "B" {
		t.Errorf("ActiveInput after switch = %q, want B", p.ActiveInput())
	}

	if err := p.SetActiveInput("A"); err != nil {
		t.Fatalf("SetActiveInput(A): %v", err)
	}
	if p.ActiveInput() != "A" {
		t.Errorf("ActiveInput after switch back = %q, want A", p.ActiveInput())
	}

	if err := p.SetActiveInput("Z"); err == nil {
		t.Error("SetActiveInput(Z): want error, got nil")
	}
}

func TestNewPipeline_AppsinkExists(t *testing.T) {
	Init()
	cfg := &Config{
		RTPPortA:          15006,
		RTPPortB:          15007,
		OutputFormat:      "mp3",
		OutputBitrateKbps: 128,
	}
	p, err := NewPipeline(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if p.MP3Appsink() == nil {
		t.Error("MP3Appsink() = nil, want non-nil")
	}
}

func TestNewPipeline_InputPadAccessor(t *testing.T) {
	Init()
	cfg := &Config{
		RTPPortA:          15008,
		RTPPortB:          15009,
		OutputFormat:      "mp3",
		OutputBitrateKbps: 128,
	}
	p, err := NewPipeline(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if p.InputPad("A") == nil {
		t.Error("InputPad(A) = nil")
	}
	if p.InputPad("B") == nil {
		t.Error("InputPad(B) = nil")
	}
	if p.InputPad("Z") != nil {
		t.Error("InputPad(Z) should be nil for unknown input")
	}
}

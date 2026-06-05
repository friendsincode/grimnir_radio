/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package edgeencoder

import (
	"testing"
	"time"
)

func TestInputHealth_InitiallyUnhealthy(t *testing.T) {
	ih := NewInputHealth(100 * time.Millisecond)
	if ih.IsHealthy() {
		t.Error("new InputHealth claims healthy; want unhealthy until first packet")
	}
}

func TestInputHealth_HealthyAfterPacket(t *testing.T) {
	ih := NewInputHealth(100 * time.Millisecond)
	ih.RecordPacket()
	if !ih.IsHealthy() {
		t.Error("InputHealth after RecordPacket: unhealthy, want healthy")
	}
}

func TestInputHealth_StaleAfterWindow(t *testing.T) {
	ih := NewInputHealth(50 * time.Millisecond)
	ih.RecordPacket()
	time.Sleep(75 * time.Millisecond)
	if ih.IsHealthy() {
		t.Error("InputHealth after window elapsed: healthy, want unhealthy")
	}
}

func TestInputHealth_GRPCGate(t *testing.T) {
	ih := NewInputHealth(100 * time.Millisecond)
	ih.RecordPacket()
	if !ih.IsHealthy() {
		t.Fatal("packets present but unhealthy")
	}
	ih.SetGRPCHealthy(false)
	if ih.IsHealthy() {
		t.Error("gRPC unhealthy override ignored; want unhealthy")
	}
	ih.SetGRPCHealthy(true)
	if !ih.IsHealthy() {
		t.Error("gRPC restored + packets present: unhealthy, want healthy")
	}
}

func TestInputHealth_DefaultGRPCGateIsTrue(t *testing.T) {
	ih := NewInputHealth(100 * time.Millisecond)
	ih.RecordPacket()
	if !ih.IsHealthy() {
		t.Error("packets present + default gRPC gate: unhealthy, want healthy")
	}
}

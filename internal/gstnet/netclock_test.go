/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package gstnet

import (
	"testing"
	"time"

	"github.com/go-gst/go-gst/gst"
)

func TestNewNetTimeProvider_StartsAndCloses(t *testing.T) {
	gst.Init(nil)
	sysClock := gst.ObtainSystemClock()
	if sysClock == nil {
		t.Fatal("ObtainSystemClock returned nil")
	}
	defer sysClock.Unref()

	p := NewNetTimeProvider(sysClock.Clock, "127.0.0.1", 19094)
	if p == nil {
		t.Fatal("NewNetTimeProvider returned nil")
	}
	if err := p.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestNetClientClock_SyncsToLocalServer(t *testing.T) {
	gst.Init(nil)

	srvSysClock := gst.ObtainSystemClock()
	if srvSysClock == nil {
		t.Fatal("ObtainSystemClock returned nil")
	}
	defer srvSysClock.Unref()

	provider := NewNetTimeProvider(srvSysClock.Clock, "127.0.0.1", 19095)
	if provider == nil {
		t.Fatal("provider nil")
	}
	defer provider.Close()

	client := NewNetClientClock("test-client", "127.0.0.1", 19095)
	if client == nil {
		t.Fatal("client nil")
	}

	start := time.Now()
	synced := client.WaitForSync(3 * time.Second)
	elapsed := time.Since(start)
	t.Logf("WaitForSync returned %v after %s", synced, elapsed)
	if !synced {
		t.Error("WaitForSync returned false; client never synced to local provider")
	}
}

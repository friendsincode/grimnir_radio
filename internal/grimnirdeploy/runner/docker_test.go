/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package runner

import (
	"context"
	"strings"
	"testing"
)

func TestDockerPullUsesGrimnirWrapper(t *testing.T) {
	f := NewFake()
	f.SetResponsePrefix("cd /srv/docker/grimnir_radio && ./grimnir pull", "pulled\n", "", 0, nil)
	d := NewDockerCompose(f, "/srv/docker/grimnir_radio")
	if err := d.Pull(context.Background(), "node-1"); err != nil {
		t.Fatalf("Pull: %v", err)
	}
	if len(f.Calls) != 1 || !strings.Contains(f.Calls[0].Cmd, "./grimnir pull") {
		t.Errorf("Calls[0] = %+v", f.Calls[0])
	}
}

func TestDockerUpAndStop(t *testing.T) {
	f := NewFake()
	d := NewDockerCompose(f, "/srv/docker/grimnir_radio")
	_ = d.Up(context.Background(), "node-1")
	_ = d.Stop(context.Background(), "node-1", "grimnir")
	if len(f.Calls) != 2 {
		t.Fatalf("Calls = %d, want 2", len(f.Calls))
	}
	if !strings.Contains(f.Calls[0].Cmd, "./grimnir up -d") {
		t.Errorf("Calls[0].Cmd = %q", f.Calls[0].Cmd)
	}
	if !strings.Contains(f.Calls[1].Cmd, "stop grimnir") {
		t.Errorf("Calls[1].Cmd = %q", f.Calls[1].Cmd)
	}
}

func TestDockerCurrentImageTag(t *testing.T) {
	f := NewFake()
	f.SetResponsePrefix("docker inspect --format", "v1.40.7\n", "", 0, nil)
	d := NewDockerCompose(f, "/srv/docker/grimnir_radio")
	tag, err := d.CurrentTag(context.Background(), "node-1", "grimnir-radio")
	if err != nil {
		t.Fatalf("CurrentTag: %v", err)
	}
	if tag != "v1.40.7" {
		t.Errorf("CurrentTag = %q, want v1.40.7", tag)
	}
}

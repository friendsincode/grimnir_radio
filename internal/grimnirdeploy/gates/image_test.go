/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package gates

import (
	"context"
	"testing"

	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/runner"
)

func TestImageGatePassesWhenManifestExists(t *testing.T) {
	f := runner.NewFake()
	f.SetResponsePrefix("docker manifest inspect", "{}", "", 0, nil)
	g := NewImageGate(f, []string{"local", "node-2"}, "ghcr.io/friendsincode/grimnir-radio:v1.0.0")
	if err := g.Evaluate(context.Background()); err != nil {
		t.Errorf("manifest exists; got %v", err)
	}
}

func TestImageGateAbortsWhenManifestMissing(t *testing.T) {
	f := runner.NewFake()
	f.SetResponsePrefix("docker manifest inspect", "", "no such manifest", 1, nil)
	g := NewImageGate(f, []string{"local", "node-2"}, "ghcr.io/x:bad")
	if !IsAborted(g.Evaluate(context.Background())) {
		t.Error("missing manifest should abort")
	}
}

/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package gates

import (
	"context"
	"fmt"

	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/runner"
)

// ImageGate verifies the target image manifest is reachable from every node
// in the cluster (i.e., the registry is up and the tag exists).
type ImageGate struct {
	R     runner.Runner
	Hosts []string
	Image string // full image ref including tag
}

// NewImageGate constructs an ImageGate.
func NewImageGate(r runner.Runner, hosts []string, image string) *ImageGate {
	return &ImageGate{R: r, Hosts: hosts, Image: image}
}

// Name returns the gate identifier.
func (g *ImageGate) Name() string { return "image-exists" }

// Evaluate runs `docker manifest inspect` on every host and aborts on the
// first miss.
func (g *ImageGate) Evaluate(ctx context.Context) error {
	for _, h := range g.Hosts {
		_, stderr, code, err := g.R.Run(ctx, h, fmt.Sprintf("docker manifest inspect %s", g.Image))
		if err != nil {
			return fmt.Errorf("manifest probe %s: %w", h, err)
		}
		if code != 0 {
			return &Aborted{Gate: g.Name(), Reason: fmt.Sprintf("image %s missing on %s: %s",
				g.Image, h, stderr)}
		}
	}
	return nil
}

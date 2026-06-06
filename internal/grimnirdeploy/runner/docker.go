/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package runner

import (
	"context"
	"fmt"
	"strings"
)

// DockerCompose wraps the ./grimnir compose-wrapper script on a target host.
// Per CLAUDE.md, production must use ./grimnir rather than direct docker
// compose to get the correct compose-file ordering.
type DockerCompose struct {
	r       Runner
	dir     string // e.g., /srv/docker/grimnir_radio
	wrapper string // default "./grimnir"
}

// NewDockerCompose constructs a DockerCompose helper bound to a Runner.
func NewDockerCompose(r Runner, dir string) *DockerCompose {
	return &DockerCompose{r: r, dir: dir, wrapper: "./grimnir"}
}

// Pull pulls latest images via the wrapper.
func (d *DockerCompose) Pull(ctx context.Context, host string) error {
	cmd := fmt.Sprintf("cd %s && %s pull", d.dir, d.wrapper)
	_, stderr, code, err := d.r.Run(ctx, host, cmd)
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("docker pull exit %d: %s", code, stderr)
	}
	return nil
}

// Up starts all services via the wrapper (./grimnir up -d).
func (d *DockerCompose) Up(ctx context.Context, host string) error {
	cmd := fmt.Sprintf("cd %s && %s up -d", d.dir, d.wrapper)
	_, stderr, code, err := d.r.Run(ctx, host, cmd)
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("docker up exit %d: %s", code, stderr)
	}
	return nil
}

// Down stops all services.
func (d *DockerCompose) Down(ctx context.Context, host string) error {
	cmd := fmt.Sprintf("cd %s && %s down", d.dir, d.wrapper)
	_, stderr, code, err := d.r.Run(ctx, host, cmd)
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("docker down exit %d: %s", code, stderr)
	}
	return nil
}

// Stop stops a single service (matches `docker compose stop <svc>`).
func (d *DockerCompose) Stop(ctx context.Context, host, service string) error {
	cmd := fmt.Sprintf("cd %s && docker compose stop %s", d.dir, service)
	_, stderr, code, err := d.r.Run(ctx, host, cmd)
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("docker stop %s exit %d: %s", service, code, stderr)
	}
	return nil
}

// CurrentTag returns the image tag currently running for the named container
// on the target host. Empty string + nil if no such container.
func (d *DockerCompose) CurrentTag(ctx context.Context, host, container string) (string, error) {
	cmd := fmt.Sprintf("docker inspect --format='{{ .Config.Image }}' %s", container)
	out, _, code, err := d.r.Run(ctx, host, cmd)
	if err != nil || code != 0 {
		return "", err
	}
	image := strings.TrimSpace(out)
	if idx := strings.LastIndex(image, ":"); idx >= 0 {
		return image[idx+1:], nil
	}
	return image, nil
}

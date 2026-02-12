/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

// Package version provides version information and update checking.
package version

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

// Version is the current version of Grimnir Radio.
// This is set at build time via ldflags:
//
//	-X github.com/friendsincode/grimnir_radio/internal/version.Version=X.Y.Z
var Version = "1.15.42"

// GitHubRepo is the repository to check for updates
const GitHubRepo = "friendsincode/grimnir_radio"

// UpdateInfo contains information about available updates.
type UpdateInfo struct {
	CurrentVersion  string
	LatestVersion   string
	UpdateAvailable bool
	ReleaseURL      string
	ReleaseNotes    string
	CheckedAt       time.Time
}

// Checker periodically checks for updates.
type Checker struct {
	mu          sync.RWMutex
	info        *UpdateInfo
	logger      zerolog.Logger
	checkPeriod time.Duration
	httpClient  *http.Client
	cancel      context.CancelFunc
}

// GitHubRelease represents a GitHub release API response.
type GitHubRelease struct {
	TagName     string `json:"tag_name"`
	HTMLURL     string `json:"html_url"`
	Body        string `json:"body"`
	PublishedAt string `json:"published_at"`
}

// NewChecker creates a new update checker.
func NewChecker(logger zerolog.Logger) *Checker {
	return &Checker{
		logger:      logger.With().Str("component", "update-checker").Logger(),
		checkPeriod: 6 * time.Hour, // Check every 6 hours
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		info: &UpdateInfo{
			CurrentVersion: Version,
		},
	}
}

// Start begins periodic update checking.
func (c *Checker) Start(ctx context.Context) {
	ctx, c.cancel = context.WithCancel(ctx)

	// Check immediately on startup
	c.check(ctx)

	// Start periodic checking
	go func() {
		ticker := time.NewTicker(c.checkPeriod)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				c.check(ctx)
			}
		}
	}()
}

// Stop stops the update checker.
func (c *Checker) Stop() {
	if c.cancel != nil {
		c.cancel()
	}
}

// Info returns the current update information.
func (c *Checker) Info() *UpdateInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.info == nil {
		return &UpdateInfo{CurrentVersion: Version}
	}
	return c.info
}

// check fetches the latest release from GitHub.
func (c *Checker) check(ctx context.Context) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", GitHubRepo)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		c.logger.Debug().Err(err).Msg("failed to create request")
		return
	}

	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "Grimnir-Radio/"+Version)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.logger.Debug().Err(err).Msg("failed to fetch releases")
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		c.logger.Debug().Int("status", resp.StatusCode).Msg("unexpected status from GitHub")
		return
	}

	var release GitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		c.logger.Debug().Err(err).Msg("failed to decode release")
		return
	}

	latestVersion := strings.TrimPrefix(release.TagName, "v")

	c.mu.Lock()
	c.info = &UpdateInfo{
		CurrentVersion:  Version,
		LatestVersion:   latestVersion,
		UpdateAvailable: compareVersions(Version, latestVersion) < 0,
		ReleaseURL:      release.HTMLURL,
		ReleaseNotes:    truncateNotes(release.Body, 200),
		CheckedAt:       time.Now(),
	}
	c.mu.Unlock()

	if c.info.UpdateAvailable {
		c.logger.Info().
			Str("current", Version).
			Str("latest", latestVersion).
			Str("url", release.HTMLURL).
			Msg("new version available")
	}
}

// compareVersions compares two semver versions.
// Returns -1 if a < b, 0 if a == b, 1 if a > b
func compareVersions(a, b string) int {
	aParts := parseVersion(a)
	bParts := parseVersion(b)

	for i := 0; i < 3; i++ {
		if aParts[i] < bParts[i] {
			return -1
		}
		if aParts[i] > bParts[i] {
			return 1
		}
	}
	return 0
}

// parseVersion parses a semver string into major, minor, patch.
func parseVersion(v string) [3]int {
	v = strings.TrimPrefix(v, "v")
	parts := strings.Split(v, ".")

	var result [3]int
	for i := 0; i < len(parts) && i < 3; i++ {
		fmt.Sscanf(parts[i], "%d", &result[i])
	}
	return result
}

// truncateNotes truncates release notes to a maximum length.
func truncateNotes(s string, maxLen int) string {
	// Take only the first line or first N characters
	lines := strings.SplitN(s, "\n", 2)
	s = strings.TrimSpace(lines[0])
	if len(s) > maxLen {
		return s[:maxLen-3] + "..."
	}
	return s
}

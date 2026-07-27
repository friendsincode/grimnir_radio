/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package webstream

import (
	"fmt"
	"net/url"
	"strings"
)

// MountNameFromLiveURL returns the mount name a /live/ URL targets, or "" when
// the URL is not a /live/ path at all.
func MountNameFromLiveURL(sourceURL string) string {
	u, err := url.Parse(sourceURL)
	if err != nil || !strings.HasPrefix(u.Path, "/live/") {
		return ""
	}
	return strings.TrimPrefix(u.Path, "/live/")
}

// LoopbackURL rewrites a URL that points at one of this instance's own /live/
// mounts into a loopback URL on the local HTTP port, and reports whether it
// rewrote anything.
//
// Most of this deployment's webstreams are cross-station syndication: station B
// relays station A's mount by its PUBLIC https URL even though both live on
// this same box. Resolving that publicly sends the request out to the CDN edge
// and back over the tunnel to fetch audio already in memory here, which makes
// an internal relay depend on the least reliable link in the system. Loopback
// never leaves the box.
//
// isLocalMount reports whether a mount name is served by this instance; a nil
// predicate, an unparseable URL, a non-/live/ path, or an unknown mount all
// leave the URL untouched, so genuine external relays are never redirected.
func LoopbackURL(sourceURL string, port int, isLocalMount func(string) bool) (string, bool) {
	if isLocalMount == nil {
		return sourceURL, false
	}
	mountName := MountNameFromLiveURL(sourceURL)
	if mountName == "" || !isLocalMount(mountName) {
		return sourceURL, false
	}
	if port <= 0 {
		port = 8080
	}
	return fmt.Sprintf("http://127.0.0.1:%d/live/%s", port, mountName), true
}

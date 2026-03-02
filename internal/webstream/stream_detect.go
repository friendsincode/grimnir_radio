/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package webstream

import (
	"net/url"
	"strings"
)

const (
	StreamTypeHLS = "hls"
	StreamTypeICY = "icy"
)

// DetectStreamType returns StreamTypeHLS for HLS playlist URLs and
// StreamTypeICY for everything else (Icecast/SHOUTcast).
func DetectStreamType(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return StreamTypeICY
	}
	path := strings.ToLower(u.Path)
	if strings.HasSuffix(path, ".m3u8") {
		return StreamTypeHLS
	}
	return StreamTypeICY
}

/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package harbor

import "net/http"

// parseIceHeaders extracts Icecast metadata headers from a source connection request.
func parseIceHeaders(r *http.Request) map[string]string {
	headers := map[string]string{}
	interesting := []string{
		"Ice-Name",
		"Ice-Description",
		"Ice-Genre",
		"Ice-Bitrate",
		"Ice-URL",
		"Ice-Public",
		"Content-Type",
		"User-Agent",
	}
	for _, key := range interesting {
		if val := r.Header.Get(key); val != "" {
			headers[key] = val
		}
	}
	return headers
}

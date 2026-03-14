/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package webstream

import "context"

// MetadataPoller is implemented by ICYPoller and HLSPoller so the director
// can manage either kind of stream metadata poller interchangeably.
type MetadataPoller interface {
	// Start begins the background polling loop. Blocks until ctx is cancelled.
	Start(ctx context.Context)
	Stop()
	SetURL(url string)
	// FetchOnce performs a single synchronous metadata fetch and returns the
	// result. It does not update internal state or publish events.
	FetchOnce(ctx context.Context) (title, artist string, err error)
}

/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package webstream

import "context"

// MetadataPoller is implemented by ICYPoller and HLSPoller so the director
// can manage either kind of stream metadata poller interchangeably.
type MetadataPoller interface {
	Start(ctx context.Context)
	Stop()
	SetURL(url string)
}

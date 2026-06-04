/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

// Package edgeencoder ingests PCM-over-RTP from media engines, performs
// sample-aligned input switching, and serves the result to listeners via
// HTTP/ICY and HLS. This package is the core of the cmd/edge-encoder binary.
//
// Unlike the rest of the grimnir codebase, edgeencoder drives GStreamer via
// CGo (go-gst) rather than gst-launch subprocess. This is deliberate: input
// switching requires runtime property changes on a running pipeline, which
// gst-launch cannot provide. See docs/superpowers/plans/2026-06-03-edge-encoder-pcm-transport.md.
package edgeencoder

import (
	"sync"

	"github.com/go-gst/go-gst/gst"
)

var initOnce sync.Once

// Init initializes the GStreamer library. It is idempotent and safe to call
// multiple times; only the first call performs initialization.
func Init() {
	initOnce.Do(func() {
		gst.Init(nil)
	})
}

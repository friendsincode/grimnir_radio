/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package edgeencoder

import (
	"strings"
	"testing"

	"github.com/go-gst/go-gst/gst"
)

func TestGstInit(t *testing.T) {
	// Init() must be idempotent and not panic.
	Init()
	Init()

	major, minor, micro := gst.VersionMajor, gst.VersionMinor, gst.VersionMicro
	t.Logf("GStreamer %d.%d.%d", major, minor, micro)

	if major < 1 || (major == 1 && minor < 20) {
		t.Fatalf("GStreamer version %d.%d below required 1.20", major, minor)
	}
}

func TestGstRequiredElements(t *testing.T) {
	Init()
	required := []string{
		"udpsrc",
		"rtpjitterbuffer",
		"rtpL16depay",
		"input-selector",
		"audioconvert",
		"audioresample",
		"lamemp3enc",
		"hlssink2",
		"appsink",
	}
	for _, name := range required {
		t.Run(name, func(t *testing.T) {
			elt, err := gst.NewElement(name)
			if err != nil {
				if strings.Contains(err.Error(), "no element") {
					t.Fatalf("element %q not available; install the missing plugin pack", name)
				}
				t.Fatalf("creating %q: %v", name, err)
			}
			if elt == nil {
				t.Fatalf("element %q created nil with no error", name)
			}
		})
	}
}

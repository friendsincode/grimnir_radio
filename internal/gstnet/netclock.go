/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

// Package gstnet provides minimal CGo wrappers for the GStreamer network
// clock subsystem (libgstnet-1.0). go-gst v1.4.0 doesn't include these
// bindings, so we wrap them ourselves. Used by internal/playout for
// NetClock master/slave PCM alignment in HA mode.
package gstnet

/*
#cgo pkg-config: gstreamer-net-1.0
#include <gst/net/gstnet.h>
#include <gst/gstclock.h>
#include <glib-object.h>
#include <stdlib.h>
*/
import "C"

import (
	"time"
	"unsafe"

	"github.com/go-gst/go-gst/gst"
)

// NetTimeProvider exposes a GstClock as a TCP service that GstNetClientClocks
// can dial to synchronize.
type NetTimeProvider struct {
	ptr *C.GstNetTimeProvider
}

// NewNetTimeProvider starts a clock server on address:port serving the given
// clock. Returns nil on failure.
func NewNetTimeProvider(clock *gst.Clock, address string, port int) *NetTimeProvider {
	if clock == nil {
		return nil
	}
	cAddr := C.CString(address)
	defer C.free(unsafe.Pointer(cAddr))

	p := C.gst_net_time_provider_new(
		(*C.GstClock)(unsafe.Pointer(clock.Instance())),
		cAddr,
		C.gint(port),
	)
	if p == nil {
		return nil
	}
	return &NetTimeProvider{ptr: p}
}

// Close stops the provider and releases the underlying GObject reference.
func (p *NetTimeProvider) Close() error {
	if p == nil || p.ptr == nil {
		return nil
	}
	C.g_object_unref(C.gpointer(unsafe.Pointer(p.ptr)))
	p.ptr = nil
	return nil
}

// NetClientClock embeds *gst.Clock; pass directly to pipeline.UseClock(...).
type NetClientClock struct {
	*gst.Clock
	ptr *C.GstClock
}

// NewNetClientClock creates a client clock that synchronizes to a remote
// NetTimeProvider. Pass empty name for default. Returns nil on failure.
func NewNetClientClock(name, remoteAddress string, remotePort int) *NetClientClock {
	cName := C.CString(name)
	cAddr := C.CString(remoteAddress)
	defer C.free(unsafe.Pointer(cName))
	defer C.free(unsafe.Pointer(cAddr))

	c := C.gst_net_client_clock_new(cName, cAddr, C.gint(remotePort), 0)
	if c == nil {
		return nil
	}
	gstClock := gst.FromGstClockUnsafeFull(unsafe.Pointer(c))
	return &NetClientClock{
		Clock: gstClock,
		ptr:   c,
	}
}

// WaitForSync blocks until the client clock is synced to the remote provider
// or the timeout elapses. Returns true on success.
func (c *NetClientClock) WaitForSync(timeout time.Duration) bool {
	if c == nil || c.ptr == nil {
		return false
	}
	ns := C.GstClockTime(timeout.Nanoseconds())
	ret := C.gst_clock_wait_for_sync(c.ptr, ns)
	return ret != 0
}

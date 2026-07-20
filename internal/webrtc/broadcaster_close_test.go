/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package webrtc

import (
	"sync"
	"testing"
)

// TestPeerCloseDoneIdempotent reproduces the 2026-07-20 production crash: pion
// fires OnConnectionStateChange more than once during teardown (Failed then
// Closed), and Stop() also closes the peer's done channel. Before closeDone()
// wrapped the close in a sync.Once, the second close panicked with
// "close of closed channel" and killed the whole process, dropping every
// listener on every mount at once.
func TestPeerCloseDoneIdempotent(t *testing.T) {
	p := &peerConnection{id: "peer-1", done: make(chan struct{})}

	// Sequential double-close: the exact Failed-then-Closed path from the crash.
	p.closeDone()
	p.closeDone()
	p.closeDone()

	select {
	case <-p.done:
	default:
		t.Fatal("done channel should be closed after closeDone()")
	}
}

// TestPeerCloseDoneConcurrent races the state-change callback against Stop().
// With -race this fails if the close is not serialized through the sync.Once.
func TestPeerCloseDoneConcurrent(t *testing.T) {
	p := &peerConnection{id: "peer-2", done: make(chan struct{})}

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p.closeDone()
		}()
	}
	wg.Wait()

	select {
	case <-p.done:
	default:
		t.Fatal("done channel should be closed after concurrent closeDone()")
	}
}

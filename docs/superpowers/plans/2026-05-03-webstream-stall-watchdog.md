# Webstream Stall Watchdog Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Detect and recover from zombie webstream pipelines where GStreamer connects to an HTTP source but produces no audio, leaving the broadcast mount silent indefinitely.

**Architecture:** Add an atomic last-fed timestamp to `broadcast.Mount` that is updated each time bytes are written. In `watchWebstreamPipeline`, start a per-pipeline stall watchdog goroutine that monitors this timestamp and force-stops the pipeline if no bytes have flowed for 30 seconds (after an initial 20-second grace period). This piggybacks on the existing `watchWebstreamPipeline` reconnect loop — a force-stop causes `doneCh` to fire, which the existing machinery handles exactly like a natural pipeline crash.

**Note:** GStreamer already has a `watchdog timeout=15000` element in the pipeline that fires when no *input buffers* flow. The Go-level watchdog covers a complementary scenario: GStreamer is processing data but zero encoded bytes reach the broadcast mount (e.g., pipe blockage between GStreamer fd=3 and the Go `FeedFrom` goroutine, or silent encoded audio).

**Tech Stack:** Go 1.24, `sync/atomic`, existing `broadcast.Mount`, existing `watchWebstreamPipeline` reconnect loop.

---

## Chunk 1: Byte-flow tracking in broadcast.Mount

### Task 1: Add `lastFedAt` atomic to `broadcast.Mount`

**Files:**
- Modify: `internal/broadcast/server.go`

**Context:**  
`Mount` is in `internal/broadcast/server.go`. It has a `FeedFrom(r io.Reader) error` method that reads bytes from a GStreamer pipe and calls `m.Broadcast(data)`. We need to record the timestamp of the last successful byte write so the stall watchdog can check it. Use `sync/atomic` on an `int64` (unix nanoseconds) — no mutex needed, reads and writes are independent.

- [ ] **Step 1: Write a failing test**

Create `internal/broadcast/server_test.go`:

```go
/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package broadcast

import (
	"bytes"
	"testing"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/rs/zerolog"
)

func TestMount_BytesReceivedAt_ZeroBeforeFeed(t *testing.T) {
	bus := events.NewBus()
	m := NewMount("test", "audio/mpeg", 128, zerolog.Nop(), bus)
	got := m.BytesReceivedAt()
	if !got.IsZero() {
		t.Errorf("BytesReceivedAt() = %v, want zero time before any feed", got)
	}
}

func TestMount_BytesReceivedAt_UpdatedAfterFeed(t *testing.T) {
	bus := events.NewBus()
	m := NewMount("test", "audio/mpeg", 128, zerolog.Nop(), bus)

	before := time.Now()
	// FeedFrom reads until EOF; give it a small payload
	r := bytes.NewReader(make([]byte, 1024))
	_ = m.FeedFrom(r) // returns io.EOF

	got := m.BytesReceivedAt()
	if got.IsZero() {
		t.Fatal("BytesReceivedAt() is zero after FeedFrom wrote bytes")
	}
	if got.Before(before) {
		t.Errorf("BytesReceivedAt() = %v, want after %v", got, before)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test -v -run "TestMount_BytesReceivedAt" ./internal/broadcast/
```

Expected: FAIL — `BytesReceivedAt undefined`

- [ ] **Step 3: Add `lastFedAt` field and `BytesReceivedAt()` method to `Mount`**

In `internal/broadcast/server.go`:

Add `"sync/atomic"` to imports (it is likely not yet imported).

Add `lastFedAt int64` field to the `Mount` struct (after `inputCount`):

```go
type Mount struct {
	Name        string
	ContentType string
	Bitrate     int

	mu         sync.RWMutex
	clients    map[*client]struct{}
	buffer     *ringBuffer
	logger     zerolog.Logger
	inputDone  chan struct{}
	inputCount int
	lastFedAt  int64       // unix nano; updated atomically each time bytes are written
	bus        *events.Bus
}
```

Add `BytesReceivedAt()` method after `NewMount`:

```go
// BytesReceivedAt returns the time bytes were last written to this mount by FeedFrom.
// Returns zero time if no bytes have been written yet.
func (m *Mount) BytesReceivedAt() time.Time {
	ns := atomic.LoadInt64(&m.lastFedAt)
	if ns == 0 {
		return time.Time{}
	}
	return time.Unix(0, ns)
}
```

In `FeedFrom`, update `lastFedAt` immediately after the `m.Broadcast(data)` call:

```go
if n > 0 {
    totalBytes += n
    if time.Since(lastLog) > 10*time.Second {
        m.logger.Info().
            Str("mount", m.Name).
            Int("bytes_last_10s", totalBytes).
            Int("clients", m.ClientCount()).
            Msg("feed active")
        totalBytes = 0
        lastLog = time.Now()
    }
    data := make([]byte, n)
    copy(data, buf[:n])
    m.Broadcast(data)
    atomic.StoreInt64(&m.lastFedAt, time.Now().UnixNano()) // ← add this line
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test -v -run "TestMount_BytesReceivedAt" ./internal/broadcast/
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/broadcast/server.go internal/broadcast/server_test.go
git commit -m "Add BytesReceivedAt to broadcast.Mount for stall detection"
```

---

## Chunk 2: Stall watchdog in watchWebstreamPipeline

### Task 2: Add `startWebstreamStallWatchdog` and wire it up

**Files:**
- Modify: `internal/playout/director.go`
- Modify: `internal/playout/director_webstream_test.go`

**Context:**  
`watchWebstreamPipeline` is in `internal/playout/director.go` around line 1161. It loops on `<-doneCh` (pipeline exited) and then runs the fast-retry and slow-retry reconnect logic. There are three points where a pipeline is successfully started and `doneCh` is set:

1. **Initial pipeline** — set up in `startWebstreamEntry` (not in `watchWebstreamPipeline` itself); `doneCh` comes from `pipeline.Done()` at line 1167.
2. **Fast reconnect success** — `reconnected = true; break` at line 1287; outer loop falls through to re-fetch `pipeline.Done()` at lines 1449–1457.
3. **Slow retry success** — `reconnected = true; break` at line 1444; same re-fetch of `doneCh`.

The stall watchdog goroutine must:
- Start after each successful pipeline connection (including the initial one when `watchWebstreamPipeline` begins watching)
- Stop when the current pipeline's `doneCh` fires (either naturally or because the watchdog killed it)
- Only declare a stall if `BytesReceivedAt()` has been non-zero (bytes did flow) and has gone stale
- Wait an initial grace period (20 s) before starting to check, to allow the pipeline to connect and start flowing

**Stall watchdog function signature:**

```go
func (d *Director) startWebstreamStallWatchdog(
    ctx context.Context,
    mountID string,
    mountName string,
    hqMount *broadcast.Mount,
    pipelineDone <-chan struct{},
)
```

The watchdog runs as a goroutine. It returns as soon as `ctx` is cancelled, `pipelineDone` fires, or it has stopped the pipeline for a stall.

**Constants to add near the top of `director.go` (with the other `const` block):**

```go
const (
    webstreamStallTimeout   = 30 * time.Second
    webstreamStallGrace     = 20 * time.Second
    webstreamStallCheckTick = 10 * time.Second
)
```

- [ ] **Step 1: Write a failing test**

Add to `internal/playout/director_webstream_test.go`:

```go
func TestWatchWebstreamPipeline_StallWatchdogStopsPipeline(t *testing.T) {
	// Verify: if a webstream pipeline produces zero bytes for longer than
	// the stall timeout, the stall watchdog force-stops it.
	d, _ := newMockDirector(t)

	mountName := "stall-test-" + uuid.NewString()[:8]
	hqMount := d.broadcast.CreateMount(mountName, "audio/mpeg", 128)
	lqMount := d.broadcast.CreateMount(mountName+"-lq", "audio/mpeg", 64)
	_ = lqMount

	// Simulate "bytes flowed once, then stopped" by writing to lastFedAt via FeedFrom
	// on a reader that closes immediately, then manually confirming BytesReceivedAt is set.
	// The key behaviour: after grace + stall timeout with no new bytes, the pipeline is stopped.
	//
	// We use very short timeouts so the test finishes in < 1 second.
	const grace = 10 * time.Millisecond
	const stallTimeout = 50 * time.Millisecond
	const checkTick = 10 * time.Millisecond

	// Record a "last fed" time in the past (beyond stall timeout)
	// We call the internal method directly via a tiny FeedFrom to seed lastFedAt.
	_ = hqMount.FeedFrom(bytes.NewReader([]byte("audio")))

	// Now make lastFedAt appear stale by waiting
	time.Sleep(stallTimeout + 10*time.Millisecond)

	// Create a mock pipeline that never exits on its own
	doneCh := make(chan struct{})
	stopped := make(chan struct{})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	d.startWebstreamStallWatchdog(ctx, "mock-mount-id", mountName, hqMount, doneCh,
		grace, stallTimeout, checkTick,
		func() { close(stopped) }, // stall action: close stopped channel instead of stopping real pipeline
	)

	select {
	case <-stopped:
		// Watchdog fired — correct
	case <-time.After(500 * time.Millisecond):
		t.Error("stall watchdog did not fire within timeout")
	}
}

func TestWatchWebstreamPipeline_StallWatchdogExitsOnPipelineDone(t *testing.T) {
	d, _ := newMockDirector(t)

	mountName := "stall-exit-" + uuid.NewString()[:8]
	hqMount := d.broadcast.CreateMount(mountName, "audio/mpeg", 128)
	_ = hqMount.FeedFrom(bytes.NewReader([]byte("audio")))

	doneCh := make(chan struct{})
	stopped := make(chan struct{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	d.startWebstreamStallWatchdog(ctx, "mock-id", mountName, hqMount, doneCh,
		10*time.Millisecond, 50*time.Millisecond, 10*time.Millisecond,
		func() { close(stopped) },
	)

	// Close doneCh before the stall timeout elapses
	close(doneCh)

	select {
	case <-stopped:
		t.Error("watchdog fired stall action even though pipeline exited naturally")
	case <-time.After(200 * time.Millisecond):
		// Correct — watchdog exited cleanly without stopping
	}
}
```

Note: `bytes` package import needed in the test file. The test uses an injectable `stallAction func()` parameter for testability — the real implementation passes `func() { d.manager.StopPipeline(mountID) }`. `startWebstreamStallWatchdog` spawns the goroutine internally and returns immediately — do NOT call it with `go` at any call site.

- [ ] **Step 2: Run test to verify it fails**

```bash
go test -v -run "TestWatchWebstreamPipeline_Stall" ./internal/playout/
```

Expected: FAIL — `startWebstreamStallWatchdog undefined`

- [ ] **Step 3: Add `startWebstreamStallWatchdog` to `director.go`**

Add the constants block near the top of `director.go` (with existing constants):

```go
const (
    webstreamStallTimeout   = 30 * time.Second
    webstreamStallGrace     = 20 * time.Second
    webstreamStallCheckTick = 10 * time.Second
)
```

Add the function (anywhere in `director.go`, e.g., just before `watchWebstreamPipeline`):

```go
// startWebstreamStallWatchdog monitors a webstream pipeline for audio stalls.
// A stall is declared if bytes were received at some point (BytesReceivedAt is
// non-zero) but then stopped flowing for longer than stallTimeout. When a stall
// is detected, stallAction is called (typically: stop the pipeline so doneCh
// fires and watchWebstreamPipeline can reconnect).
//
// The watchdog exits immediately when ctx is cancelled or pipelineDone fires.
// grace, stallTimeout, and checkTick are injectable for testing; the production
// caller passes webstreamStallGrace, webstreamStallTimeout, webstreamStallCheckTick.
func (d *Director) startWebstreamStallWatchdog(
	ctx context.Context,
	mountID, mountName string,
	hqMount *broadcast.Mount,
	pipelineDone <-chan struct{},
	grace, stallTimeout, checkTick time.Duration,
	stallAction func(),
) {
	if hqMount == nil {
		return
	}
	go func() {
		// Grace period: give the pipeline time to connect and start producing bytes.
		select {
		case <-ctx.Done():
			return
		case <-pipelineDone:
			return
		case <-time.After(grace):
		}

		ticker := time.NewTicker(checkTick)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-pipelineDone:
				return
			case <-ticker.C:
				lastFed := hqMount.BytesReceivedAt()
				if lastFed.IsZero() {
					// No bytes ever received; GStreamer's own watchdog handles this.
					continue
				}
				if time.Since(lastFed) > stallTimeout {
					d.logger.Warn().
						Str("mount", mountID).
						Str("mount_name", mountName).
						Dur("stall", time.Since(lastFed)).
						Msg("webstream stall detected — forcing pipeline restart")
					stallAction()
					return
				}
			}
		}
	}()
}
```

- [ ] **Step 4: Wire the watchdog into `watchWebstreamPipeline`**

There are three places where a live pipeline exists and `doneCh` is known. Add `startWebstreamStallWatchdog` calls at each.

**Point 1: Initial watch loop entry** (around line 1167, after `doneCh := pipeline.Done()`):

```go
doneCh := pipeline.Done()
if doneCh == nil {
    return
}

// Start stall watchdog for the initial pipeline.
d.startWebstreamStallWatchdog(ctx, entry.MountID, mountName,
    d.broadcast.GetMount(mountName),
    doneCh,
    webstreamStallGrace, webstreamStallTimeout, webstreamStallCheckTick,
    func() { _ = d.manager.StopPipeline(entry.MountID) },
)
```

**Point 2: Fast reconnect success and slow retry success** — both break out of their respective inner loops and fall through to the single outer re-fetch block at lines 1449–1457. Add the watchdog call there (one call covers both paths):

```go
// Re-enter watch loop with the new pipeline
pipeline = d.manager.GetPipeline(entry.MountID)
if pipeline == nil {
    return
}
doneCh = pipeline.Done()
if doneCh == nil {
    return
}
// Start stall watchdog for the reconnected pipeline.
d.startWebstreamStallWatchdog(ctx, entry.MountID, mountName,
    d.broadcast.GetMount(mountName),
    doneCh,
    webstreamStallGrace, webstreamStallTimeout, webstreamStallCheckTick,
    func() { _ = d.manager.StopPipeline(entry.MountID) },
)
```

**Point 3:** No additional call needed — both fast and slow retry paths reach the same re-fetch block above.

- [ ] **Step 5: Add `bytes` import to test file if not present**

Check the import block in `internal/playout/director_webstream_test.go` and add `"bytes"` if missing.

- [ ] **Step 6: Run tests to verify they pass**

```bash
go test -v -run "TestWatchWebstreamPipeline_Stall" ./internal/playout/
```

Expected: PASS

- [ ] **Step 7: Run full CI**

```bash
make ci
```

Expected: all green, no `gofmt` failures.

- [ ] **Step 8: Commit**

```bash
git add internal/playout/director.go internal/playout/director_webstream_test.go
git commit -m "Add webstream stall watchdog to detect silent zombie pipelines"
```

---

## Chunk 3: Version bump and tag

### Task 3: Bump version to v1.39.10

**Files:**
- Modify: `internal/version/version.go`

- [ ] **Step 1: Update version**

In `internal/version/version.go`, change:
```go
var Version = "1.39.9"
```
to:
```go
var Version = "1.39.10"
```

- [ ] **Step 2: Commit and tag**

```bash
git add internal/version/version.go
git commit -m "Webstream stall watchdog — detects and recovers silent zombie pipelines (v1.39.10)"
git tag -a v1.39.10 -m "Version 1.39.10"
git push origin main
git push origin v1.39.10
```

---

## Implementation Notes

**Why not just lower the GStreamer watchdog timeout?**  
The GStreamer `watchdog timeout=15000` fires when no *input buffers* arrive (i.e., the HTTP source stops sending). It does not fire when the source sends data that GStreamer can't decode, or when encoded bytes make it out of GStreamer but don't reach the broadcast mount. The Go-level watchdog covers that gap.

**Why a 20-second grace period?**  
On a slow connection, souphttpsrc may take 5–15 seconds to buffer enough data before sending the first decoded frame. The grace period prevents the watchdog from firing before the pipeline has had a chance to start flowing.

**Why check `BytesReceivedAt().IsZero()`?**  
If no bytes have ever been written, the GStreamer watchdog (15s) will handle the timeout. We only need the Go-level watchdog for the case where bytes flowed initially (proving the pipeline connected) and then stopped.

**Thread safety:**  
`lastFedAt` is an `int64` updated with `atomic.StoreInt64` and read with `atomic.LoadInt64`. No mutex needed — the write in `FeedFrom` and the read in `BytesReceivedAt` are independent. Worst case is a slightly stale read, which is acceptable for a 30-second stall window.

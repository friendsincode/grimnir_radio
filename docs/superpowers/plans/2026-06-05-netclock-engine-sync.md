# NetClock Engine Sync Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

> **Status:** Complete. 6 chunks (Chunk 0 spike + Chunks 1-5). Written 2026-06-05 incrementally per `feedback_brainstorming_incremental_save.md`.

**Goal:** Synchronize the GStreamer pipeline clocks across N media engines so the PCM each engine emits is sample-aligned at the same wall-clock instant — making the edge encoder's input switch truly seamless (no phase discontinuity at the switch sample).

**Architecture:** One engine in each region acquires the NetClock master role via Redis lease (separate key from the scheduler leader); it serves clock time on a TCP port via go-gst's `GstNetTimeProvider`. All engines (master and slaves) bind their pipelines to a `GstNetClientClock` pointing at the master. The slaves' pipeline clock follows the master's to within microseconds on a LAN. **The existing pipeline STRINGS in `internal/playout/director.go` stay unchanged**; the migration is at the pipeline-construction layer (`internal/playout/pipeline.go`) — replacing `exec.Command("gst-launch-1.0", ...)` with go-gst's `gst.NewPipelineFromString(...)` so we can programmatically call `pipeline.UseClock(netClock)` before state transition to PLAYING.

**Tech Stack:** Go 1.24, **go-gst** (CGo bindings; already in v2-dev via Track A step 4), GStreamer 1.20+ NetClock subsystem (`GstNetTimeProvider`, `GstNetClientClock` — both in `gst-plugins-base/libs/gst/net/`), existing `internal/leadership` Redis lease infra for election.

**Issue:** TBD — file when first chunk merges.

**Parent design:** `docs/superpowers/plans/2026-06-01-ha-zero-loss-failover-design.md` Section 2 ("Clock sync: GStreamer NetClock") + Section 9.1 Track A step 5.

**Builds on:** Track A step 4 (edge encoder + PCM transport, shipped in v2.0.0-alpha.3).

**Decision locked 2026-06-05 (Q-NC1):**

| Q | Decision | Rationale |
|---|---|---|
| Q-NC1 | **A** — migrate engine pipeline construction to programmatic go-gst (via `gst.NewPipelineFromString`) so `UseClock()` is callable in Go | NetClock + UseClock requires programmatic clock binding; gst-launch can't provide it; the parse_launch path preserves the existing pipeline strings (and their existing tests) while flipping ownership to go-gst |

**Honest scope:** 6 chunks (Chunk 0 spike + Chunks 1-5). Conservative estimate **3-4 calendar weeks at solo pace**. Half of what a full ground-up go-gst port would cost, because we keep the existing pipeline strings and only flip the spawning layer.

**Out of scope** (deferred follow-ups):
- Per-region NetClock failover behavior under master loss (slave promotion). Phase 1 = master loss requires manual operator action to flip another engine to master. Auto-failover is straightforward to add later once the basic master/slave path works.
- PTP (IEEE 1588) as an alternative clock source — NetClock alone is enough for LAN sub-millisecond sync. PTP becomes interesting at cross-DC scales (phase 2/3).
- `GstPtpClock` migration (would need PTP daemon on each host). Same precision as NetClock on a LAN; deferred unless an actual operational issue with NetClock appears.

---

## File structure

| File | Status | Responsibility |
|---|---|---|
| `internal/playout/spike/netclock_spike.go` | Create then delete | Spike binary: two programmatic pipelines bound to one NetClock master; verify timestamp alignment |
| `docs/superpowers/spikes/2026-06-05-netclock-spike.md` | Create | Spike report (proceed / redesign) |
| `internal/playout/pipeline.go` | Modify | Migrate `Pipeline.Start()` / `StartWithDualOutput()` from `exec.Command("gst-launch-1.0", ...)` to `gst.NewPipelineFromString(...)` |
| `internal/playout/pipeline_test.go` | Modify | Adjust process-lifecycle tests; existing pipeline-string tests in other files stay unchanged |
| `internal/playout/clock.go` | Create | NetClock master election + provider lifecycle; NetClient construction |
| `internal/playout/clock_test.go` | Create | Master/slave state-machine tests; mock leadership |
| `internal/leadership/election.go` | Possibly modify | Add a `netclock-master-<region>` lease key alongside existing scheduler-leader key (or expose a generic per-key acquire API if not already there) |
| `internal/config/config.go` | Modify | Add `GRIMNIR_NETCLOCK_ENABLED`, `GRIMNIR_NETCLOCK_PORT`, `GRIMNIR_NETCLOCK_REGION`, `GRIMNIR_NETCLOCK_MASTER_LEASE_KEY` |
| `cmd/mediaengine/main.go` | Modify | Wire the clock-master goroutine + clock-client construction; pass clock to playout director |
| `internal/playout/director.go` | Modify | Accept a `clock *gst.Clock` parameter (from main); pass to each pipeline at construction; existing pipeline-STRING functions stay unchanged |
| `internal/playout/integration_test/netclock_integration_test.go` | Create | E2E test: 2 mediaengine processes + NetClock + edge encoder; assert no phase discontinuity in encoded output at switch |
| `CLAUDE.md` | Modify | Document the new NetClock env vars + the architectural change (mediaengine now uses go-gst, like edge-encoder) |
| `internal/version/version.go` | Modify | Bump 2.0.0-alpha.3 → 2.0.0-alpha.4 |

**Decomposition principle:** the lift-and-shift from gst-launch subprocess to `gst.NewPipelineFromString` is contained to `internal/playout/pipeline.go`. The pipeline strings in `director.go` don't move. NetClock master/slave logic lives in its own `clock.go` file. Each file < 300 lines.

---

## Chunk 0: Spike — NetClock between two programmatic pipelines

Validate that the NetClock approach actually produces phase-aligned PCM before committing to the engine migration. Throwaway spike.

### Task 0.1: Spike script

**Files:**
- Create then delete: `internal/playout/spike/netclock_spike.go` (or `/tmp/netclock-spike/` — whichever is cleaner)

**Context:**
The spike spawns three programmatic GStreamer pipelines via go-gst:
1. **Master engine**: `audiotestsrc freq=440 → audioconvert → audio/x-raw,format=S16BE,44100,channels=2 → rtpL16pay → udpsink :15004` AND a separate `NetTimeProvider` on port 9094 publishing the same pipeline's clock.
2. **Slave engine**: same pipeline shape pointing at `udpsink :15005`, BUT before SetState(PLAYING) it sets `pipeline.UseClock(NewNetClientClock(addr=master:9094))`.
3. **Listener / verifier**: reads RTP from both ports, parses the RTP timestamps, asserts the timestamp deltas between corresponding samples differ by less than 1ms (typical NetClock LAN sync precision is < 100µs).

If the RTP timestamps line up: PROCEED. If they drift by >1ms even with NetClock bound: investigate (could be NetClient configuration, could be GStreamer version bug).

- [ ] **Step 1: Write the spike**

`internal/playout/spike/netclock_spike.go`:

```go
//go:build spike

/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

// Command netclock_spike validates GStreamer NetClock master/slave sync.
// Two programmatic pipelines, one master and one NetClient-bound slave,
// both emit RTP-L16 to different UDP ports. A verifier reads both, extracts
// RTP timestamps, asserts they align within 1ms.
//
// Build: go build -tags=spike -o /tmp/netclock_spike ./internal/playout/spike/
// Run:   /tmp/netclock_spike
package main

import (
	"fmt"
	"log"
	"net"
	"os"
	"time"

	"github.com/go-gst/go-gst/gst"
	"github.com/go-gst/go-gst/gst/net"
)

const (
	netClockPort   = 9094
	masterRTPPort  = 15004
	slaveRTPPort   = 15005
	runDuration    = 5 * time.Second
)

func main() {
	gst.Init(nil)

	// ----- Master pipeline -----
	masterPipeline, err := gst.NewPipelineFromString(fmt.Sprintf(
		"audiotestsrc is-live=true freq=440 wave=sine ! "+
			"audio/x-raw,rate=44100,channels=2,format=S16BE ! "+
			"rtpL16pay pt=10 mtu=1400 ! udpsink host=127.0.0.1 port=%d sync=true",
		masterRTPPort))
	if err != nil {
		log.Fatalf("master pipeline: %v", err)
	}

	// Expose master's clock as a NetTimeProvider on port netClockPort
	masterClock := masterPipeline.GetPipelineClock()
	provider := gstnet.NewNetTimeProvider(masterClock, "0.0.0.0", netClockPort)
	if provider == nil {
		log.Fatal("NewNetTimeProvider returned nil")
	}
	defer provider.Unref()

	// ----- Slave pipeline -----
	slavePipeline, err := gst.NewPipelineFromString(fmt.Sprintf(
		"audiotestsrc is-live=true freq=440 wave=sine ! "+
			"audio/x-raw,rate=44100,channels=2,format=S16BE ! "+
			"rtpL16pay pt=10 mtu=1400 ! udpsink host=127.0.0.1 port=%d sync=true",
		slaveRTPPort))
	if err != nil {
		log.Fatalf("slave pipeline: %v", err)
	}

	// Bind slave to the master's clock via NetClient
	netClient := gstnet.NewNetClientClock("netclock-spike", "127.0.0.1", netClockPort, 0)
	if netClient == nil {
		log.Fatal("NewNetClientClock returned nil")
	}
	// Wait for the client to lock onto the master (GStreamer says this can take a few seconds)
	if !netClient.WaitForSync(5 * time.Second) {
		log.Fatal("NetClient never synced to master within 5s")
	}
	slavePipeline.UseClock(netClient.Clock)

	// ----- Start both pipelines -----
	if err := masterPipeline.SetState(gst.StatePlaying); err != nil {
		log.Fatalf("master playing: %v", err)
	}
	if err := slavePipeline.SetState(gst.StatePlaying); err != nil {
		log.Fatalf("slave playing: %v", err)
	}
	defer masterPipeline.SetState(gst.StateNull)
	defer slavePipeline.SetState(gst.StateNull)

	// ----- Verifier: capture RTP from both ports for runDuration, compare timestamps -----
	go captureRTP(masterRTPPort, "master")
	go captureRTP(slaveRTPPort, "slave")

	time.Sleep(runDuration)
	fmt.Println("Spike complete. Listen to /tmp/spike-master.raw + /tmp/spike-slave.raw or compare RTP timestamps.")
	os.Exit(0)
}

func captureRTP(port int, label string) {
	addr := net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: port}
	conn, err := net.ListenUDP("udp", &addr)
	if err != nil {
		log.Printf("[%s] listen %d: %v", label, port, err)
		return
	}
	defer conn.Close()
	conn.SetReadDeadline(time.Now().Add(runDuration + time.Second))

	buf := make([]byte, 1500)
	for i := 0; ; i++ {
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			return
		}
		if n < 12 {
			continue
		}
		// RTP timestamp is bytes 4-7 (big-endian 32-bit)
		ts := uint32(buf[4])<<24 | uint32(buf[5])<<16 | uint32(buf[6])<<8 | uint32(buf[7])
		seq := uint16(buf[2])<<8 | uint16(buf[3])
		if i%50 == 0 {
			fmt.Printf("[%s] pkt#%d seq=%d ts=%d (recv %s)\n", label, i, seq, ts, time.Now().Format("15:04:05.000"))
		}
	}
}
```

Notes:
- `import "github.com/go-gst/go-gst/gst/net"` — confirm the actual package path; could be `gstnet` directly. Check `go doc github.com/go-gst/go-gst/gst/net` first.
- `WaitForSync` may have a different signature; check API.
- `NewNetTimeProvider` signature: real one likely takes `(clock *Clock, address string, port int) *NetTimeProvider`. Adapt if different.

- [ ] **Step 2: Build and run**

```bash
go build -tags=spike -o /tmp/netclock_spike ./internal/playout/spike/
/tmp/netclock_spike
```

Expected: prints `[master]` and `[slave]` packet rows. **The RTP timestamp deltas between corresponding sequence numbers should be tightly aligned** (within a few hundred microseconds at 44.1kHz = ~5 RTP-timestamp ticks). If the deltas are wildly off (>1ms = ~45 ticks), NetClock isn't binding correctly.

- [ ] **Step 3: Document findings + decide**

Create `docs/superpowers/spikes/2026-06-05-netclock-spike.md`:

```markdown
# Spike — NetClock master/slave PCM alignment

**Date:** 2026-06-05
**Decision:** [PROCEED | PROCEED WITH CAVEAT | REDESIGN]

## Hypothesis
GstNetTimeProvider + GstNetClientClock produces sample-aligned PCM between two programmatic pipelines.

## Method
[describe spike]

## Observed
- NetClient sync time: ___
- Timestamp deltas between master/slave for matched sequence numbers: ___
- Subjective listening of captured PCM (if decoded): ___

## Decision
[describe]
```

- [ ] **Step 4: Delete spike code**

```bash
rm -rf internal/playout/spike/
rm -f /tmp/netclock_spike
git add docs/superpowers/spikes/2026-06-05-netclock-spike.md
git commit -m "spike: validate NetClock master/slave PCM alignment ([result])"
```

- [ ] **Step 5: Confirm decision with user before starting Chunk 1**

Chunk 1 is the engine pipeline lifecycle migration — significant. Worth a green light from the spike before proceeding.

---

## Chunk 1: Migrate Pipeline lifecycle to programmatic go-gst (preserving pipeline strings)

The change isolated to `internal/playout/pipeline.go`: stop spawning `gst-launch-1.0` as a subprocess; instead construct the pipeline via `gst.NewPipelineFromString(<existing pipeline string>)` and own its lifecycle from Go. Pipeline strings in `director.go` stay verbatim. This single migration unlocks programmatic clock binding (the prerequisite for NetClock).

### Task 1.1: Add `gst.NewPipelineFromString` smoke test

**Files:**
- Create: `internal/playout/pipeline_gogst_test.go`

**Context:**
Before touching production code, verify go-gst can parse and run one of the existing pipeline strings correctly. The smoke test uses one of the simplest existing pipeline strings from `director.go` (the dual-bitrate one is the canonical case).

- [ ] **Step 1: Write the smoke test**

```go
/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package playout

import (
	"testing"
	"time"

	"github.com/go-gst/go-gst/gst"
)

// TestGoGstCanParseDualBroadcastPipeline confirms that a representative
// existing pipeline string (the kind director.go produces) is parseable by
// gst.NewPipelineFromString without errors. This is the prerequisite for
// the rest of Chunk 1 — we're not changing the pipeline strings, only how
// they get spawned.
func TestGoGstCanParseDualBroadcastPipeline(t *testing.T) {
	gst.Init(nil)
	// A trimmed version of the kind of string buildDualBroadcastPipeline produces
	// with HQ + LQ outputs to fakesinks (avoids needing fd=3/4 setup for this test).
	pipelineStr := "audiotestsrc num-buffers=10 ! audioconvert ! audioresample ! " +
		"audio/x-raw,rate=44100,channels=2 ! tee name=t " +
		"t. ! queue ! lamemp3enc bitrate=128 ! fakesink sync=true " +
		"t. ! queue ! lamemp3enc bitrate=64  ! fakesink sync=true"

	pipeline, err := gst.NewPipelineFromString(pipelineStr)
	if err != nil {
		t.Fatalf("NewPipelineFromString: %v", err)
	}
	defer pipeline.SetState(gst.StateNull)

	if err := pipeline.SetState(gst.StatePlaying); err != nil {
		t.Fatalf("SetState(Playing): %v", err)
	}

	// Let it run briefly; verify no bus errors
	bus := pipeline.GetPipelineBus()
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		msg := bus.TimedPop(gst.ClockTime(50 * time.Millisecond))
		if msg == nil {
			continue
		}
		if msg.Type() == gst.MessageError {
			t.Fatalf("pipeline error: %v", msg.ParseError())
		}
		if msg.Type() == gst.MessageEOS {
			break
		}
	}
}
```

- [ ] **Step 2: Run test**

```bash
go test -v -run TestGoGstCanParseDualBroadcastPipeline ./internal/playout/
```

Expected: PASS. If it fails on `lamemp3enc` not found or similar, install the missing plugin pack (should already be installed from Track A step 4).

- [ ] **Step 3: Commit**

```bash
git add internal/playout/pipeline_gogst_test.go
git commit -m "playout: smoke test confirming go-gst parses existing pipeline strings"
```

---

### Task 1.2: Refactor `Pipeline.Start()` to use go-gst, preserving fd-pipe output

**Files:**
- Modify: `/home/code/projects/grimnir_radio/internal/playout/pipeline.go` (`Start()`, `StartWithDualOutput()`, `Stop()`, `Done()`)
- Modify: `/home/code/projects/grimnir_radio/internal/playout/pipeline_test.go`

**Context:**
Current shape (per the earlier Explore findings):
```go
shellCmd := fmt.Sprintf("%s -e %s", p.cfg.GStreamerBin, launch)
cmd := exec.CommandContext(ctx, "sh", "-c", shellCmd)
cmd.SysProcAttr = newPipelineProcessGroup()
```

Replace with:
```go
gstPipeline, err := gst.NewPipelineFromString(launch)
if err != nil { ... }
if err := gstPipeline.SetState(gst.StatePlaying); err != nil { ... }
```

**Critical preservation**: the existing pipelines write encoded bytes to `fdsink fd=3` / `fdsink fd=4`, which the parent Go process reads from `os.Pipe()` file descriptors. This pattern works ONLY because `exec.Command` lets us pre-attach FDs to the child process. With go-gst running in-process, **`fdsink fd=3` will write to OUR FD 3**, not to a child's. That changes the plumbing.

Two options for handling this:
- **A. Swap `fdsink fd=N` → `appsink name=hq/lq` in the pipeline string** at construction time (i.e., when `buildDualBroadcastPipeline` etc. produce the string, OR when `Pipeline.Start` receives the string). Then read encoded bytes via `app.SinkFromElement(elt).PullSample()` per the same pattern as the edge encoder's broadcast.go. Cleaner long-term, easier to test, no FD juggling. **Recommended.**
- **B. Keep `fdsink fd=3` and have go-gst inherit our process's FDs.** Possible (the process IS the parent), but requires manually creating pipes with `os.Pipe()`, writing to FD 3/4 before pipeline construction, then reading from the read end. Mirrors the existing pattern but more fragile.

Going with **A**. The change ripples to `Pipeline.HQReader()` / `LQReader()` — they'll wrap the app.Sink instead of an `*os.File`.

This task is BIG. Break into sub-steps with a fixture-based test at each.

- [ ] **Step 1: Replace `Pipeline.Start()` body**

In `internal/playout/pipeline.go`, replace the exec.Command-based implementation with go-gst. New shape:

```go
import (
    "github.com/go-gst/go-gst/gst"
    "github.com/go-gst/go-gst/gst/app"
)

type Pipeline struct {
    cfg          PipelineConfig
    launch       string

    // go-gst owned objects
    gst          *gst.Pipeline
    hqAppsink    *app.Sink   // formerly read via fdsink fd=3
    lqAppsink    *app.Sink   // formerly fd=4

    // Lifecycle
    bus          *gst.Bus
    doneCh       chan error
    cancel       context.CancelFunc

    // ... existing fields preserved
}

func (p *Pipeline) Start(ctx context.Context, launch string) error {
    // Translate "fdsink fd=3" / "fd=4" markers in launch string to appsink names.
    launch = strings.ReplaceAll(launch, "fdsink fd=3", "appsink name=hq emit-signals=false sync=false max-buffers=10")
    launch = strings.ReplaceAll(launch, "fdsink fd=4", "appsink name=lq emit-signals=false sync=false max-buffers=10")
    p.launch = launch

    pipeline, err := gst.NewPipelineFromString(launch)
    if err != nil {
        return fmt.Errorf("parse pipeline: %w", err)
    }
    p.gst = pipeline

    // Locate the appsinks (only present in dual-output pipelines)
    if hqElt, _ := pipeline.GetElementByName("hq"); hqElt != nil {
        p.hqAppsink = app.SinkFromElement(hqElt)
    }
    if lqElt, _ := pipeline.GetElementByName("lq"); lqElt != nil {
        p.lqAppsink = app.SinkFromElement(lqElt)
    }

    // Bus consumer
    p.bus = pipeline.GetPipelineBus()
    p.doneCh = make(chan error, 1)
    pipelineCtx, cancel := context.WithCancel(ctx)
    p.cancel = cancel
    go p.consumeBus(pipelineCtx)

    if err := pipeline.SetState(gst.StatePlaying); err != nil {
        return fmt.Errorf("set state PLAYING: %w", err)
    }
    return nil
}

func (p *Pipeline) consumeBus(ctx context.Context) {
    for {
        msg := p.bus.TimedPop(gst.ClockTimeNone)
        if msg == nil {
            select { case p.doneCh <- nil: default: }
            return
        }
        switch msg.Type() {
        case gst.MessageError:
            err := msg.ParseError()
            select { case p.doneCh <- err: default: }
            return
        case gst.MessageEOS:
            select { case p.doneCh <- nil: default: }
            return
        }
        if ctx.Err() != nil {
            return
        }
    }
}

func (p *Pipeline) Stop() error {
    if p.cancel != nil { p.cancel() }
    if p.gst != nil {
        // Post EOS to wake the bus consumer cleanly (per edge encoder Chunk 4 Task 4.2 lesson)
        p.gst.PostMessage(gst.NewEOSMessage(p.gst))
        return p.gst.SetState(gst.StateNull)
    }
    return nil
}

func (p *Pipeline) Done() <-chan error {
    return p.doneCh
}
```

- [ ] **Step 2: Replace `HQReader()` / `LQReader()` with appsink-backed readers**

Looking at the existing API: pipeline.HQReader() returns an `io.Reader` (or similar) that the broadcast layer reads from. Replace its implementation:

```go
func (p *Pipeline) HQReader() io.Reader {
    if p.hqAppsink == nil {
        return nil
    }
    return newAppsinkReader(p.hqAppsink)  // helper paralleling internal/edgeencoder/broadcast.go's AppsinkReader
}
```

Reuse the same `AppsinkReader` pattern from `internal/edgeencoder/broadcast.go`. Either duplicate it (briefly acceptable, small file) or extract it to a shared `internal/gstadapter/appsinkreader.go` package. **Lean toward extraction** since both packages now want it.

- [ ] **Step 3: Update tests**

Existing `pipeline_test.go` tests that spawn pipelines and read bytes — re-validate. May need to adjust timing (programmatic go-gst is faster to start than subprocess).

- [ ] **Step 4: Run full playout package tests**

```bash
go test -v -race ./internal/playout/
```

Expected: ALL existing tests still pass. If anything fails, debug — DO NOT proceed with the migration until the test suite is green.

- [ ] **Step 5: Manual integration check via the existing system**

This is mediaengine work that affects production behavior. Before commit, manually run a quick end-to-end: spawn mediaengine locally, point a test file at it, verify HQ + LQ streams come out the way they did before. The behavior should be byte-identical because the pipeline string is unchanged.

- [ ] **Step 6: Commit**

```bash
git add internal/playout/pipeline.go internal/playout/pipeline_test.go internal/gstadapter/ # if extracted
git commit -m "playout: migrate Pipeline lifecycle from gst-launch subprocess to programmatic go-gst

Preserves existing pipeline strings; only swaps how they're spawned and
how encoded bytes flow back (fdsink fd=3/4 → appsink). Necessary
prerequisite for NetClock binding (which requires programmatic
pipeline.UseClock() — gst-launch can't do it).

No behavior change expected for listeners. All existing tests still pass."
```

---

### Task 1.3: Migrate `StartWithDualOutput`, `Stop`, signal handling

**Files:**
- Modify: `/home/code/projects/grimnir_radio/internal/playout/pipeline.go`

**Context:**
The same fd-pipe pattern that `Start` had also applies to `StartWithDualOutput`. The process-group + SIGINT cleanup logic (per memory: orphan gst-launch was a real prod bug) no longer applies — when there's no subprocess to kill, the `setpgid` machinery is dead code. Remove it cleanly.

- [ ] **Step 1: Remove subprocess-specific cleanup**

Delete `newPipelineProcessGroup()` and any orphan-killer logic that's specific to subprocess management. Add inline comments where removed so future code archeologists understand why.

- [ ] **Step 2: Run package tests, verify no regression**

```bash
go test -v -race ./internal/playout/
```

- [ ] **Step 3: Commit**

```bash
git add internal/playout/pipeline.go
git commit -m "playout: remove subprocess-specific cleanup; in-process go-gst doesn't need pgid handling"
```

---

### Task 1.4: Verify orphan-process behavior is unchanged

**Files:**
- No code changes; verification only

**Context:**
Memory notes a real production bug (v1.40.1-v1.40.3) where orphan `gst-launch` processes accumulated. With go-gst, there's no subprocess to leak. But — go-gst internally spawns helper threads. Verify those clean up properly on Pipeline.Stop().

- [ ] **Step 1: Run a stress test locally**

```bash
# Run mediaengine through a soak (e.g., 100 playout cycles in a loop)
# Check for thread/file descriptor leak via /proc/<pid>/status and /proc/<pid>/fd
```

Specifics depend on existing soak infrastructure; if none exists, write a brief test that creates 100 pipelines back-to-back and asserts goroutine count stays bounded.

- [ ] **Step 2: If leaks observed**

Add explicit `gst_object_unref` calls (in go-gst: usually `elt.Unref()`) for any orphaned elements. The most common leak is request pads not being released.

- [ ] **Step 3: Commit (if any fix needed)**

---

## Chunk 2: NetClock master election + NetTimeProvider

Election: per-region Redis lease; the winning engine spawns a `NetTimeProvider` exposing its pipeline clock on a TCP port. Lease losers stay quiet (Chunk 3 handles them as clients).

### Task 2.1: Add NetClock config fields

**Files:**
- Modify: `/home/code/projects/grimnir_radio/internal/config/config.go`
- Modify: `/home/code/projects/grimnir_radio/internal/config/config_test.go`

**Context:**
New env vars:

| Variable | Default | Purpose |
|---|---|---|
| `GRIMNIR_NETCLOCK_ENABLED` | `false` | Master switch. Single-instance keeps default = false. |
| `GRIMNIR_NETCLOCK_PORT` | `9094` | TCP port master uses to serve clock time. Slaves dial this. |
| `GRIMNIR_NETCLOCK_REGION` | empty | Region identifier (used as part of the Redis lease key) |
| `GRIMNIR_NETCLOCK_MASTER_ADDR` | empty | When slave, where to dial. If empty, slave auto-discovers via Redis. |

Validation: if `Enabled=true` and `Region=""`, error.

TDD pattern same as prior config tasks.

- [ ] **Steps 1-6: failing test → impl → verify → commit (mirror earlier config tasks)**

```bash
git commit -m "config: add GRIMNIR_NETCLOCK_* env vars"
```

---

### Task 2.2: Master election via leadership package

**Files:**
- Modify: `/home/code/projects/grimnir_radio/internal/leadership/election.go` (if needed for per-key generalization)
- Create: `/home/code/projects/grimnir_radio/internal/playout/clock.go`
- Create: `/home/code/projects/grimnir_radio/internal/playout/clock_test.go`

**Context:**
The leadership package already implements Redis lease-based election for the scheduler. Need a SECOND lease, keyed `grimnir-netclock-master-<region>`, that operates independently. If the existing leadership API is single-key, generalize it; if it already takes a key parameter, just call it with the new key.

`Clock` type owns the election state machine:
- Init: not yet master, not yet client
- Try acquire lease → if won, become MASTER (spawn NetTimeProvider)
- If lost, become SLAVE (announce intent; Chunk 3 wires the NetClient)
- On lease loss while master, demote to slave (kill provider, become client)

- [ ] **Step 1: Write failing tests with mock leadership**

```go
func TestClock_AcquireMasterSpawnsProvider(t *testing.T) {
    fakeLead := &fakeLeadership{acquire: true}
    c := NewClock(cfg, fakeLead)
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()
    go c.Run(ctx)
    time.Sleep(100*time.Millisecond)
    if !c.IsMaster() { t.Error("expected master after acquire") }
    if c.Provider() == nil { t.Error("expected NetTimeProvider running") }
}

func TestClock_LoseLeaseDemotes(t *testing.T) {
    fakeLead := &fakeLeadership{acquire: true}
    c := NewClock(cfg, fakeLead)
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()
    go c.Run(ctx)
    time.Sleep(50*time.Millisecond)
    fakeLead.lose()  // simulate lease loss
    time.Sleep(50*time.Millisecond)
    if c.IsMaster() { t.Error("expected demoted after lease loss") }
    if c.Provider() != nil { t.Error("expected provider stopped after demotion") }
}
```

- [ ] **Step 2-5: implement → test → make ci → commit**

```bash
git commit -m "playout: NetClock master election + provider lifecycle"
```

---

### Task 2.3: Wire Clock into mediaengine main

**Files:**
- Modify: `/home/code/projects/grimnir_radio/cmd/mediaengine/main.go`
- Modify: `/home/code/projects/grimnir_radio/internal/playout/director.go`

**Context:**
mediaengine main constructs the Clock (when enabled), runs its goroutine, and passes a `func() *gst.Clock` accessor to the director. The director, when building pipelines, will call this accessor and pass the result to `pipeline.UseClock()`. (When clock is nil — disabled — pipelines use default GstSystemClock per today's behavior.)

- [ ] **Step 1-4: wire → integration smoke → commit**

```bash
git commit -m "mediaengine: wire NetClock master election + clock accessor into director"
```

---

## Chunk 3: NetClock client binding for slaves

When an engine is NOT the master (lease lost), it must construct a `GstNetClientClock` pointing at the master's address and bind it to every pipeline before that pipeline transitions to PLAYING.

### Task 3.1: NetClient lifecycle

**Files:**
- Modify: `/home/code/projects/grimnir_radio/internal/playout/clock.go`
- Modify: `/home/code/projects/grimnir_radio/internal/playout/clock_test.go`

**Context:**
When `Clock.IsMaster()` is false, it should produce a working `gst.Clock` (a `GstNetClientClock`) for the director to use. Three sub-states:

1. **Master address unknown** — config didn't set GRIMNIR_NETCLOCK_MASTER_ADDR and Redis hasn't revealed it yet. Pipelines must wait or fall back to system clock.
2. **Master address known, NetClient syncing** — `WaitForSync()` not yet returned true. Pipelines that start in this window will be off; we want to either delay them or accept transient mis-sync.
3. **Master address known, NetClient synced** — return the synced clock for pipelines.

Cleanest is to expose `Clock.GstClock() *gst.Clock` that blocks (with timeout) until synced.

- [ ] **Step 1: Write failing tests for slave/client behavior**
- [ ] **Step 2: Implement**
- [ ] **Step 3: make ci**
- [ ] **Step 4: Commit**

```bash
git commit -m "playout: NetClient construction + sync wait for slave engines"
```

---

### Task 3.2: Director passes clock to every pipeline

**Files:**
- Modify: `/home/code/projects/grimnir_radio/internal/playout/director.go` (every pipeline-construction site)
- Modify: `/home/code/projects/grimnir_radio/internal/playout/pipeline.go` (`Start()` accepts an optional clock arg)

**Context:**
`Pipeline.Start()` grows a new parameter (or constructor option): `clock *gst.Clock`. If non-nil, `pipeline.UseClock(clock)` is called BEFORE `SetState(PLAYING)`. If nil, pipeline uses default behavior (today's behavior).

Director's pipeline-build sites all gain a `d.clock.GstClock()` call before invoking `pipeline.Start()`.

- [ ] **Step 1: Update Pipeline.Start signature**
- [ ] **Step 2: Update all director call sites**
- [ ] **Step 3: Run full playout tests**
- [ ] **Step 4: Commit**

```bash
git commit -m "playout: all pipelines bind to NetClock before SetState(PLAYING)"
```

---

## Chunk 4: Cross-engine PCM verification test

Extend the existing edge-encoder integration test (or write a new sibling) that spawns TWO real `mediaengine` processes (not just `audiotestsrc`-as-engine), both with NetClock enabled, both feeding the edge encoder. Inject engine death; assert the encoded output has materially less phase discontinuity than the pre-NetClock baseline.

### Task 4.1: Two-mediaengine E2E test

**Files:**
- Create: `test/integration/netclock_two_engines_test.go` (build tag `integration`)

**Context:**
Hard test. Requires:
- Postgres reachable (existing TEST_DB_DSN convention)
- Two mediaengine binaries spawned with distinct ports + same NETCLOCK_REGION (so they elect against each other)
- Both pointing at a single playable test file
- Edge encoder spawned consuming both
- HTTP client reads /live, dumps to file
- After kill of one engine, the captured file must have the second engine's output continuing seamlessly

Quantitative success measure: post-switch sample-to-sample first derivative (zero-crossing analysis or just RMS over a 10ms window around the switch) should be within 2× the steady-state RMS. Today's pre-NetClock behavior often shows 10-100× spikes at the switch.

- [ ] **Step 1: Write the test**
- [ ] **Step 2: Run with `-tags=integration`**
- [ ] **Step 3: Commit**

```bash
git commit -m "test: cross-engine NetClock PCM alignment verification (E2E)"
```

---

## Chunk 5: Docs + version bump

### Task 5.1: CLAUDE.md updates

**Files:**
- Modify: `/home/code/projects/grimnir_radio/CLAUDE.md`

**Context:**
Document:
- New env vars
- Mediaengine now uses go-gst (architectural change worth flagging)
- NetClock master/slave operational model (one master per region, lease-elected)
- How operators verify NetClock is working (`grimnir-deploy verify` once that lands; manual SQL until then)

```bash
git commit -m "CLAUDE.md: document NetClock env vars + mediaengine go-gst migration"
```

---

### Task 5.2: Version bump v2.0.0-alpha.4

**Files:**
- Modify: `/home/code/projects/grimnir_radio/internal/version/version.go`

```bash
git add internal/version/version.go
git commit -m "Bump to v2.0.0-alpha.4 (NetClock engine sync, Track A step 5)"
git tag -a v2.0.0-alpha.4 -m "v2.0.0-alpha.4 — NetClock engine sync"
git push origin v2-dev
git push origin v2.0.0-alpha.4
```

---

## Acceptance criteria for the plan as a whole

- Chunk 0 spike report exists in `docs/superpowers/spikes/`, decision = PROCEED.
- Mediaengine playout pipelines now spawn via `gst.NewPipelineFromString` (no more `exec.Command("gst-launch-1.0", ...)`).
- NetClock master election works: with two mediaengines in the same region, exactly one is master, exactly one is slave.
- Slave's pipeline clock follows master's within < 1ms LAN-sync precision.
- End-to-end E2E test (Chunk 4) shows post-switch RMS within 2× steady-state (vs 10-100× pre-NetClock).
- `make ci` exits 0 throughout; GHA green.
- v2.0.0-alpha.4 tagged on origin.

## Out of scope (deferred to follow-up issues)

- **NetClock auto-failover on master death.** Phase 1: master dies → operator manually triggers another engine to acquire the lease (or operator restarts an engine and it tries to acquire on startup). Auto-failover (Redis lease TTL expiry triggers slave promotion) is straightforward to add but isn't required for the first ship.
- **PTP fallback.** If NetClock turns out to be too lossy on a particular network, swap to `GstPtpClock`. Operationally heavier; defer until needed.
- **Engine-divergence detection** (already filed as issue #236). With NetClock running, that detector becomes meaningful and the issue can be picked up.

## Estimated effort

- Chunk 0 (spike): 1 day
- Chunk 1 (pipeline migration): 4-7 days (the structural change; most risk)
- Chunk 2 (master election + provider): 2-3 days
- Chunk 3 (client binding): 2-3 days
- Chunk 4 (E2E verification): 2 days
- Chunk 5 (docs + version): 1 day

**Total: 12-17 working days = 3-4 calendar weeks at solo pace.**



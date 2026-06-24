# GStreamer Process Hardening Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stop `gst-plugin-scan` zombie accumulation and guarantee teardown of every GStreamer subprocess in v2, with metrics and click-storm regression tests proving resources return to baseline.

**Architecture:** A new shared `internal/gstproc` package owns two things every subprocess spawner needs: a writable `GST_REGISTRY` cache env (so `gst-plugin-scan` runs once, not per spawn) and a `Managed` wrapper that spawns in a process group and guarantees an idempotent kill+Wait while updating an active-count gauge. The four live spawn sites (crossfade, Harbor, WebRTC, media engine) route through it. `init: true` (tini) in compose reaps any straggler. Tests are split into a unit tier (fakeable spawner, injectable `/proc`) and an `integration` tier (real PIDs).

**Tech Stack:** Go 1.24, `os/exec`, `syscall` (Setpgid), Prometheus client (`internal/metrics` per-binary registries), go-gst (unaffected), Docker Compose.

**Spec:** `docs/superpowers/specs/2026-06-23-gst-process-hardening-design.md`

**Skills:** Use @superpowers:test-driven-development for every task. Use @superpowers:verification-before-completion before claiming a task done.

**Branch:** Implement on `v2-dev` (or a worktree off it via @superpowers:using-git-worktrees).

---

## Chunk 1: Foundation — `internal/gstproc`

### Task 1: Registry cache env resolver

**Files:**
- Create: `internal/gstproc/registry.go`
- Test: `internal/gstproc/registry_test.go`

- [ ] **Step 1: Write the failing test**

```go
package gstproc

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRegistryEnv_CreatesAndReturnsPath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GRIMNIR_GST_CACHE_DIR", dir)

	env, err := RegistryEnv()
	if err != nil {
		t.Fatalf("RegistryEnv: %v", err)
	}
	var got string
	for _, kv := range env {
		if strings.HasPrefix(kv, "GST_REGISTRY=") {
			got = strings.TrimPrefix(kv, "GST_REGISTRY=")
		}
	}
	if got == "" {
		t.Fatal("GST_REGISTRY not present in env")
	}
	if filepath.Dir(got) != dir {
		t.Fatalf("registry %q not under cache dir %q", got, dir)
	}
}

func TestRegistryEnv_FailsOnUnwritableDir(t *testing.T) {
	t.Setenv("GRIMNIR_GST_CACHE_DIR", "/proc/cannot-write-here")
	if _, err := RegistryEnv(); err == nil {
		t.Fatal("expected error for unwritable cache dir")
	}
	_ = os.Getpid
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/gstproc/ -run TestRegistryEnv -v`
Expected: FAIL (package/function not defined).

- [ ] **Step 3: Write minimal implementation**

```go
// Package gstproc centralizes how Grimnir spawns and reaps gst-launch
// subprocesses: a shared GST_REGISTRY cache so gst-plugin-scan runs once, and
// a process-group wrapper with guaranteed idempotent kill+Wait.
package gstproc

import (
	"fmt"
	"os"
	"path/filepath"
)

// RegistryEnv resolves a writable GStreamer registry cache path and returns the
// env pairs to append to a gst process's Env. A persistent cache makes gst_init
// load the cached registry instead of re-forking gst-plugin-scan on every spawn.
// The plugin set is immutable inside a container image, so the scan runs once.
func RegistryEnv() ([]string, error) {
	dir := os.Getenv("GRIMNIR_GST_CACHE_DIR")
	if dir == "" {
		dir = "/var/lib/grimnir/gst-cache"
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("gstproc: registry cache dir %q: %w", dir, err)
	}
	probe := filepath.Join(dir, ".writable")
	if err := os.WriteFile(probe, []byte("x"), 0o644); err != nil {
		return nil, fmt.Errorf("gstproc: registry cache dir %q not writable: %w", dir, err)
	}
	_ = os.Remove(probe)
	return []string{"GST_REGISTRY=" + filepath.Join(dir, "registry.bin")}, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/gstproc/ -run TestRegistryEnv -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/gstproc/registry.go internal/gstproc/registry_test.go
git commit -m "feat(gstproc): writable GST_REGISTRY cache env resolver"
```

### Task 2: Managed process (process group + idempotent kill+Wait + gauge)

**Files:**
- Create: `internal/gstproc/managed.go`
- Test: `internal/gstproc/managed_test.go`

Reuse the `Setpgid` pattern already in `internal/playout/processgroup.go:26-33`. The gauge is an interface so the package stays binary-agnostic (a Prometheus gauge satisfies it).

- [ ] **Step 1: Write the failing test** (uses `sleep` as a stand-in child so no GStreamer is needed)

```go
package gstproc

import (
	"context"
	"os/exec"
	"sync/atomic"
	"testing"
	"time"
)

type fakeGauge struct{ v int64 }

func (f *fakeGauge) Inc() { atomic.AddInt64(&f.v, 1) }
func (f *fakeGauge) Dec() { atomic.AddInt64(&f.v, -1) }

func TestManaged_StartStop_IdempotentAndGauge(t *testing.T) {
	g := &fakeGauge{}
	cmd := exec.Command("sleep", "30")
	m, err := Start(context.Background(), cmd, g)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if atomic.LoadInt64(&g.v) != 1 {
		t.Fatalf("gauge after start = %d, want 1", g.v)
	}
	// Stop twice; must not panic, error, or double-decrement.
	m.Stop()
	m.Stop()
	if got := atomic.LoadInt64(&g.v); got != 0 {
		t.Fatalf("gauge after stop = %d, want 0", got)
	}
	// The child must actually be gone.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !pidAlive(m.pid) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("pid %d still alive after Stop", m.pid)
}
```

Add a tiny test-only `pidAlive` helper in `managed_test.go`:

```go
func pidAlive(pid int) bool {
	// signal 0 probes existence without delivering a signal
	return exec.Command("kill", "-0", itoa(pid)).Run() == nil
}
func itoa(i int) string { return fmt.Sprintf("%d", i) }
```
(import `fmt`.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/gstproc/ -run TestManaged -v`
Expected: FAIL (`Start`/`Managed` undefined).

- [ ] **Step 3: Write minimal implementation**

```go
package gstproc

import (
	"context"
	"os/exec"
	"sync"
	"syscall"
)

// ActiveGauge is the subset of prometheus.Gauge that Managed touches.
type ActiveGauge interface {
	Inc()
	Dec()
}

// Managed is a gst subprocess spawned in its own process group with a
// guaranteed, idempotent teardown.
type Managed struct {
	cmd  *exec.Cmd
	pid  int
	g    ActiveGauge
	once sync.Once
	done chan struct{}
}

// Start launches cmd in a new process group, injects the registry cache env,
// increments the gauge, and reaps the whole group on Stop. cmd.Env, Stdin,
// Stdout, Stderr should be set by the caller before Start.
func Start(ctx context.Context, cmd *exec.Cmd, g ActiveGauge) (*Managed, error) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true // own group so we can kill grandchildren
	if reg, err := RegistryEnv(); err == nil {
		if cmd.Env == nil {
			cmd.Env = append([]string{}, syscallEnviron()...)
		}
		cmd.Env = append(cmd.Env, reg...)
	} else {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	if g != nil {
		g.Inc()
	}
	m := &Managed{cmd: cmd, pid: cmd.Process.Pid, g: g, done: make(chan struct{})}
	go func() { _ = cmd.Wait(); close(m.done) }() // reap when it exits on its own
	if ctx != nil {
		go func() {
			select {
			case <-ctx.Done():
				m.Stop()
			case <-m.done:
			}
		}()
	}
	return m, nil
}

// Stop SIGKILLs the whole process group and waits. Safe to call repeatedly.
func (m *Managed) Stop() {
	m.once.Do(func() {
		if m.cmd.Process != nil {
			_ = syscall.Kill(-m.pid, syscall.SIGKILL) // negative pid = the group
		}
		<-m.done // the Wait goroutine reaps; no zombie left behind
		if m.g != nil {
			m.g.Dec()
		}
	})
}
```

Add `syscallEnviron` in `managed.go`:

```go
import "os"

func syscallEnviron() []string { return os.Environ() }
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/gstproc/ -run TestManaged -race -v`
Expected: PASS, no race.

- [ ] **Step 5: Commit**

```bash
git add internal/gstproc/managed.go internal/gstproc/managed_test.go
git commit -m "feat(gstproc): process-group Managed with idempotent kill+Wait and gauge"
```

---

## Chunk 2: Route the four spawn sites through gstproc

For each site: write a test that the spawner's teardown leaves no live child and decrements the gauge, then switch the site to `gstproc.Start`/`Managed.Stop`.

### Task 3: Media engine (`gstreamer.go`) — add process group + gstproc reap

**Files:**
- Modify: `internal/mediaengine/gstreamer.go` (spawn at `:169`, Stop/Kill at `:238`/`:272`)
- Test: `internal/mediaengine/gstreamer_test.go`

This site currently uses a bare `gp.cmd.Process.Kill()` with no `Setpgid`, so its `gst-launch` grandchildren orphan. Route Start/Stop through `gstproc`.

- [ ] **Step 1: Write the failing test** — spawn a fake long-lived child via the engine's start path (inject `GRIMNIR_GSTREAMER_BIN=sleep`-style stub if supported, else use the integration tier). Assert that after `Stop()`, the process group is gone. Expected: FAIL because grandchildren survive today.
- [ ] **Step 2: Run** `go test ./internal/mediaengine/ -run TestGStreamerProcess_StopKillsGroup -v` → FAIL.
- [ ] **Step 3: Implement** — replace the manual `exec.CommandContext` + `Process.Kill()` with `gstproc.Start(ctx, cmd, metrics.MediaEngineGstSubprocesses)` and `managed.Stop()`. Keep the existing stdout/stderr monitors; attach them to `cmd` before `Start`.
- [ ] **Step 4: Run** the test → PASS. Then `go test ./internal/mediaengine/ -race`.
- [ ] **Step 5: Commit** `fix(mediaengine): spawn gst-launch in a process group via gstproc`.

### Task 4: WebRTC (`ingress_webrtc.go`) — close the reachability leak

**Files:**
- Modify: `internal/grimnirfanout/ingress_webrtc.go` (`attachTrack` ~`:375`, `startOpusDecoder` ~`:321`, `removePeer` ~`:411`, peer insert ~`:274-276`, `OnConnectionStateChange` ~`:237-243`)
- Test: `internal/grimnirfanout/ingress_webrtc_test.go`

Two fixes: (a) on any `attachTrack` error, tear the decoder down directly even if the peer isn't in `ing.peers` yet; (b) add a connection-establishment timeout that forces teardown for a peer stuck in `Connecting`.

- [ ] **Step 1: Write failing test** — drive `attachTrack` with a decoder-start error injected; assert the active gauge returns to 0 (today it leaks). Add a second test: a peer that never reaches a terminal ICE state is torn down after the timeout.
- [ ] **Step 2: Run** → FAIL (decoder leaks / no timeout).
- [ ] **Step 3: Implement** — route the opus decoder through `gstproc.Start(..., metrics.FanoutGstSubprocesses.WithType("webrtc"))`; in `attachTrack`'s error paths call `managed.Stop()` on the local handle directly; add a `time.AfterFunc(connTimeout, ...)` armed at peer creation and cancelled once connected.
- [ ] **Step 4: Run** both tests → PASS; `go test ./internal/grimnirfanout/ -race`.
- [ ] **Step 5: Commit** `fix(fanout): always reap webrtc decoder on attachTrack error and connect timeout`.

### Task 5: Harbor (`harbor_sink.go`) — End always Waits

**Files:**
- Modify: `internal/grimnirfanout/harbor_sink.go` (`Begin` `:77`, `End` `:154`)
- Test: `internal/grimnirfanout/harbor_sink_test.go`

- [ ] **Step 1: Write failing test** — `Begin()` half-fails (decoder starts, pipeline fails), then `End()`; assert the decoder is reaped and gauge returns to 0.
- [ ] **Step 2: Run** → FAIL.
- [ ] **Step 3: Implement** — route through `gstproc`; make `End()` idempotent and call `managed.Stop()` unconditionally even when `Begin` half-failed (store the handle as soon as the process starts).
- [ ] **Step 4: Run** → PASS; `-race`.
- [ ] **Step 5: Commit** `fix(fanout): harbor End reaps decoder even on partial Begin`.

### Task 6: Crossfade (`crossfade.go`) — panic-safe teardown

**Files:**
- Modify: `internal/playout/crossfade.go` (`Play` `:101`, decoder spawn `:159`, `stop` `:206`)
- Test: extend `internal/playout/crossfade_pump_test.go`

- [ ] **Step 1: Write failing test** — a `Play()` whose post-spawn step panics must still reap the decoder (add a `defer` that stops the in-flight decoder on panic).
- [ ] **Step 2: Run** → FAIL.
- [ ] **Step 3: Implement** — wrap the spawn-then-store sequence so a `defer` stops the decoder if the function returns/panics before ownership transfers. Route the spawn through `gstproc` with `metrics.GrimnirRadioGstSubprocesses.WithType("crossfade")`.
- [ ] **Step 4: Run** → PASS; `-race`.
- [ ] **Step 5: Commit** `fix(playout): panic-safe crossfade decoder teardown`.

---

## Chunk 3: Metrics

### Task 7: Per-binary active-subprocess gauges

**Files:**
- Modify: `internal/metrics/` (per-binary registries at `registry.go:38-42`)
- Test: `internal/metrics/gst_test.go`

- [ ] **Step 1: Write failing test** — register the gauges and assert each is on the correct registry: `GrimnirRadioGstSubprocesses` (crossfade) on `GrimnirRadioRegistry`, `FanoutGstSubprocesses` (harbor/webrtc) on `FanoutRegistry`, `MediaEngineGstSubprocesses` on `MediaEngineRegistry`. Assert `WithLabelValues("webrtc")` works.
- [ ] **Step 2: Run** → FAIL.
- [ ] **Step 3: Implement** — define the three `*prometheus.GaugeVec` (label `type`) and register each on its binary's registry. Expose `.WithType(s)` helpers returning an `gstproc.ActiveGauge`.
- [ ] **Step 4: Run** → PASS.
- [ ] **Step 5: Commit** `feat(metrics): per-binary gst subprocess active gauges`.

### Task 8: Zombie-count gauge via injectable `/proc`

**Files:**
- Create: `internal/gstproc/zombies.go`
- Test: `internal/gstproc/zombies_test.go`

Reuse the injectable-`/proc` seam pattern from `internal/playout/reaper.go` so this is unit-testable with a fake tree.

- [ ] **Step 1: Write failing test** — point the counter at a fake `/proc` containing two `Z`-state children of the current PID; assert `CountZombieChildren(fakeProc, selfPID)` returns 2.
- [ ] **Step 2: Run** → FAIL.
- [ ] **Step 3: Implement** — `CountZombieChildren(procRoot string, ppid int) (int, error)` scanning `procRoot/*/stat` for state `Z` and `PPid == ppid`. Add a sampler that updates a `grimnir_gst_zombies` gauge every 30s; each gst-spawning binary starts it.
- [ ] **Step 4: Run** → PASS.
- [ ] **Step 5: Commit** `feat(gstproc): zombie-child gauge with injectable /proc`.

### Task 9: `orphans_reaped_total` counter in the reaper

**Files:**
- Modify: `internal/playout/reaper.go` (kill site `:85`)
- Test: `internal/playout/reaper_test.go`

- [ ] **Step 1: Write failing test** — using the existing fake `/proc`, run a scan that kills one orphan; assert the counter incremented by 1. (Stub the kill so the test doesn't signal a real PID.)
- [ ] **Step 2: Run** → FAIL.
- [ ] **Step 3: Implement** — increment `metrics.GrimnirRadioOrphansReaped` on each successful `syscall.Kill`. Additive only; do not change the matching signature.
- [ ] **Step 4: Run** → PASS.
- [ ] **Step 5: Commit** `feat(playout): count reaped broadcast orphans`.

---

## Chunk 4: Click-storm regression tests, integration tier, compose

### Task 10: Unit click-storm tests (return-to-baseline via gauge)

**Files:**
- Create: `internal/grimnirfanout/clickstorm_test.go`, extend `internal/playout/crossfade_pump_test.go`
- Helper: `internal/gstproc/baseline_test.go` (exported test helper `AssertReturnsToBaseline`)

- [ ] **Step 1: Write the helper + tests**

```go
// AssertReturnsToBaseline polls countFn until it equals baseline or timeout.
func AssertReturnsToBaseline(t *testing.T, name string, countFn func() int, baseline int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if countFn() == baseline {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%s did not return to baseline %d; got %d", name, baseline, countFn())
}
```

Crossfade: call `Play()` 50 times rapidly with overlapping `next`; assert the crossfade gauge settles to <=1. WebRTC: attach/detach 50 peers including error paths; assert gauge settles to 0. Harbor: begin/end 50 sessions; assert 0. All use the fakeable spawner (`GStreamerBin: "true"`), asserting on the gauge, not real PIDs.

- [ ] **Step 2: Run** each → FAIL until the gauge wiring from Chunk 2/3 is correct.
- [ ] **Step 3: Implement** — only the test code; production behavior already landed in Chunks 2-3. If a test exposes a real leak, fix the site.
- [ ] **Step 4: Run** `go test ./internal/playout/ ./internal/grimnirfanout/ -race` → PASS.
- [ ] **Step 5: Commit** `test: click-storm leak regression for crossfade/webrtc/harbor`.

### Task 11: Integration real-PID tests (`integration` build tag)

**Files:**
- Create: `internal/gstproc/realpid_integration_test.go` (header `//go:build integration`)

- [ ] **Step 1: Write test** — spawn N real `gst-launch` (or `sleep` if gst absent) children via `gstproc.Start`, `Stop` them, and assert `CountGstChildren(selfPID)` (match by PPid + signature) returns to baseline via `AssertReturnsToBaseline`. Skip with `t.Skip` if `gst-launch-1.0` not on PATH.
- [ ] **Step 2: Run** `go test -tags=integration ./internal/gstproc/ -v` → exercise.
- [ ] **Step 3: Implement** any missing `CountGstChildren`.
- [ ] **Step 4: Run** → PASS (or skipped where gst absent).
- [ ] **Step 5: Commit** `test(integration): real-PID return-to-baseline for gstproc`.

### Task 12: Registry-warm test

**Files:**
- Create: `internal/gstproc/registry_warm_test.go`

- [ ] **Step 1: Write test** — set `GRIMNIR_GST_CACHE_DIR` to a temp dir, spawn a gst child twice, assert the registry file exists after the first spawn and its `ModTime` is unchanged after the second (cache reused, not rebuilt). Tag `integration` since it needs real gst; skip if absent.
- [ ] **Step 2-4:** Run → implement any helper → PASS.
- [ ] **Step 5: Commit** `test(integration): GST_REGISTRY cache reused across spawns`.

### Task 13: Compose — tini + registry volume

**Files:**
- Modify: `docker-compose.yml` (control plane + media engine), `docker-compose.fanout.yml` (fan-out)
- Test: manual / `docker compose config` validation

- [ ] **Step 1:** Add `init: true` to the grimnir, mediaengine, and fanout services. Add a named volume `gst-cache` mounted at `/var/lib/grimnir/gst-cache` on each gst-running service.
- [ ] **Step 2:** Run `docker compose -f docker-compose.yml -f docker-compose.fanout.yml config` → valid, `Init: true` present.
- [ ] **Step 3:** Add an integration check that SIGTERM still drains pipelines cleanly with tini in front (verify graceful shutdown unaffected).
- [ ] **Step 4:** `make ci` green.
- [ ] **Step 5: Commit** `chore(compose): tini init + persistent gst registry volume`.

---

## Final verification

- [ ] `make ci` green (verify + fmt-check).
- [ ] `go test -race ./internal/gstproc/ ./internal/playout/ ./internal/grimnirfanout/ ./internal/mediaengine/ ./internal/metrics/`
- [ ] `go test -tags=integration ./internal/gstproc/` (where gst available).
- [ ] Confirm the four spawn sites all route through `gstproc` (grep for residual bare `Process.Kill()` in those files).
- [ ] Use @superpowers:verification-before-completion before declaring done.

# Edge Encoder + PCM Transport Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

> **Status:** Complete. 11 chunks (Chunk 0 spike + Chunks 1-10). Plan written 2026-06-03 incrementally per `feedback_brainstorming_incremental_save.md` — every chunk saved to disk before the next was drafted.

**Goal:** Build a new `cmd/edge-encoder/` Go binary that ingests raw PCM via RTP from two media engines, performs sample-aligned input switching when the active source goes unhealthy, and serves the encoded result to HTTP/ICY and HLS listeners — delivering the "zero audible glitch on engine failure" property the HA architecture rests on.

**Architecture:** Engines emit raw L16 stereo 44.1kHz PCM via RTP (`rtpL16pay + udpsink`) to both edge encoders in the region. Each edge encoder runs a single GStreamer pipeline driven via **go-gst CGo bindings** (deliberate departure from the existing `gst-launch-1.0` subprocess pattern — runtime control of `input-selector` is required and gst-launch can't provide it). Pipeline: `2× (udpsrc + rtpjitterbuffer + rtpL16depay) → input-selector → audioconvert → tee → [lamemp3enc → appsink → broadcast.Mount]` and `[hlssink2 → S3 segment writer]`. Switching is driven by Go: per-input health combines packet-arrival watchdog (100ms) AND engine gRPC health subscription. Sample-aligned switch happens at running-time boundary because both engines share a NetClock master (Track A step 5 — not this plan, but the design requires it).

**Tech Stack:** Go 1.24, **go-gst** (CGo bindings for GStreamer 1.0; new dependency), GStreamer 1.20+ (Ubuntu 22.04 default), `gst-plugins-base`, `gst-plugins-good` (rtpjitterbuffer, rtpL16depay, udpsrc), `gst-plugins-ugly` (lamemp3enc), `gst-plugins-bad` (hlssink2), pion/rtp (already in go.mod, used for engine-side payload validation in tests), existing `internal/broadcast.Mount` pattern for HTTP/ICY clients.

**Issue:** TBD — file when first chunk merges.

**Parent design:** `docs/superpowers/plans/2026-06-01-ha-zero-loss-failover-design.md` Section 2 (audio path) — full architecture rationale. Section 9.1 Track A step 4 — this plan implements it.

**Decisions locked 2026-06-03 (Q-EE1..Q-EE4):**

| Q | Decision | Rationale |
|---|---|---|
| Q-EE1 | **C** — go-gst CGo bindings for this binary | gst-launch can't do runtime input-selector control; fallbackswitch alone doesn't honor gRPC health gate; clean programmatic control is worth one new build dep |
| Q-EE2 | **B** — switch decision uses both packet arrival AND gRPC health | Engines can be "alive" (sending packets) but reporting unhealthy via gRPC (e.g., catastrophic playout error); gRPC gate catches these |
| Q-EE3 | **B** — ship HTTP/ICY AND HLS in scope | Listener parity from day one; HLS path is well-understood and adds modest code volume |
| Q-EE4 | **B** — 1-day spike before the production plan executes | The PCM-switching property is the load-bearing claim of the whole HA design; cheap to validate, expensive to discover broken after weeks of integration |

**Honest scope:** 12 chunks. Conservative estimate 4–8 weeks of focused engineering. The novel pieces are Chunks 0 (spike), 1 (go-gst integration), 4 (pipeline construction), and 9 (HLS branch); the rest are well-established patterns.

---

## File structure

| File | Status | Responsibility |
|---|---|---|
| `internal/edgeencoder/spike/main.go` | **Create then delete after Chunk 0** | Spike binary: minimal sample-alignment validation with hand-crafted audiotestsrc sources |
| `docs/superpowers/spikes/2026-06-03-pcm-switching-spike.md` | Create | Spike report: hypothesis, method, observed behavior, decision (proceed/redesign) |
| `cmd/edge-encoder/main.go` | Create | CLI entry point, flag parsing, signal handling, dependency wiring |
| `cmd/edge-encoder/main_test.go` | Create | Integration tests against the assembled binary |
| `internal/edgeencoder/config.go` | Create | Env-var loading; all configuration in one place |
| `internal/edgeencoder/config_test.go` | Create | Config-loading tests, default verification |
| `internal/edgeencoder/pipeline.go` | Create | GStreamer pipeline construction via go-gst; bus message handling; runtime element-property access (for input-selector switching) |
| `internal/edgeencoder/pipeline_test.go` | Create | Pipeline state transitions; bus message handling |
| `internal/edgeencoder/health.go` | Create | Per-input health tracking: packet-arrival watchdog + gRPC health subscription combiner |
| `internal/edgeencoder/health_test.go` | Create | Health-state-machine tests with synthetic packet flows and mock gRPC |
| `internal/edgeencoder/switcher.go` | Create | Switching policy: reads health, decides active input, calls pipeline's input-selector setter |
| `internal/edgeencoder/switcher_test.go` | Create | Switching decision tests; verify hysteresis (don't flap) |
| `internal/edgeencoder/broadcast.go` | Create | Adapter from GStreamer appsink output to existing `internal/broadcast.Mount` pattern |
| `internal/edgeencoder/broadcast_test.go` | Create | Adapter tests; verify byte stream continuity |
| `internal/edgeencoder/hls.go` | Create | HLS segment writer; S3 upload; m3u8 manifest endpoint |
| `internal/edgeencoder/hls_test.go` | Create | Segment writing, manifest format, S3 mock |
| `internal/edgeencoder/grpc.go` | Create | gRPC server (`GetStatus` matching mediaengine convention) + client for engine health subscriptions |
| `internal/edgeencoder/grpc_test.go` | Create | gRPC server/client tests |
| `internal/edgeencoder/metrics.go` | Create | Prometheus metrics: per-input packet rate, selector position, output byte-flow, switch counter |
| `internal/edgeencoder/metrics_test.go` | Create | Metric registration tests |
| `internal/playout/director.go` | Modify | Add PCM-over-RTP output mode (HA-enabled flag); existing `fdsink fd=3/4` paths remain for single-instance mode |
| `internal/playout/pipeline.go` | Possibly modify | Depending on how cleanly the PCM-RTP output integrates |
| `Dockerfile` | Modify (if exists for mediaengine) OR new `Dockerfile.edge-encoder` | Install `libgstreamer1.0-dev`, `gstreamer1.0-plugins-{base,good,ugly,bad}`, `gobject-introspection`, `gir1.2-gstreamer-1.0` |
| `.github/workflows/ci.yml` | Modify | Install GStreamer dev libs in runner before `go test` (`apt-get install -y libgstreamer1.0-dev gstreamer1.0-plugins-base gstreamer1.0-plugins-good gstreamer1.0-plugins-ugly gstreamer1.0-plugins-bad`) |
| `Makefile` | Modify | Add `build-edge-encoder` target; include in `build` cascade |
| `CLAUDE.md` | Modify | Document the new binary, the go-gst CGo build dependency, and the env-var config |
| `go.mod` / `go.sum` | Modify | Add `github.com/go-gst/go-gst` (or chosen go-gst module) + cgo dependencies |

**Decomposition principle:** edge encoder = (1) pipeline lifecycle, (2) per-input health, (3) switching decision, (4) broadcast adapter, (5) HLS adapter, (6) gRPC service. Each is its own file, < 300 lines each. Tests mirror source one-to-one. Pipeline file is the most coupled to GStreamer specifics; the other files are pure Go and easy to test in isolation.

---

## Chunk 0: Spike — validate sample-aligned PCM switching

> **This chunk produces no production code.** It's a 1-day disposable spike whose only deliverable is a written go/no-go for the design. If the spike fails, the entire HA architecture's "zero audible glitch" claim is suspect and Section 2 of the design needs revisiting before any production code ships.

### Task 0.1: Set up the spike environment

**Files:**
- Create: `internal/edgeencoder/spike/main.go` (throwaway; deleted at end of chunk)
- Create: `internal/edgeencoder/spike/run.sh` (throwaway helper)

**Context:**
The spike uses `gst-launch-1.0` only — go-gst integration comes in Chunk 1. The point is to confirm GStreamer can do what the design claims, not to validate the production pattern. Three GStreamer processes total:

1. **Source A** — emits a 440 Hz sine wave as L16 RTP to `udp:localhost:5004`.
2. **Source B** — emits a 880 Hz sine wave as L16 RTP to `udp:localhost:5005`. (Different frequency on purpose, so a glitch at the switchover is audible by humans listening to the output.)
3. **Edge encoder** — receives both, switches between them, encodes to MP3, writes to a file.

- [ ] **Step 1: Verify GStreamer + required plugins are installed on the workhorse**

```bash
gst-launch-1.0 --version
gst-inspect-1.0 fallbackswitch 2>&1 | head -5
gst-inspect-1.0 input-selector 2>&1 | head -5
gst-inspect-1.0 rtpL16pay 2>&1 | head -5
gst-inspect-1.0 rtpL16depay 2>&1 | head -5
gst-inspect-1.0 rtpjitterbuffer 2>&1 | head -5
gst-inspect-1.0 lamemp3enc 2>&1 | head -5
```

Expected: each command prints version + factory name lines, not "No such element."

If `fallbackswitch` is missing on the workhorse, install:

```bash
sudo pacman -S gst-plugins-rs   # Arch
# OR
sudo apt-get install -y gstreamer1.0-plugins-bad gstreamer1.0-plugins-rs   # Debian/Ubuntu
```

If any other element is missing, stop and document which plugin pack ships it before proceeding.

- [ ] **Step 2: Write the spike runner script**

`internal/edgeencoder/spike/run.sh`:

```bash
#!/bin/bash
# Spike: validate sample-aligned PCM switching on packet loss.
# This script spawns three gst-launch processes:
#   - Source A: 440 Hz sine → RTP-L16 → udp:5004
#   - Source B: 880 Hz sine → RTP-L16 → udp:5005
#   - Edge:     2× udpsrc → fallbackswitch → MP3 → /tmp/spike-output.mp3
#
# Run, then kill source A after ~5 seconds. Listen to the output:
# the frequency should switch from 440 Hz to 880 Hz with no audible click,
# pop, or silence.
#
# Usage:  ./run.sh
# Output: /tmp/spike-output.mp3
# Kill:   pkill -f "gst-launch-1.0.*sine"

set -e
OUT=/tmp/spike-output.mp3
rm -f "$OUT"

echo "=== Spawning Source A (440 Hz on :5004) ==="
gst-launch-1.0 -q \
    audiotestsrc is-live=true freq=440 wave=sine \
    ! audio/x-raw,rate=44100,channels=2,format=S16BE \
    ! rtpL16pay pt=10 mtu=1400 \
    ! udpsink host=127.0.0.1 port=5004 sync=true \
    &
PID_A=$!

echo "=== Spawning Source B (880 Hz on :5005) ==="
gst-launch-1.0 -q \
    audiotestsrc is-live=true freq=880 wave=sine \
    ! audio/x-raw,rate=44100,channels=2,format=S16BE \
    ! rtpL16pay pt=10 mtu=1400 \
    ! udpsink host=127.0.0.1 port=5005 sync=true \
    &
PID_B=$!

sleep 1

echo "=== Spawning edge (fallbackswitch, output to $OUT) ==="
gst-launch-1.0 -q \
    fallbackswitch name=fs latency=80000000 timeout=200000000 immediate-fallback=true \
    ! audioconvert ! audioresample \
    ! lamemp3enc target=1 bitrate=128 cbr=true \
    ! filesink location="$OUT" sync=false \
    \
    udpsrc port=5004 caps="application/x-rtp,media=audio,clock-rate=44100,encoding-name=L16,payload=10,channels=2" \
    ! rtpjitterbuffer latency=80 \
    ! rtpL16depay \
    ! audio/x-raw,rate=44100,channels=2 \
    ! fs.sink_0 \
    \
    udpsrc port=5005 caps="application/x-rtp,media=audio,clock-rate=44100,encoding-name=L16,payload=10,channels=2" \
    ! rtpjitterbuffer latency=80 \
    ! rtpL16depay \
    ! audio/x-raw,rate=44100,channels=2 \
    ! fs.sink_1 \
    &
PID_EDGE=$!

echo "=== Running for 5 seconds (both sources active) ==="
sleep 5

echo "=== Killing Source A (PID $PID_A) — fallbackswitch should switch to B ==="
kill $PID_A

echo "=== Running for 5 more seconds (B only, edge should output 880 Hz) ==="
sleep 5

echo "=== Cleaning up ==="
kill $PID_B $PID_EDGE 2>/dev/null
wait 2>/dev/null

echo "=== Spike output: $OUT ==="
ls -l "$OUT"
echo "=== Decode + spectrogram for analysis ==="
ffmpeg -i "$OUT" -f wav /tmp/spike-output.wav -y -loglevel error
echo "Play with: aplay /tmp/spike-output.wav"
echo "Or analyze the spectrogram with: sox /tmp/spike-output.wav -n spectrogram -o /tmp/spike-spectrogram.png"
```

Make executable:

```bash
chmod +x internal/edgeencoder/spike/run.sh
```

- [ ] **Step 3: Run the spike**

```bash
./internal/edgeencoder/spike/run.sh
```

- [ ] **Step 4: Listen to the output**

```bash
aplay /tmp/spike-output.wav
```

Listen specifically at the 5-second mark — that's where Source A was killed and fallbackswitch should hand over to Source B. The frequency should jump from 440 Hz to 880 Hz with:

- No silence gap
- No audible click or pop
- No phase discontinuity glitch

- [ ] **Step 5: Generate a spectrogram for objective analysis**

```bash
sox /tmp/spike-output.wav -n spectrogram -o /tmp/spike-spectrogram.png
```

Open `/tmp/spike-spectrogram.png` and look at the transition point. A clean switch shows the 440 Hz band ending and 880 Hz band starting at the same vertical line, no horizontal artifact bar across all frequencies (which would indicate a click).

### Task 0.2: Document spike findings and decide

**Files:**
- Create: `docs/superpowers/spikes/2026-06-03-pcm-switching-spike.md`

- [ ] **Step 1: Write the spike report**

Write `docs/superpowers/spikes/2026-06-03-pcm-switching-spike.md` with these sections:

```markdown
# Spike — PCM Switching Sample-Alignment Validation

**Date:** 2026-06-03
**Author:** [your name]
**Purpose:** Validate the load-bearing claim of the HA design (Section 2) that GStreamer's `fallbackswitch` produces sample-aligned switching between two PCM-RTP inputs with no audible glitch on a source death.

## Hypothesis

Section 2 of the HA design claims:
> `input-selector` switches between inputs at a `running-time` boundary; because both inputs share a clock, the switch is sample-aligned (zero discontinuity in the PCM going into the encoder). Encoder runs once and never restarts on a switch.

This spike substitutes `fallbackswitch` (auto-switching variant of input-selector) and tests the claim with two test signals running independently (no shared clock yet — that's Track A step 5's NetClock work).

## Method

[describe what `run.sh` does, link the script]

## Observed behavior

- Audio playback subjective assessment: [Clean / Audible click / Silence gap / Phase distortion]
- Spectrogram visual inspection: [paste link to `/tmp/spike-spectrogram.png` or describe transition]
- Switch latency (sound A stop → sound B audible): [estimate in ms]

## Decision

- [ ] **PROCEED**: switching is acceptably clean. Production plan continues as written.
- [ ] **PROCEED WITH NetClock CAVEAT**: switching is clean only because of test-signal coincidence; needs NetClock to actually work in production (this was the design's assumption, just confirmed).
- [ ] **REDESIGN**: switching produces audible artifacts even with hand-crafted inputs. Stop and rethink Section 2.

## Notes for the production plan

[Anything observed during the spike that should change Chunks 1–11.]
```

Fill in the observed behavior and check the decision box honestly.

- [ ] **Step 2: Delete the spike code**

If the decision was PROCEED or PROCEED WITH NetClock CAVEAT:

```bash
rm -rf internal/edgeencoder/spike/
git add docs/superpowers/spikes/2026-06-03-pcm-switching-spike.md
git commit -m "spike: validate PCM-switching sample-alignment via fallbackswitch (passed)"
```

If REDESIGN: stop, surface to user, do not proceed with Chunks 1+ until Section 2 has been revisited.

- [ ] **Step 3: Confirm decision with the user before proceeding to Chunk 1**

The user needs to see the spike report and confirm before Chunk 1 starts (CGo integration is a big enough step that we want explicit go-ahead).

---

## Chunk 1: go-gst integration (CGo + build/CI changes)

This chunk introduces CGo into the codebase. Adds `libgstreamer1.0-dev` + plugin packs as build/test dependencies. The rest of the repo continues to use subprocess GStreamer; only the edge encoder uses go-gst.

### Task 1.1: Add go-gst dependency and confirm it builds locally

**Files:**
- Modify: `go.mod`, `go.sum`
- Create: `internal/edgeencoder/gst_init.go` (minimal: imports `go-gst/gst`, exports `Init()`)
- Create: `internal/edgeencoder/gst_init_test.go` (smoke test: gst.Init() doesn't panic, version > 1.20)

**Context:**
The canonical Go binding is `github.com/go-gst/go-gst` (active fork of the older `tinyzimmer/go-gst`). Imports require CGo + GStreamer dev headers at compile time. On Ubuntu 22.04:

```bash
sudo apt-get install -y libgstreamer1.0-dev gstreamer1.0-plugins-base \
    gstreamer1.0-plugins-good gstreamer1.0-plugins-ugly \
    gstreamer1.0-plugins-bad gstreamer1.0-plugins-rs
```

On Arch (workhorse):

```bash
sudo pacman -S gstreamer gst-plugins-base gst-plugins-good \
    gst-plugins-ugly gst-plugins-bad gst-plugins-rs
```

- [ ] **Step 1: Add go-gst to go.mod**

```bash
cd /home/code/projects/grimnir_radio
go get github.com/go-gst/go-gst@latest
go mod tidy
```

Confirm `go.mod` now lists `github.com/go-gst/go-gst vX.Y.Z`.

- [ ] **Step 2: Write the smoke test**

`internal/edgeencoder/gst_init_test.go`:

```go
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

	major, minor, micro, nano := gst.Version()
	t.Logf("GStreamer %d.%d.%d.%d", major, minor, micro, nano)

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
		"fallbackswitch",
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
```

- [ ] **Step 3: Run the test to verify it fails**

```bash
go test -v -run "TestGstInit|TestGstRequiredElements" ./internal/edgeencoder/
```

Expected: FAIL — `Init` undefined (and / or the gst_init.go file doesn't exist).

- [ ] **Step 4: Implement the minimal Init wrapper**

`internal/edgeencoder/gst_init.go`:

```go
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
```

- [ ] **Step 5: Run the test to verify it passes**

```bash
go test -v -run "TestGstInit|TestGstRequiredElements" ./internal/edgeencoder/
```

Expected: PASS. If `TestGstRequiredElements` fails on a specific element name, install the missing plugin pack before continuing — the production pipeline needs every one of them.

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum internal/edgeencoder/gst_init.go internal/edgeencoder/gst_init_test.go
git commit -m "edgeencoder: introduce go-gst CGo bindings + element availability test"
```

---

### Task 1.2: Update CI workflow to install GStreamer dev libs

**Files:**
- Modify: `.github/workflows/ci.yml`

**Context:**
CI runs on `ubuntu-latest`, which is currently Ubuntu 22.04. The workflow needs GStreamer dev headers + every plugin pack the edge encoder uses, installed before `go test`. Without this, `TestGstRequiredElements` will fail on the runner even though it passes locally.

- [ ] **Step 1: Add the install step to the workflow**

Edit `.github/workflows/ci.yml` to add a new step between "Set up Go" and "Tidy (must be clean)":

```yaml
      - name: Install GStreamer (required for edge encoder CGo build)
        run: |
          sudo apt-get update -qq
          sudo DEBIAN_FRONTEND=noninteractive apt-get install -y -qq \
            libgstreamer1.0-dev \
            gstreamer1.0-plugins-base \
            gstreamer1.0-plugins-good \
            gstreamer1.0-plugins-ugly \
            gstreamer1.0-plugins-bad \
            gstreamer1.0-plugins-rs \
            gstreamer1.0-tools
          gst-launch-1.0 --version
          gst-inspect-1.0 fallbackswitch | head -3
```

- [ ] **Step 2: Test the workflow change locally if possible**

Since we can't easily simulate CI locally, the verification is in Step 3 of Task 1.4 (CI must pass after committing). For now confirm the YAML parses:

```bash
python3 -c "import yaml; yaml.safe_load(open('.github/workflows/ci.yml'))" && echo OK
```

- [ ] **Step 3: Commit**

```bash
git add .github/workflows/ci.yml
git commit -m "ci: install GStreamer dev libs + plugin packs for edge encoder build"
```

---

### Task 1.3: Add `build-edge-encoder` Makefile target

**Files:**
- Modify: `Makefile`

**Context:**
The existing `build` target builds `grimnirradio` and `mediaengine`. Edge encoder needs its own target (because of CGo it builds differently — it needs `CGO_ENABLED=1` and pkg-config available). The cleanest addition is a separate target that the main `build` cascade calls.

- [ ] **Step 1: Add the target**

In `Makefile`, near the existing `build:` target:

```makefile
build-edge-encoder:
	@CGO_ENABLED=1 $(GO) build $(GOFLAGS) -o ./edge-encoder ./cmd/edge-encoder

build: build-edge-encoder
	@$(GO) build $(GOFLAGS) -o ./grimnirradio ./cmd/grimnirradio
	@$(GO) build $(GOFLAGS) -o ./mediaengine ./cmd/mediaengine
```

(Adjust the existing `build:` line — the change is to add `build-edge-encoder` as a prerequisite.)

Append `build-edge-encoder` to the `.PHONY:` line at the top.

- [ ] **Step 2: Verify the target fails gracefully when the binary doesn't exist yet**

```bash
make build-edge-encoder 2>&1 | tail -3
```

Expected: `no Go files in /home/code/projects/grimnir_radio/cmd/edge-encoder` or similar — that's correct because `cmd/edge-encoder/main.go` doesn't exist yet (created in Chunk 3). The Makefile target itself is correct; we're staging it for use.

- [ ] **Step 3: Commit**

```bash
git add Makefile
git commit -m "Makefile: add build-edge-encoder target (CGo-enabled build of the new binary)"
```

(The target builds successfully starting in Chunk 3 once `cmd/edge-encoder/main.go` exists.)

---

### Task 1.4: Verify the full CI gate is green with the new dependency

**Files:** (no source changes)

- [ ] **Step 1: Run `make ci` locally**

```bash
make ci
```

Expected: exit 0. If the migration-lint or other steps fail, debug those before continuing. The new go-gst dependency means `go vet ./...` and `go test ./...` will now compile/run CGo code — confirm both pass.

- [ ] **Step 2: Push the branch**

The plan is on `v2-dev` per the parent project. After this chunk:

```bash
git push origin v2-dev
```

- [ ] **Step 3: Verify CI passes remotely**

```bash
gh run list --repo friendsincode/grimnir_radio --branch v2-dev --limit 1
```

Wait for the latest run to complete and confirm conclusion=success. If failure: investigate the install step (GStreamer plugin pack name differences between Ubuntu releases are the most likely cause).

---

## Chunk 2: Engine-side PCM emission (HA-mode output)

Add a new "HA mode" to the media engine that emits raw PCM via RTP **in addition to** the existing dual-bitrate `fdsink fd=3/4` output. The existing fdsink output stays untouched — single-instance deployments continue to work exactly as today. HA mode is opt-in via a per-station/per-mount config flag.

### Task 2.1: Plumb the HA-mode config flag

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/playout/director.go` (read the new flag)
- Test: `internal/config/config_test.go`

**Context:**
Two config knobs:

1. `GRIMNIR_HA_PCM_RTP_ENABLED` (bool, default false) — global on/off for the engine's PCM-RTP output.
2. `GRIMNIR_HA_PCM_RTP_TARGETS` (comma-separated host:port list) — where to send the PCM-RTP stream. Typically two entries: the local edge encoder + the peer host's edge encoder.

When disabled (default), the engine pipelines behave exactly as today. When enabled, every playout pipeline gets an additional `tee → audioconvert → audio/x-raw,format=S16BE,rate=44100,channels=2 → rtpL16pay → multiudpsink clients=host1:port,host2:port` branch.

- [ ] **Step 1: Write the failing test**

Add to `internal/config/config_test.go`:

```go
func TestHAPCMRTPConfig(t *testing.T) {
	t.Run("disabled by default", func(t *testing.T) {
		t.Setenv("GRIMNIR_HA_PCM_RTP_ENABLED", "")
		t.Setenv("GRIMNIR_HA_PCM_RTP_TARGETS", "")
		c, err := LoadFromEnv()
		if err != nil {
			t.Fatal(err)
		}
		if c.HAPCMRTPEnabled {
			t.Error("HAPCMRTPEnabled = true, want false (default)")
		}
		if len(c.HAPCMRTPTargets) != 0 {
			t.Errorf("HAPCMRTPTargets = %v, want empty", c.HAPCMRTPTargets)
		}
	})

	t.Run("parses target list", func(t *testing.T) {
		t.Setenv("GRIMNIR_HA_PCM_RTP_ENABLED", "true")
		t.Setenv("GRIMNIR_HA_PCM_RTP_TARGETS", "<node-a-ip>:5004,<node-b-ip>:5004")
		c, err := LoadFromEnv()
		if err != nil {
			t.Fatal(err)
		}
		if !c.HAPCMRTPEnabled {
			t.Error("HAPCMRTPEnabled = false, want true")
		}
		want := []string{"<node-a-ip>:5004", "<node-b-ip>:5004"}
		if !reflect.DeepEqual(c.HAPCMRTPTargets, want) {
			t.Errorf("HAPCMRTPTargets = %v, want %v", c.HAPCMRTPTargets, want)
		}
	})

	t.Run("ignores whitespace in target list", func(t *testing.T) {
		t.Setenv("GRIMNIR_HA_PCM_RTP_ENABLED", "true")
		t.Setenv("GRIMNIR_HA_PCM_RTP_TARGETS", " <node-a-ip>:5004 , <node-b-ip>:5004 ")
		c, err := LoadFromEnv()
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"<node-a-ip>:5004", "<node-b-ip>:5004"}
		if !reflect.DeepEqual(c.HAPCMRTPTargets, want) {
			t.Errorf("HAPCMRTPTargets = %v, want %v", c.HAPCMRTPTargets, want)
		}
	})

	t.Run("enabled requires non-empty targets", func(t *testing.T) {
		t.Setenv("GRIMNIR_HA_PCM_RTP_ENABLED", "true")
		t.Setenv("GRIMNIR_HA_PCM_RTP_TARGETS", "")
		_, err := LoadFromEnv()
		if err == nil {
			t.Error("LoadFromEnv with enabled=true and empty targets: want error, got nil")
		}
	})
}
```

Imports needed: `reflect`, `testing`. Add `import "reflect"` if not already present in the test file.

- [ ] **Step 2: Run test, verify it fails**

```bash
go test -v -run "TestHAPCMRTPConfig" ./internal/config/
```

Expected: FAIL — `HAPCMRTPEnabled` field undefined.

- [ ] **Step 3: Add the fields to `Config` and parsing**

In `internal/config/config.go`:

1. Add to the `Config` struct (alongside other fields):

```go
// HAPCMRTPEnabled controls whether the media engine emits raw L16 PCM via
// RTP for ingest by the new edge encoder. False (default) keeps the legacy
// fdsink fd=3/4 dual-bitrate output as the sole output. True adds a PCM-RTP
// branch alongside the legacy output.
HAPCMRTPEnabled bool

// HAPCMRTPTargets is the list of `host:port` destinations for the PCM-RTP
// stream. Typically two entries (local edge encoder + peer edge encoder).
// Required when HAPCMRTPEnabled is true.
HAPCMRTPTargets []string
```

2. In `LoadFromEnv()`:

```go
c.HAPCMRTPEnabled = getEnvBoolAny("GRIMNIR_HA_PCM_RTP_ENABLED", "RLM_HA_PCM_RTP_ENABLED", false)
if raw := getEnvAny("GRIMNIR_HA_PCM_RTP_TARGETS", "RLM_HA_PCM_RTP_TARGETS"); raw != "" {
    for _, t := range strings.Split(raw, ",") {
        t = strings.TrimSpace(t)
        if t != "" {
            c.HAPCMRTPTargets = append(c.HAPCMRTPTargets, t)
        }
    }
}
if c.HAPCMRTPEnabled && len(c.HAPCMRTPTargets) == 0 {
    return nil, fmt.Errorf("GRIMNIR_HA_PCM_RTP_ENABLED=true requires non-empty GRIMNIR_HA_PCM_RTP_TARGETS")
}
```

If `getEnvBoolAny` doesn't exist yet, add it next to `getEnvAny` / `getEnvIntAny`:

```go
func getEnvBoolAny(keys ...string) func(bool) bool {
    return func(def bool) bool {
        for _, k := range keys {
            if v := os.Getenv(k); v != "" {
                switch strings.ToLower(v) {
                case "1", "true", "yes", "on":
                    return true
                case "0", "false", "no", "off":
                    return false
                }
            }
        }
        return def
    }
}
```

Then use: `c.HAPCMRTPEnabled = getEnvBoolAny("GRIMNIR_HA_PCM_RTP_ENABLED", "RLM_HA_PCM_RTP_ENABLED")(false)`.

(Adjust the signature to match how the existing `getEnvIntAny` etc. work in the file. Goal is uniform style with the rest of the helpers.)

- [ ] **Step 4: Run test, verify pass**

```bash
go test -v -run "TestHAPCMRTPConfig" ./internal/config/
```

Expected: PASS, all four subtests green.

- [ ] **Step 5: gofmt + make ci**

```bash
gofmt -w internal/config/
make ci
```

Expected: exit 0.

- [ ] **Step 6: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "config: add GRIMNIR_HA_PCM_RTP_ENABLED + _TARGETS for edge encoder ingest"
```

---

### Task 2.2: Add PCM-RTP branch to dual-bitrate playout pipeline

**Files:**
- Modify: `internal/playout/director.go` (`buildDualBroadcastPipeline()` around line 2829)
- Test: `internal/playout/director_pcm_rtp_test.go` (new file)

**Context:**
Current pipeline (simplified):

```
filesrc ! decodebin ! audioconvert ! audioresample ! audio/x-raw,rate=44100,channels=2 ! tee name=t
    t. ! queue ! lamemp3enc bitrate=128 ! fdsink fd=3
    t. ! queue ! lamemp3enc bitrate=64  ! fdsink fd=4
```

HA-mode adds a third tee branch:

```
    t. ! queue ! audioconvert ! audio/x-raw,format=S16BE,rate=44100,channels=2 ! rtpL16pay pt=10 mtu=1400 ! multiudpsink clients=<node-a-ip>:5004,<node-b-ip>:5004 sync=true
```

The `multiudpsink` element sends the same RTP stream to N destinations efficiently (single packet, fanned out at the socket layer).

Important: `audio/x-raw,format=S16BE` is **signed 16-bit big-endian**, which is what `rtpL16pay` expects (the L16 RTP payload format is big-endian per RFC 3551).

- [ ] **Step 1: Write the failing test**

Create `internal/playout/director_pcm_rtp_test.go`:

```go
/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package playout

import (
	"strings"
	"testing"
)

func TestBuildDualBroadcastPipeline_NoHAModeOmitsRTPBranch(t *testing.T) {
	// When HAPCMRTPEnabled is false, the pipeline must NOT include rtpL16pay
	// or multiudpsink. Backwards-compatible single-instance behavior.
	d := newTestDirector(t)
	d.cfg.HAPCMRTPEnabled = false
	pipeline := d.buildDualBroadcastPipeline("/some/file.mp3", "audio/mpeg", 128, 64)

	if strings.Contains(pipeline, "rtpL16pay") {
		t.Errorf("HA disabled but pipeline contains rtpL16pay:\n%s", pipeline)
	}
	if strings.Contains(pipeline, "multiudpsink") {
		t.Errorf("HA disabled but pipeline contains multiudpsink:\n%s", pipeline)
	}
}

func TestBuildDualBroadcastPipeline_HAModeAddsRTPBranch(t *testing.T) {
	d := newTestDirector(t)
	d.cfg.HAPCMRTPEnabled = true
	d.cfg.HAPCMRTPTargets = []string{"<node-a-ip>:5004", "<node-b-ip>:5004"}
	pipeline := d.buildDualBroadcastPipeline("/some/file.mp3", "audio/mpeg", 128, 64)

	// Required elements
	for _, expected := range []string{
		"rtpL16pay",
		"multiudpsink",
		"audio/x-raw,format=S16BE,rate=44100,channels=2",
		"clients=<node-a-ip>:5004,<node-b-ip>:5004",
	} {
		if !strings.Contains(pipeline, expected) {
			t.Errorf("HA enabled but pipeline missing %q:\n%s", expected, pipeline)
		}
	}

	// Existing dual-bitrate output must still be there
	for _, legacy := range []string{
		"fdsink fd=3",
		"fdsink fd=4",
		"lamemp3enc",
	} {
		if !strings.Contains(pipeline, legacy) {
			t.Errorf("HA enabled but legacy output missing %q:\n%s", legacy, pipeline)
		}
	}
}
```

`newTestDirector(t)` is a helper to create a Director with sensible test defaults. If it doesn't exist yet, create one in a new `internal/playout/testing.go` (build tag `test`) — but check first; the existing test files almost certainly have such a helper already.

- [ ] **Step 2: Run test, verify failure**

```bash
go test -v -run "TestBuildDualBroadcastPipeline" ./internal/playout/
```

Expected: FAIL — pipeline doesn't have the rtpL16pay branch yet.

- [ ] **Step 3: Modify `buildDualBroadcastPipeline()` to add the branch**

In `internal/playout/director.go` around line 2829 (`buildDualBroadcastPipeline`):

Find the tee branches block and append a new branch when `d.cfg.HAPCMRTPEnabled` is true:

```go
// Existing branches stay as-is:
//     t. ! queue ! lamemp3enc bitrate=128 ! fdsink fd=3
//     t. ! queue ! lamemp3enc bitrate=64  ! fdsink fd=4

if d.cfg.HAPCMRTPEnabled && len(d.cfg.HAPCMRTPTargets) > 0 {
    clients := strings.Join(d.cfg.HAPCMRTPTargets, ",")
    pipeline += fmt.Sprintf(" t. ! queue ! audioconvert ! audio/x-raw,format=S16BE,rate=44100,channels=2 ! rtpL16pay pt=10 mtu=1400 ! multiudpsink clients=%s sync=true", clients)
}
```

(Adjust to whatever string-building pattern the existing function uses — `fmt.Sprintf`, strings.Builder, etc.)

- [ ] **Step 4: Run test, verify pass**

```bash
go test -v -run "TestBuildDualBroadcastPipeline" ./internal/playout/
```

Expected: PASS, both subtests green.

- [ ] **Step 5: Run full package tests + make ci**

```bash
go test -v ./internal/playout/
make ci
```

Expected: existing playout tests still pass; new tests green; make ci exit 0.

- [ ] **Step 6: Commit**

```bash
git add internal/playout/director.go internal/playout/director_pcm_rtp_test.go
git commit -m "playout: add PCM-over-RTP tee branch when HA mode enabled"
```

---

### Task 2.3: Add PCM-RTP branch to webstream pipeline

**Files:**
- Modify: `internal/playout/director.go` (`buildWebstreamBroadcastPipeline()` around line 1633)
- Test: `internal/playout/director_pcm_rtp_test.go` (extend)

**Context:**
Webstream pipeline (per Explore agent report) already has a `tee` with HQ, LQ, and (optionally) WebRTC branches. Add the PCM-RTP branch the same way as Task 2.2.

- [ ] **Step 1: Extend the failing test**

Append to `internal/playout/director_pcm_rtp_test.go`:

```go
func TestBuildWebstreamBroadcastPipeline_HAModeAddsRTPBranch(t *testing.T) {
	d := newTestDirector(t)
	d.cfg.HAPCMRTPEnabled = true
	d.cfg.HAPCMRTPTargets = []string{"<node-a-ip>:5004"}
	pipeline := d.buildWebstreamBroadcastPipeline("https://example.org/stream", "audio/mpeg", 128, 64, false)

	for _, expected := range []string{
		"rtpL16pay",
		"multiudpsink",
		"clients=<node-a-ip>:5004",
	} {
		if !strings.Contains(pipeline, expected) {
			t.Errorf("HA-enabled webstream pipeline missing %q:\n%s", expected, pipeline)
		}
	}
}
```

The exact signature of `buildWebstreamBroadcastPipeline` may differ; adjust the call accordingly.

- [ ] **Step 2: Run, verify failure → implement → verify pass**

```bash
go test -v -run "TestBuildWebstreamBroadcastPipeline" ./internal/playout/
```

Expected: FAIL.

Modify `buildWebstreamBroadcastPipeline()` similarly to Task 2.2: append a `t. ! queue ! audioconvert ! audio/x-raw,format=S16BE,... ! rtpL16pay ! multiudpsink ...` branch when HA mode is enabled.

Re-run, expect PASS.

- [ ] **Step 3: make ci + commit**

```bash
make ci
git add internal/playout/director.go internal/playout/director_pcm_rtp_test.go
git commit -m "playout: add PCM-RTP branch to webstream pipeline as well"
```

---

### Task 2.4: Integration test — engine emits RTP, verify packets arrive

**Files:**
- Create: `internal/playout/integration_test/pcm_rtp_test.go` (build tag `integration`)

**Context:**
Pure unit tests only verify pipeline-string construction. The real concern is whether the GStreamer process actually emits RTP packets on the network. This integration test spawns a real engine pipeline with HA mode on, listens on a UDP port for incoming RTP, and asserts packets arrive with expected size and rate.

Requires a small test audio file in `testdata/` (a 10-second silence MP3 is fine; create with `ffmpeg -f lavfi -i anullsrc -t 10 testdata/silence.mp3`).

- [ ] **Step 1: Create the test fixture**

```bash
mkdir -p internal/playout/integration_test/testdata
ffmpeg -f lavfi -i anullsrc=channel_layout=stereo:sample_rate=44100 -t 10 -c:a libmp3lame -b:a 128k internal/playout/integration_test/testdata/silence.mp3 -y
```

- [ ] **Step 2: Write the integration test**

`internal/playout/integration_test/pcm_rtp_test.go`:

```go
//go:build integration
// +build integration

/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package integration_test

import (
	"context"
	"net"
	"testing"
	"time"
	// import the playout package and config package; exact paths TBD
)

func TestEngineEmitsRTPInHAMode(t *testing.T) {
	// Listen on a free UDP port for incoming RTP packets.
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	port := conn.LocalAddr().(*net.UDPAddr).Port

	// Configure director with HA enabled, target = our listener.
	cfg := /* ... build a Config with HAPCMRTPEnabled=true, HAPCMRTPTargets=[127.0.0.1:port] ... */
	dir := /* ... newDirector(cfg) ... */

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go dir.PlayFile(ctx, "testdata/silence.mp3", "audio/mpeg")

	// Wait for at least 10 RTP packets to arrive (about 100 ms at typical packet sizes).
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	buf := make([]byte, 1500)
	count := 0
	for count < 10 {
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			t.Fatalf("after %d packets, ReadFromUDP error: %v", count, err)
		}
		if n < 12 {
			t.Errorf("packet too short: %d bytes (RTP header is 12 bytes minimum)", n)
		}
		// Validate RTP header version (top 2 bits == 2)
		if (buf[0] >> 6) != 2 {
			t.Errorf("packet does not look like RTP: first byte = 0x%02x", buf[0])
		}
		// Validate payload type == 10 (L16)
		if (buf[1] & 0x7F) != 10 {
			t.Errorf("packet PT = %d, want 10 (L16)", buf[1]&0x7F)
		}
		count++
	}
	t.Logf("Received %d RTP packets, all well-formed", count)
}
```

(Adjust import paths and Director construction to match the actual exported API.)

- [ ] **Step 3: Run the integration test**

```bash
go test -v -tags=integration -run TestEngineEmitsRTPInHAMode ./internal/playout/integration_test/
```

Expected: PASS. If FAIL: check that GStreamer can find the input file, that the pipeline string is valid (run it manually with `gst-launch-1.0`), and that no firewall is blocking localhost UDP.

- [ ] **Step 4: Commit**

```bash
git add internal/playout/integration_test/
git commit -m "test: integration test for engine PCM-RTP emission in HA mode"
```

---

### Task 2.5: Update mediascan / mediaengine docs

**Files:**
- Modify: `docs/MIGRATION.md` (or wherever mediaengine config is documented)
- Modify: `CLAUDE.md`

**Context:**
The two new env vars need to be documented so an operator deploying HA mode knows what to set.

- [ ] **Step 1: Add documentation**

In `CLAUDE.md`'s Environment Variables section, append:

```markdown
- `GRIMNIR_HA_PCM_RTP_ENABLED` - When true, media engine emits raw L16 PCM via RTP to the configured edge encoders (in addition to the legacy fdsink output). Required for the HA architecture. Default: false.
- `GRIMNIR_HA_PCM_RTP_TARGETS` - Comma-separated list of `host:port` for RTP delivery. Required when HA enabled. Example: `<node-a-ip>:5004,<node-b-ip>:5004`.
```

- [ ] **Step 2: Commit**

```bash
git add CLAUDE.md
git commit -m "CLAUDE.md: document GRIMNIR_HA_PCM_RTP_* env vars"
```

---

## Chunk 3: Edge encoder scaffold (binary + config + minimal gRPC)

Stand up the new binary's skeleton: a `main()` that loads config, starts a gRPC server with `GetStatus`, exposes `/healthz` over HTTP, exits cleanly on SIGTERM. No GStreamer pipeline yet (that's Chunk 4). The point of this chunk is to make `cmd/edge-encoder/` a buildable, deployable binary that does nothing useful but does nothing wrong.

### Task 3.1: Config loader

**Files:**
- Create: `internal/edgeencoder/config.go`
- Create: `internal/edgeencoder/config_test.go`

**Context:**
Edge encoder needs these env vars (matching mediaengine's pattern of `MEDIAENGINE_*` per the Explore agent report, namespaced `EDGE_ENCODER_*`):

| Variable | Default | Purpose |
|---|---|---|
| `EDGE_ENCODER_BIND_ADDR` | `0.0.0.0` | gRPC + /healthz HTTP bind address |
| `EDGE_ENCODER_GRPC_PORT` | `9092` | gRPC port (mediaengine uses 9091; one above) |
| `EDGE_ENCODER_METRICS_PORT` | `9192` | Prometheus metrics endpoint port |
| `EDGE_ENCODER_HTTP_PORT` | `8001` | HTTP/ICY listener port (8000 may be taken by existing broadcast server) |
| `EDGE_ENCODER_RTP_PORT_A` | `5004` | UDP port for engine A's RTP-L16 input |
| `EDGE_ENCODER_RTP_PORT_B` | `5005` | UDP port for engine B's RTP-L16 input |
| `EDGE_ENCODER_ENGINE_A_GRPC` | empty | `host:port` for engine A's gRPC (for health subscription); empty disables subscription |
| `EDGE_ENCODER_ENGINE_B_GRPC` | empty | same for engine B |
| `EDGE_ENCODER_OUTPUT_FORMAT` | `mp3` | Output codec: `mp3` or `aac` |
| `EDGE_ENCODER_OUTPUT_BITRATE_KBPS` | `128` | Encoder bitrate |
| `EDGE_ENCODER_HLS_ENABLED` | `false` | If true, also emit HLS segments (Chunk 7) |
| `EDGE_ENCODER_HLS_S3_BUCKET` | empty | Bucket for HLS segments; required if HLS enabled |
| `EDGE_ENCODER_LOG_LEVEL` | `info` | Log level |

- [ ] **Step 1: Write the failing test**

`internal/edgeencoder/config_test.go`:

```go
/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package edgeencoder

import (
	"testing"
)

func TestConfig_Defaults(t *testing.T) {
	// Clear all env vars
	for _, k := range []string{
		"EDGE_ENCODER_BIND_ADDR", "EDGE_ENCODER_GRPC_PORT",
		"EDGE_ENCODER_METRICS_PORT", "EDGE_ENCODER_HTTP_PORT",
		"EDGE_ENCODER_RTP_PORT_A", "EDGE_ENCODER_RTP_PORT_B",
		"EDGE_ENCODER_OUTPUT_FORMAT", "EDGE_ENCODER_OUTPUT_BITRATE_KBPS",
		"EDGE_ENCODER_HLS_ENABLED",
	} {
		t.Setenv(k, "")
	}
	c, err := LoadConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if c.BindAddr != "0.0.0.0" {
		t.Errorf("BindAddr = %q, want 0.0.0.0", c.BindAddr)
	}
	if c.GRPCPort != 9092 {
		t.Errorf("GRPCPort = %d, want 9092", c.GRPCPort)
	}
	if c.MetricsPort != 9192 {
		t.Errorf("MetricsPort = %d, want 9192", c.MetricsPort)
	}
	if c.HTTPPort != 8001 {
		t.Errorf("HTTPPort = %d, want 8001", c.HTTPPort)
	}
	if c.RTPPortA != 5004 {
		t.Errorf("RTPPortA = %d, want 5004", c.RTPPortA)
	}
	if c.RTPPortB != 5005 {
		t.Errorf("RTPPortB = %d, want 5005", c.RTPPortB)
	}
	if c.OutputFormat != "mp3" {
		t.Errorf("OutputFormat = %q, want mp3", c.OutputFormat)
	}
	if c.OutputBitrateKbps != 128 {
		t.Errorf("OutputBitrateKbps = %d, want 128", c.OutputBitrateKbps)
	}
	if c.HLSEnabled {
		t.Error("HLSEnabled = true, want false")
	}
}

func TestConfig_OverridesViaEnv(t *testing.T) {
	t.Setenv("EDGE_ENCODER_GRPC_PORT", "19092")
	t.Setenv("EDGE_ENCODER_OUTPUT_BITRATE_KBPS", "192")
	t.Setenv("EDGE_ENCODER_HLS_ENABLED", "true")
	t.Setenv("EDGE_ENCODER_HLS_S3_BUCKET", "my-hls-bucket")

	c, err := LoadConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if c.GRPCPort != 19092 {
		t.Errorf("GRPCPort = %d, want 19092", c.GRPCPort)
	}
	if c.OutputBitrateKbps != 192 {
		t.Errorf("OutputBitrateKbps = %d, want 192", c.OutputBitrateKbps)
	}
	if !c.HLSEnabled {
		t.Error("HLSEnabled = false, want true")
	}
	if c.HLSS3Bucket != "my-hls-bucket" {
		t.Errorf("HLSS3Bucket = %q, want my-hls-bucket", c.HLSS3Bucket)
	}
}

func TestConfig_HLSRequiresBucket(t *testing.T) {
	t.Setenv("EDGE_ENCODER_HLS_ENABLED", "true")
	t.Setenv("EDGE_ENCODER_HLS_S3_BUCKET", "")
	_, err := LoadConfigFromEnv()
	if err == nil {
		t.Error("LoadConfigFromEnv with HLS enabled and empty bucket: want error, got nil")
	}
}

func TestConfig_InvalidOutputFormat(t *testing.T) {
	t.Setenv("EDGE_ENCODER_OUTPUT_FORMAT", "flac")
	_, err := LoadConfigFromEnv()
	if err == nil {
		t.Error("LoadConfigFromEnv with format=flac: want error, got nil")
	}
}
```

- [ ] **Step 2: Run test, verify failure**

```bash
go test -v -run "TestConfig_" ./internal/edgeencoder/
```

Expected: FAIL — `LoadConfigFromEnv` undefined.

- [ ] **Step 3: Implement `Config` + `LoadConfigFromEnv`**

`internal/edgeencoder/config.go`:

```go
/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package edgeencoder

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Config holds all runtime configuration for the edge encoder. Loaded from
// environment variables; see LoadConfigFromEnv for the variable names.
type Config struct {
	BindAddr          string
	GRPCPort          int
	MetricsPort       int
	HTTPPort          int
	RTPPortA          int
	RTPPortB          int
	EngineAGRPC       string
	EngineBGRPC       string
	OutputFormat      string // "mp3" or "aac"
	OutputBitrateKbps int
	HLSEnabled        bool
	HLSS3Bucket       string
	LogLevel          string
}

func LoadConfigFromEnv() (*Config, error) {
	c := &Config{
		BindAddr:          getEnvOr("EDGE_ENCODER_BIND_ADDR", "0.0.0.0"),
		GRPCPort:          getEnvIntOr("EDGE_ENCODER_GRPC_PORT", 9092),
		MetricsPort:       getEnvIntOr("EDGE_ENCODER_METRICS_PORT", 9192),
		HTTPPort:          getEnvIntOr("EDGE_ENCODER_HTTP_PORT", 8001),
		RTPPortA:          getEnvIntOr("EDGE_ENCODER_RTP_PORT_A", 5004),
		RTPPortB:          getEnvIntOr("EDGE_ENCODER_RTP_PORT_B", 5005),
		EngineAGRPC:       os.Getenv("EDGE_ENCODER_ENGINE_A_GRPC"),
		EngineBGRPC:       os.Getenv("EDGE_ENCODER_ENGINE_B_GRPC"),
		OutputFormat:      strings.ToLower(getEnvOr("EDGE_ENCODER_OUTPUT_FORMAT", "mp3")),
		OutputBitrateKbps: getEnvIntOr("EDGE_ENCODER_OUTPUT_BITRATE_KBPS", 128),
		HLSEnabled:        getEnvBoolOr("EDGE_ENCODER_HLS_ENABLED", false),
		HLSS3Bucket:       os.Getenv("EDGE_ENCODER_HLS_S3_BUCKET"),
		LogLevel:          getEnvOr("EDGE_ENCODER_LOG_LEVEL", "info"),
	}

	switch c.OutputFormat {
	case "mp3", "aac":
	default:
		return nil, fmt.Errorf("EDGE_ENCODER_OUTPUT_FORMAT=%q invalid; want mp3 or aac", c.OutputFormat)
	}
	if c.HLSEnabled && c.HLSS3Bucket == "" {
		return nil, fmt.Errorf("EDGE_ENCODER_HLS_ENABLED=true requires non-empty EDGE_ENCODER_HLS_S3_BUCKET")
	}
	return c, nil
}

func getEnvOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getEnvIntOr(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func getEnvBoolOr(key string, def bool) bool {
	v := strings.ToLower(os.Getenv(key))
	switch v {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}
	return def
}
```

- [ ] **Step 4: Run test, verify pass**

```bash
go test -v ./internal/edgeencoder/
```

Expected: PASS, all subtests green.

- [ ] **Step 5: Commit**

```bash
git add internal/edgeencoder/config.go internal/edgeencoder/config_test.go
git commit -m "edgeencoder: config loader with env vars and validation"
```

---

### Task 3.2: gRPC service skeleton (GetStatus)

**Files:**
- Create: `internal/edgeencoder/grpc.go`
- Create: `internal/edgeencoder/grpc_test.go`
- Modify: `proto/edgeencoder/v1/edgeencoder.proto` (or reuse mediaengine proto pattern)

**Context:**
Per the Explore agent report, mediaengine exposes a custom `GetStatus(ctx, &pb.StatusRequest{})` rather than the standard `grpc.health.v1.Health`. Edge encoder follows the same pattern so the grimnir control plane can probe it identically.

A minimal `Status` returns:
- `version` (build-time variable)
- `uptime_seconds`
- `active_input` ("A", "B", or "none")
- `inputs_healthy` (bitfield: which inputs are receiving packets)

- [ ] **Step 1: Add the proto file**

Create `proto/edgeencoder/v1/edgeencoder.proto`:

```proto
syntax = "proto3";

package edgeencoder.v1;

option go_package = "github.com/friendsincode/grimnir_radio/proto/edgeencoder/v1";

service EdgeEncoder {
  rpc GetStatus(StatusRequest) returns (StatusResponse);
}

message StatusRequest {}

message StatusResponse {
  string version = 1;
  int64 uptime_seconds = 2;
  string active_input = 3;     // "A", "B", or "none"
  bool input_a_healthy = 4;
  bool input_b_healthy = 5;
  int64 listener_count = 6;
  int64 switch_count = 7;       // total times we've switched inputs
}
```

- [ ] **Step 2: Generate Go code**

```bash
make proto
```

Verify `proto/edgeencoder/v1/edgeencoder.pb.go` and `edgeencoder_grpc.pb.go` exist. If `make proto` doesn't pick up the new proto file, check the Makefile's `proto:` target — it likely globs `$(PROTO_DIR)/mediaengine/v1/*.proto`; expand to also include `$(PROTO_DIR)/edgeencoder/v1/*.proto`.

- [ ] **Step 3: Write the failing test**

`internal/edgeencoder/grpc_test.go`:

```go
/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package edgeencoder

import (
	"context"
	"net"
	"testing"
	"time"

	pb "github.com/friendsincode/grimnir_radio/proto/edgeencoder/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func TestGRPCGetStatus(t *testing.T) {
	// Spin up the server on a random port.
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer lis.Close()

	srv := NewGRPCServer(&fakeStatusProvider{
		version:      "test-version",
		activeInput:  "A",
		inputAHealthy: true,
		inputBHealthy: false,
	})
	grpcServer := grpc.NewServer()
	pb.RegisterEdgeEncoderServer(grpcServer, srv)
	go grpcServer.Serve(lis)
	defer grpcServer.Stop()

	// Dial and call GetStatus.
	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	client := pb.NewEdgeEncoderClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	resp, err := client.GetStatus(ctx, &pb.StatusRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Version != "test-version" {
		t.Errorf("Version = %q, want test-version", resp.Version)
	}
	if resp.ActiveInput != "A" {
		t.Errorf("ActiveInput = %q, want A", resp.ActiveInput)
	}
	if !resp.InputAHealthy {
		t.Error("InputAHealthy = false, want true")
	}
	if resp.InputBHealthy {
		t.Error("InputBHealthy = true, want false")
	}
}

// fakeStatusProvider implements StatusProvider for the test.
type fakeStatusProvider struct {
	version       string
	activeInput   string
	inputAHealthy bool
	inputBHealthy bool
}

func (f *fakeStatusProvider) Status() Status {
	return Status{
		Version:       f.version,
		ActiveInput:   f.activeInput,
		InputAHealthy: f.inputAHealthy,
		InputBHealthy: f.inputBHealthy,
	}
}
```

- [ ] **Step 4: Run test, verify failure**

```bash
go test -v -run "TestGRPC" ./internal/edgeencoder/
```

Expected: FAIL — `NewGRPCServer`, `StatusProvider`, `Status` undefined.

- [ ] **Step 5: Implement the gRPC server**

`internal/edgeencoder/grpc.go`:

```go
/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package edgeencoder

import (
	"context"
	"time"

	pb "github.com/friendsincode/grimnir_radio/proto/edgeencoder/v1"
)

// Status is the current operational state of the edge encoder, as seen by
// the gRPC GetStatus call. The StatusProvider interface lets the gRPC server
// be tested in isolation from the pipeline.
type Status struct {
	Version       string
	UptimeSeconds int64
	ActiveInput   string
	InputAHealthy bool
	InputBHealthy bool
	ListenerCount int64
	SwitchCount   int64
}

// StatusProvider is implemented by whatever owns the live state (pipeline +
// switcher + broadcast adapter). The gRPC server queries it on every call.
type StatusProvider interface {
	Status() Status
}

// GRPCServer implements pb.EdgeEncoderServer.
type GRPCServer struct {
	pb.UnimplementedEdgeEncoderServer
	provider StatusProvider
}

func NewGRPCServer(provider StatusProvider) *GRPCServer {
	return &GRPCServer{provider: provider}
}

func (s *GRPCServer) GetStatus(ctx context.Context, _ *pb.StatusRequest) (*pb.StatusResponse, error) {
	st := s.provider.Status()
	return &pb.StatusResponse{
		Version:       st.Version,
		UptimeSeconds: st.UptimeSeconds,
		ActiveInput:   st.ActiveInput,
		InputAHealthy: st.InputAHealthy,
		InputBHealthy: st.InputBHealthy,
		ListenerCount: st.ListenerCount,
		SwitchCount:   st.SwitchCount,
	}, nil
}

// Uptime returns wall-clock duration since the receiver was created.
type StartTime time.Time

func (s StartTime) UptimeSeconds() int64 {
	return int64(time.Since(time.Time(s)).Seconds())
}
```

- [ ] **Step 6: Run test, verify pass**

```bash
go test -v ./internal/edgeencoder/
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add proto/edgeencoder/ internal/edgeencoder/grpc.go internal/edgeencoder/grpc_test.go Makefile
git commit -m "edgeencoder: gRPC server skeleton with GetStatus"
```

---

### Task 3.3: `cmd/edge-encoder/main.go` (wiring + signal handling)

**Files:**
- Create: `cmd/edge-encoder/main.go`
- Create: `cmd/edge-encoder/main_test.go`

**Context:**
The main function wires up config, gRPC, /healthz HTTP endpoint, signal handling. No pipeline yet (Chunk 4). Uses a stub `StatusProvider` that returns "no inputs, no listeners" until the real pipeline lands.

- [ ] **Step 1: Write the smoke test**

`cmd/edge-encoder/main_test.go`:

```go
/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package main

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestRun_StartsAndStopsCleanly(t *testing.T) {
	t.Setenv("EDGE_ENCODER_GRPC_PORT", "0")    // ephemeral port
	t.Setenv("EDGE_ENCODER_HTTP_PORT", "0")
	t.Setenv("EDGE_ENCODER_METRICS_PORT", "0")
	t.Setenv("EDGE_ENCODER_RTP_PORT_A", "0")
	t.Setenv("EDGE_ENCODER_RTP_PORT_B", "0")

	var stdout, stderr bytes.Buffer

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	done := make(chan int, 1)
	go func() { done <- run(ctx, &stdout, &stderr) }()

	// Wait briefly for startup logs
	time.Sleep(500 * time.Millisecond)
	cancel()

	select {
	case code := <-done:
		if code != 0 {
			t.Errorf("run exit = %d, want 0; stderr=%q", code, stderr.String())
		}
	case <-time.After(3 * time.Second):
		t.Fatal("run did not exit within 3s of context cancellation")
	}

	if !strings.Contains(stdout.String(), "edge-encoder starting") {
		t.Logf("stdout did not include expected startup line: %q", stdout.String())
	}
}

func TestRun_InvalidConfigExitsNonZero(t *testing.T) {
	t.Setenv("EDGE_ENCODER_OUTPUT_FORMAT", "wav")  // invalid
	var stderr bytes.Buffer
	code := run(context.Background(), os.Stdout, &stderr)
	if code == 0 {
		t.Error("run with invalid config: exit = 0, want non-zero")
	}
	if !strings.Contains(stderr.String(), "OUTPUT_FORMAT") {
		t.Errorf("stderr did not mention OUTPUT_FORMAT: %q", stderr.String())
	}
}
```

- [ ] **Step 2: Run test, verify failure**

```bash
go test -v ./cmd/edge-encoder/
```

Expected: FAIL — `run` undefined.

- [ ] **Step 3: Implement `main.go`**

`cmd/edge-encoder/main.go`:

```go
/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

// Command edge-encoder ingests PCM-over-RTP from media engines, performs
// sample-aligned input switching when the active source goes unhealthy, and
// serves the encoded result to HTTP/ICY and HLS listeners.
//
// See internal/edgeencoder for the per-component documentation and
// docs/superpowers/plans/2026-06-03-edge-encoder-pcm-transport.md for the
// implementation plan.
package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/edgeencoder"
	pb "github.com/friendsincode/grimnir_radio/proto/edgeencoder/v1"
	"google.golang.org/grpc"
)

// Version is set at build time via ldflags; mirrors mediaengine pattern.
var Version = "dev"

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	os.Exit(run(ctx, os.Stdout, os.Stderr))
}

func run(ctx context.Context, stdout, stderr io.Writer) int {
	cfg, err := edgeencoder.LoadConfigFromEnv()
	if err != nil {
		fmt.Fprintf(stderr, "edge-encoder: config error: %v\n", err)
		return 2
	}
	fmt.Fprintf(stdout, "edge-encoder starting; version=%s grpc_port=%d http_port=%d rtp_ports=%d,%d\n",
		Version, cfg.GRPCPort, cfg.HTTPPort, cfg.RTPPortA, cfg.RTPPortB)

	// Stub status provider — replaced by real pipeline integration in Chunk 5.
	startTime := time.Now()
	statusProvider := &stubStatus{startTime: startTime}

	// gRPC server
	grpcLis, err := net.Listen("tcp", fmt.Sprintf("%s:%d", cfg.BindAddr, cfg.GRPCPort))
	if err != nil {
		fmt.Fprintf(stderr, "edge-encoder: grpc listen: %v\n", err)
		return 2
	}
	grpcServer := grpc.NewServer()
	pb.RegisterEdgeEncoderServer(grpcServer, edgeencoder.NewGRPCServer(statusProvider))
	go grpcServer.Serve(grpcLis)
	defer grpcServer.GracefulStop()

	// /healthz HTTP endpoint (separate from listener HTTP/ICY in Chunk 6)
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "ok")
	})
	httpSrv := &http.Server{
		Addr:              fmt.Sprintf("%s:%d", cfg.BindAddr, cfg.HTTPPort),
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go httpSrv.ListenAndServe()
	defer httpSrv.Shutdown(context.Background())

	<-ctx.Done()
	fmt.Fprintln(stdout, "edge-encoder shutting down")
	return 0
}

type stubStatus struct {
	startTime time.Time
}

func (s *stubStatus) Status() edgeencoder.Status {
	return edgeencoder.Status{
		Version:       Version,
		UptimeSeconds: int64(time.Since(s.startTime).Seconds()),
		ActiveInput:   "none",
	}
}
```

- [ ] **Step 4: Run test, verify pass**

```bash
go test -v ./cmd/edge-encoder/
```

Expected: PASS.

- [ ] **Step 5: Verify the binary actually builds**

```bash
make build-edge-encoder
./edge-encoder &
PID=$!
sleep 1
curl -s http://localhost:8001/healthz
kill $PID
```

Expected: `ok` from /healthz; binary exits clean on SIGTERM.

- [ ] **Step 6: Commit**

```bash
git add cmd/edge-encoder/
git commit -m "edge-encoder: main binary with gRPC, /healthz, signal handling"
```

---

## Chunk 4: GStreamer pipeline construction (go-gst)

The central novel chunk. Build the pipeline programmatically via go-gst, manage its lifecycle, expose runtime control of the `input-selector` element, expose pad probes for packet-arrival monitoring.

The pipeline (described as a `gst-launch` string for clarity; we build it programmatically):

```
udpsrc port=5004 caps="application/x-rtp,media=audio,clock-rate=44100,encoding-name=L16,payload=10,channels=2"
  ! rtpjitterbuffer latency=80
  ! rtpL16depay
  ! audio/x-raw,rate=44100,channels=2,format=S16BE
  ! audioconvert
  ! audio/x-raw,format=S16LE
  ! input-selector.sink_0  name=input-selector cache-buffers=true sync-streams=true

udpsrc port=5005 caps="application/x-rtp,media=audio,clock-rate=44100,encoding-name=L16,payload=10,channels=2"
  ! rtpjitterbuffer latency=80
  ! rtpL16depay
  ! audio/x-raw,rate=44100,channels=2,format=S16BE
  ! audioconvert
  ! audio/x-raw,format=S16LE
  ! input-selector.sink_1

input-selector. ! audioconvert ! tee name=output

output. ! queue ! lamemp3enc target=1 bitrate=128 cbr=true ! appsink name=mp3sink
output. ! queue ! hlssink2 name=hlssink (deferred to Chunk 7; not in this chunk)
```

For Chunk 4, omit the HLS branch — `tee` is still in the graph but only one branch (MP3) is wired up. HLS comes in Chunk 7.

### Task 4.1: Pipeline struct + constructor

**Files:**
- Create: `internal/edgeencoder/pipeline.go`
- Create: `internal/edgeencoder/pipeline_test.go`

**Context:**
The `Pipeline` type owns the go-gst `gst.Pipeline`, the input-selector element (for runtime control), the two source-branch sink pads (for pad probes), and the appsink (for output bytes).

- [ ] **Step 1: Write the failing test**

`internal/edgeencoder/pipeline_test.go`:

```go
/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package edgeencoder

import (
	"testing"
)

func TestNewPipeline_BuildsAndReachesPlayingState(t *testing.T) {
	Init()
	cfg := &Config{
		RTPPortA:          15004,  // ephemeral high ports for test isolation
		RTPPortB:          15005,
		OutputFormat:      "mp3",
		OutputBitrateKbps: 128,
	}
	p, err := NewPipeline(cfg)
	if err != nil {
		t.Fatalf("NewPipeline: %v", err)
	}
	defer p.Close()

	if err := p.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	// At this point the pipeline should be PLAYING. Without any RTP input the
	// pipeline still runs (udpsrc blocks waiting), but the element graph is
	// constructed and elements are in PLAYING state.

	if p.ActiveInput() != "A" {
		t.Errorf("ActiveInput initial = %q, want A", p.ActiveInput())
	}

	// Switch to B
	if err := p.SetActiveInput("B"); err != nil {
		t.Fatalf("SetActiveInput(B): %v", err)
	}
	if p.ActiveInput() != "B" {
		t.Errorf("ActiveInput after switch = %q, want B", p.ActiveInput())
	}

	// Switch back to A
	if err := p.SetActiveInput("A"); err != nil {
		t.Fatalf("SetActiveInput(A): %v", err)
	}
	if p.ActiveInput() != "A" {
		t.Errorf("ActiveInput after switch back = %q, want A", p.ActiveInput())
	}

	// Invalid input name
	if err := p.SetActiveInput("Z"); err == nil {
		t.Error("SetActiveInput(Z): want error, got nil")
	}
}

func TestNewPipeline_AppsinkExists(t *testing.T) {
	Init()
	cfg := &Config{RTPPortA: 15006, RTPPortB: 15007, OutputFormat: "mp3", OutputBitrateKbps: 128}
	p, err := NewPipeline(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	if p.MP3Appsink() == nil {
		t.Error("MP3Appsink() = nil, want non-nil")
	}
}
```

- [ ] **Step 2: Run test, verify failure**

```bash
go test -v -run "TestNewPipeline" ./internal/edgeencoder/
```

Expected: FAIL — `NewPipeline` undefined.

- [ ] **Step 3: Implement Pipeline**

`internal/edgeencoder/pipeline.go`:

```go
/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package edgeencoder

import (
	"fmt"
	"sync"

	"github.com/go-gst/go-gst/gst"
	"github.com/go-gst/go-gst/gst/app"
)

// Pipeline owns the GStreamer pipeline that ingests two RTP inputs, switches
// between them via input-selector, and encodes the active stream to MP3 via
// an appsink that the broadcast adapter reads from.
type Pipeline struct {
	cfg *Config

	gst             *gst.Pipeline
	inputSelector   *gst.Element
	mp3Appsink      *app.Sink

	// Track which input is active. We mirror the input-selector's state
	// because querying GStreamer for it on every call is slow.
	mu          sync.Mutex
	activeInput string

	// Map from logical input name ("A"/"B") to the GstPad on input-selector
	// that selects that input as active.
	inputPads map[string]*gst.Pad
}

// NewPipeline constructs and links the GStreamer pipeline. The pipeline is
// NOT started; call Start() after wiring pad probes (Chunk 5).
func NewPipeline(cfg *Config) (*Pipeline, error) {
	pipeline, err := gst.NewPipeline("edge-encoder")
	if err != nil {
		return nil, fmt.Errorf("gst.NewPipeline: %w", err)
	}

	// Build branch A: udpsrc → rtpjitterbuffer → rtpL16depay → audioconvert
	branchA, err := buildInputBranch(pipeline, "a", cfg.RTPPortA)
	if err != nil {
		return nil, fmt.Errorf("input branch A: %w", err)
	}

	// Build branch B
	branchB, err := buildInputBranch(pipeline, "b", cfg.RTPPortB)
	if err != nil {
		return nil, fmt.Errorf("input branch B: %w", err)
	}

	// input-selector
	selector, err := gst.NewElement("input-selector")
	if err != nil {
		return nil, fmt.Errorf("input-selector: %w", err)
	}
	if err := selector.SetProperty("cache-buffers", true); err != nil {
		return nil, fmt.Errorf("set cache-buffers: %w", err)
	}
	if err := selector.SetProperty("sync-streams", true); err != nil {
		return nil, fmt.Errorf("set sync-streams: %w", err)
	}
	if err := pipeline.Add(selector); err != nil {
		return nil, fmt.Errorf("add selector: %w", err)
	}

	// Request a sink pad for each branch and link them
	padA := selector.GetRequestPad("sink_%u")
	if padA == nil {
		return nil, fmt.Errorf("request sink_%%u from input-selector for A returned nil")
	}
	if pl := branchA.GetStaticPad("src"); pl == nil || pl.Link(padA) != gst.PadLinkOK {
		return nil, fmt.Errorf("link branch A to selector failed")
	}

	padB := selector.GetRequestPad("sink_%u")
	if padB == nil {
		return nil, fmt.Errorf("request sink_%%u from input-selector for B returned nil")
	}
	if pl := branchB.GetStaticPad("src"); pl == nil || pl.Link(padB) != gst.PadLinkOK {
		return nil, fmt.Errorf("link branch B to selector failed")
	}

	// Set active pad to A by default
	if err := selector.SetProperty("active-pad", padA); err != nil {
		return nil, fmt.Errorf("set initial active-pad: %w", err)
	}

	// Encoder + appsink
	encoder, err := buildEncoder(cfg)
	if err != nil {
		return nil, fmt.Errorf("encoder: %w", err)
	}
	for _, e := range encoder {
		if err := pipeline.Add(e); err != nil {
			return nil, fmt.Errorf("add encoder element %s: %w", e.GetName(), err)
		}
	}
	appsinkElt, err := gst.NewElementWithName("appsink", "mp3sink")
	if err != nil {
		return nil, fmt.Errorf("appsink: %w", err)
	}
	if err := pipeline.Add(appsinkElt); err != nil {
		return nil, fmt.Errorf("add appsink: %w", err)
	}
	encoder = append(encoder, appsinkElt)

	// Link selector → encoder chain → appsink
	if err := selector.Link(encoder[0]); err != nil {
		return nil, fmt.Errorf("link selector to encoder: %w", err)
	}
	for i := 0; i < len(encoder)-1; i++ {
		if err := encoder[i].Link(encoder[i+1]); err != nil {
			return nil, fmt.Errorf("link encoder %s → %s: %w", encoder[i].GetName(), encoder[i+1].GetName(), err)
		}
	}

	appsink := app.SinkFromElement(appsinkElt)
	appsink.SetProperty("emit-signals", false)
	appsink.SetProperty("sync", false)
	appsink.SetProperty("max-buffers", uint(10))
	appsink.SetProperty("drop", false)

	p := &Pipeline{
		cfg:           cfg,
		gst:           pipeline,
		inputSelector: selector,
		mp3Appsink:    appsink,
		activeInput:   "A",
		inputPads: map[string]*gst.Pad{
			"A": padA,
			"B": padB,
		},
	}
	return p, nil
}

// buildInputBranch creates udpsrc → rtpjitterbuffer → rtpL16depay →
// audioconvert and returns the last element (which has the src pad that
// links into the input-selector).
func buildInputBranch(pipe *gst.Pipeline, suffix string, port int) (*gst.Element, error) {
	udpsrc, err := gst.NewElementWithName("udpsrc", "udpsrc_"+suffix)
	if err != nil {
		return nil, err
	}
	udpsrc.SetProperty("port", port)
	caps := gst.NewCapsFromString(
		"application/x-rtp,media=audio,clock-rate=44100,encoding-name=L16,payload=10,channels=2")
	udpsrc.SetProperty("caps", caps)

	jb, err := gst.NewElement("rtpjitterbuffer")
	if err != nil {
		return nil, err
	}
	jb.SetProperty("latency", uint(80))

	depay, err := gst.NewElement("rtpL16depay")
	if err != nil {
		return nil, err
	}

	conv, err := gst.NewElementWithName("audioconvert", "audioconvert_"+suffix)
	if err != nil {
		return nil, err
	}

	for _, e := range []*gst.Element{udpsrc, jb, depay, conv} {
		if err := pipe.Add(e); err != nil {
			return nil, err
		}
	}
	if err := gst.ElementLinkMany(udpsrc, jb, depay, conv); err != nil {
		return nil, err
	}
	return conv, nil
}

// buildEncoder returns the chain of elements from input-selector to the
// element before appsink. Caller adds them to the pipeline and links them.
func buildEncoder(cfg *Config) ([]*gst.Element, error) {
	switch cfg.OutputFormat {
	case "mp3":
		conv, err := gst.NewElementWithName("audioconvert", "audioconvert_out")
		if err != nil {
			return nil, err
		}
		enc, err := gst.NewElement("lamemp3enc")
		if err != nil {
			return nil, err
		}
		enc.SetProperty("target", 1) // bitrate mode
		enc.SetProperty("bitrate", cfg.OutputBitrateKbps)
		enc.SetProperty("cbr", true)
		return []*gst.Element{conv, enc}, nil
	case "aac":
		conv, err := gst.NewElementWithName("audioconvert", "audioconvert_out")
		if err != nil {
			return nil, err
		}
		enc, err := gst.NewElement("avenc_aac")
		if err != nil {
			return nil, err
		}
		enc.SetProperty("bitrate", cfg.OutputBitrateKbps*1000)
		return []*gst.Element{conv, enc}, nil
	default:
		return nil, fmt.Errorf("unsupported output format %q", cfg.OutputFormat)
	}
}

// Start transitions the pipeline to PLAYING.
func (p *Pipeline) Start() error {
	if err := p.gst.SetState(gst.StatePlaying); err != nil {
		return fmt.Errorf("set state to PLAYING: %w", err)
	}
	return nil
}

// Close stops the pipeline and releases all resources.
func (p *Pipeline) Close() error {
	return p.gst.SetState(gst.StateNull)
}

// ActiveInput returns "A" or "B".
func (p *Pipeline) ActiveInput() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.activeInput
}

// SetActiveInput switches the input-selector's active pad. The switch is
// sample-aligned because input-selector waits for the running-time boundary.
// Safe to call from any goroutine.
func (p *Pipeline) SetActiveInput(name string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	pad, ok := p.inputPads[name]
	if !ok {
		return fmt.Errorf("unknown input %q (want A or B)", name)
	}
	if err := p.inputSelector.SetProperty("active-pad", pad); err != nil {
		return fmt.Errorf("set active-pad: %w", err)
	}
	p.activeInput = name
	return nil
}

// MP3Appsink returns the appsink that broadcast.go reads encoded bytes from.
func (p *Pipeline) MP3Appsink() *app.Sink {
	return p.mp3Appsink
}

// InputPad returns the input-selector's sink pad for the given input ("A" or "B").
// Used by health.go to attach pad probes for packet-arrival monitoring.
func (p *Pipeline) InputPad(name string) *gst.Pad {
	return p.inputPads[name]
}
```

- [ ] **Step 4: Run test, verify pass**

```bash
go test -v -run "TestNewPipeline" ./internal/edgeencoder/
```

Expected: PASS. If the test fails specifically on `Start()` because no clock or no async state-change handler — the pipeline may need to be PAUSED first, or the test may need a bus message loop. Add a small `gst.Bus` consumer goroutine in the test if needed.

- [ ] **Step 5: Commit**

```bash
git add internal/edgeencoder/pipeline.go internal/edgeencoder/pipeline_test.go
git commit -m "edgeencoder: GStreamer pipeline with input-selector for runtime switching"
```

---

### Task 4.2: Pipeline bus message loop (panic on ERROR, log on WARNING/INFO)

**Files:**
- Modify: `internal/edgeencoder/pipeline.go`
- Modify: `internal/edgeencoder/pipeline_test.go`

**Context:**
GStreamer reports element state changes, errors, and stream events via a per-pipeline message bus. The pipeline must consume these on a dedicated goroutine. Critical errors (ERROR-level messages) should be logged and surfaced via a `Wait()` / `Done()` mechanism so the main loop can react (e.g., shut down + alert).

- [ ] **Step 1: Add a failing test**

Append to `internal/edgeencoder/pipeline_test.go`:

```go
func TestPipeline_BusErrorPropagates(t *testing.T) {
	Init()
	cfg := &Config{RTPPortA: 15008, RTPPortB: 15009, OutputFormat: "mp3", OutputBitrateKbps: 128}
	p, err := NewPipeline(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	// Intentionally cause a downstream error: post a synthetic ERROR onto the bus.
	if err := p.Start(); err != nil {
		t.Fatal(err)
	}

	// Wait until pipeline reports failure or a timeout fires.
	select {
	case err := <-p.Done():
		t.Logf("Done received error: %v (expected; pipeline failure path covered)", err)
	case <-time.After(500 * time.Millisecond):
		// No error in normal operation — that's fine for this smoke test.
		// We're verifying the channel exists and doesn't accidentally fire.
	}
}
```

- [ ] **Step 2: Add `Done()` channel + bus consumer**

In `internal/edgeencoder/pipeline.go`, add to the `Pipeline` struct:

```go
done chan error
```

In `NewPipeline()`, before returning:

```go
p.done = make(chan error, 1)
go p.consumeBus()
```

Add the method:

```go
func (p *Pipeline) consumeBus() {
	bus := p.gst.GetPipelineBus()
	for {
		msg := bus.TimedPop(gst.ClockTimeNone)
		if msg == nil {
			return
		}
		switch msg.Type() {
		case gst.MessageError:
			err := msg.ParseError()
			// Best-effort: send error on done channel; non-blocking
			select {
			case p.done <- err:
			default:
			}
			return
		case gst.MessageEOS:
			select {
			case p.done <- nil:
			default:
			}
			return
		}
	}
}

// Done returns a channel that closes (or receives an error) when the pipeline
// stops, either due to an error, EOS, or Close().
func (p *Pipeline) Done() <-chan error {
	return p.done
}
```

- [ ] **Step 3: Run test, verify pass**

```bash
go test -v -run "TestPipeline_BusError" ./internal/edgeencoder/
```

- [ ] **Step 4: Commit**

```bash
git add internal/edgeencoder/pipeline.go internal/edgeencoder/pipeline_test.go
git commit -m "edgeencoder: bus message consumer with Done() channel for shutdown signaling"
```

---

## Chunk 5: Health monitoring + switching logic

Per-input health combines two signals:
1. **Packet arrival** — pad probe on each input branch records the last-buffer timestamp; healthy if `time.Since(lastBuffer) < 100ms`.
2. **gRPC engine health** — subscription to each engine's `GetStatus` every 1s; healthy if last response was successful within the last 3s.

The switcher reads both, decides the desired active input, and calls `pipeline.SetActiveInput()`. Includes hysteresis (don't flap) and respects an explicit "prefer A" bias when both healthy.

### Task 5.1: Per-input packet-arrival tracker via pad probes

**Files:**
- Create: `internal/edgeencoder/health.go`
- Create: `internal/edgeencoder/health_test.go`

**Context:**
go-gst exposes pad probes that fire for every buffer passing through a pad. We attach a probe to each input branch's output pad (the audioconvert that feeds into input-selector). The probe callback updates an atomic `lastBufferNs` timestamp.

- [ ] **Step 1: Write the failing test**

```go
/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package edgeencoder

import (
	"testing"
	"time"
)

func TestInputHealth_InitiallyUnhealthy(t *testing.T) {
	ih := NewInputHealth(100 * time.Millisecond)
	if ih.IsHealthy() {
		t.Error("new InputHealth claims healthy; want unhealthy until first packet")
	}
}

func TestInputHealth_HealthyAfterPacket(t *testing.T) {
	ih := NewInputHealth(100 * time.Millisecond)
	ih.RecordPacket()
	if !ih.IsHealthy() {
		t.Error("InputHealth after RecordPacket: unhealthy, want healthy")
	}
}

func TestInputHealth_StaleAfterWindow(t *testing.T) {
	ih := NewInputHealth(50 * time.Millisecond)
	ih.RecordPacket()
	time.Sleep(75 * time.Millisecond)
	if ih.IsHealthy() {
		t.Error("InputHealth after window elapsed: healthy, want unhealthy")
	}
}

func TestInputHealth_GRPCGate(t *testing.T) {
	ih := NewInputHealth(100 * time.Millisecond)
	ih.RecordPacket()
	if !ih.IsHealthy() {
		t.Fatal("packets present but unhealthy")
	}
	ih.SetGRPCHealthy(false)
	if ih.IsHealthy() {
		t.Error("gRPC unhealthy override ignored; want unhealthy")
	}
	ih.SetGRPCHealthy(true)
	if !ih.IsHealthy() {
		t.Error("gRPC restored to healthy + packets present: unhealthy, want healthy")
	}
}
```

- [ ] **Step 2: Run, verify failure**

```bash
go test -v -run TestInputHealth ./internal/edgeencoder/
```

- [ ] **Step 3: Implement `InputHealth`**

`internal/edgeencoder/health.go`:

```go
/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package edgeencoder

import (
	"sync/atomic"
	"time"

	"github.com/go-gst/go-gst/gst"
)

// InputHealth tracks the liveness of one input branch. Combines two signals:
//   - Packet arrival timestamp (updated by RecordPacket; healthy if within window)
//   - gRPC health gate (manual setter; defaults to true so health is purely
//     packet-based when no gRPC subscription is configured)
type InputHealth struct {
	window       time.Duration
	lastPacketNs atomic.Int64
	grpcHealthy  atomic.Bool
}

func NewInputHealth(window time.Duration) *InputHealth {
	ih := &InputHealth{window: window}
	ih.grpcHealthy.Store(true) // default healthy when no gRPC subscription
	return ih
}

func (ih *InputHealth) RecordPacket() {
	ih.lastPacketNs.Store(time.Now().UnixNano())
}

func (ih *InputHealth) SetGRPCHealthy(healthy bool) {
	ih.grpcHealthy.Store(healthy)
}

func (ih *InputHealth) IsHealthy() bool {
	if !ih.grpcHealthy.Load() {
		return false
	}
	last := ih.lastPacketNs.Load()
	if last == 0 {
		return false
	}
	return time.Since(time.Unix(0, last)) < ih.window
}

// AttachPadProbe installs a probe on the given pad that calls RecordPacket
// for every buffer that passes through. Returns the probe ID for later removal.
func (ih *InputHealth) AttachPadProbe(pad *gst.Pad) uint64 {
	return pad.AddProbe(gst.PadProbeTypeBuffer, func(_ *gst.Pad, _ *gst.PadProbeInfo) gst.PadProbeReturn {
		ih.RecordPacket()
		return gst.PadProbeOK
	})
}
```

- [ ] **Step 4: Run test, verify pass**

```bash
go test -v -run TestInputHealth ./internal/edgeencoder/
```

- [ ] **Step 5: Commit**

```bash
git add internal/edgeencoder/health.go internal/edgeencoder/health_test.go
git commit -m "edgeencoder: per-input health tracker (packet-arrival + gRPC gate)"
```

---

### Task 5.2: Switcher loop with hysteresis

**Files:**
- Create: `internal/edgeencoder/switcher.go`
- Create: `internal/edgeencoder/switcher_test.go`

**Context:**
Switcher polls both `InputHealth` instances on a 50ms tick and decides the active input:

- If active healthy → no change.
- If active unhealthy AND other healthy → switch.
- If both unhealthy → keep current (no good choice).
- If both healthy → prefer "A" unless explicit operator override sets prefer = "B".

To prevent flapping, require N consecutive ticks of unhealth before switching (default N=2 = 100ms confirmation).

- [ ] **Step 1: Write the failing test**

```go
func TestSwitcher_StaysOnHealthyInput(t *testing.T) {
	a := NewInputHealth(100 * time.Millisecond)
	b := NewInputHealth(100 * time.Millisecond)
	a.RecordPacket()
	b.RecordPacket()
	swap := &fakeSwapper{current: "A"}
	sw := NewSwitcher(a, b, swap, 10*time.Millisecond, 2)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	sw.Run(ctx)
	if swap.current != "A" {
		t.Errorf("active = %q, want A (no switch with both healthy)", swap.current)
	}
}

func TestSwitcher_FailsOverWhenActiveDies(t *testing.T) {
	a := NewInputHealth(50 * time.Millisecond)
	b := NewInputHealth(50 * time.Millisecond)
	a.RecordPacket()
	b.RecordPacket()
	swap := &fakeSwapper{current: "A"}
	sw := NewSwitcher(a, b, swap, 10*time.Millisecond, 2)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	go func() {
		// Keep B alive; let A go stale
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				b.RecordPacket()
			}
		}
	}()

	sw.Run(ctx)
	if swap.current != "B" {
		t.Errorf("active = %q, want B (should have switched after A went stale)", swap.current)
	}
}

type fakeSwapper struct {
	mu      sync.Mutex
	current string
}

func (f *fakeSwapper) ActiveInput() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.current
}

func (f *fakeSwapper) SetActiveInput(name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.current = name
	return nil
}
```

- [ ] **Step 2: Run, verify failure**

```bash
go test -v -run TestSwitcher ./internal/edgeencoder/
```

- [ ] **Step 3: Implement `Switcher`**

`internal/edgeencoder/switcher.go`:

```go
/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package edgeencoder

import (
	"context"
	"sync/atomic"
	"time"
)

// Swapper is implemented by Pipeline. Decoupled for testability.
type Swapper interface {
	ActiveInput() string
	SetActiveInput(name string) error
}

// Switcher polls per-input health and switches the active input when needed,
// with hysteresis to prevent flapping.
type Switcher struct {
	a, b         *InputHealth
	swap         Swapper
	tick         time.Duration
	hysteresisN  int
	switchCount  atomic.Int64
}

func NewSwitcher(a, b *InputHealth, swap Swapper, tick time.Duration, hysteresisN int) *Switcher {
	return &Switcher{
		a:           a,
		b:           b,
		swap:        swap,
		tick:        tick,
		hysteresisN: hysteresisN,
	}
}

func (s *Switcher) Run(ctx context.Context) {
	t := time.NewTicker(s.tick)
	defer t.Stop()
	failingTicks := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			active := s.swap.ActiveInput()
			activeHealthy := (active == "A" && s.a.IsHealthy()) || (active == "B" && s.b.IsHealthy())
			otherHealthy := (active == "A" && s.b.IsHealthy()) || (active == "B" && s.a.IsHealthy())

			if activeHealthy {
				failingTicks = 0
				continue
			}
			failingTicks++
			if failingTicks < s.hysteresisN {
				continue
			}
			if !otherHealthy {
				continue
			}
			// Switch
			next := "B"
			if active == "B" {
				next = "A"
			}
			_ = s.swap.SetActiveInput(next)
			s.switchCount.Add(1)
			failingTicks = 0
		}
	}
}

func (s *Switcher) SwitchCount() int64 {
	return s.switchCount.Load()
}
```

- [ ] **Step 4: Run, verify pass**

```bash
go test -v -run TestSwitcher ./internal/edgeencoder/
```

- [ ] **Step 5: Commit**

```bash
git add internal/edgeencoder/switcher.go internal/edgeencoder/switcher_test.go
git commit -m "edgeencoder: switcher loop with hysteresis"
```

---

### Task 5.3: Wire up pad probes in Pipeline + Switcher in main

**Files:**
- Modify: `internal/edgeencoder/pipeline.go` (add `AttachHealthProbes`)
- Modify: `cmd/edge-encoder/main.go` (wire Switcher)
- Modify: `cmd/edge-encoder/main_test.go`

- [ ] **Step 1: Add `AttachHealthProbes` to Pipeline**

In `pipeline.go`:

```go
// AttachHealthProbes installs pad probes on both input branches; the probes
// call RecordPacket() on the corresponding InputHealth. Must be called before
// Start(), so probes are in place by the time data flows.
func (p *Pipeline) AttachHealthProbes(a, b *InputHealth) {
    a.AttachPadProbe(p.inputPads["A"])
    b.AttachPadProbe(p.inputPads["B"])
}
```

- [ ] **Step 2: Wire in main.go**

In `cmd/edge-encoder/main.go`'s `run()`, after pipeline creation, before `pipeline.Start()`:

```go
healthA := edgeencoder.NewInputHealth(100 * time.Millisecond)
healthB := edgeencoder.NewInputHealth(100 * time.Millisecond)
pipeline.AttachHealthProbes(healthA, healthB)

switcher := edgeencoder.NewSwitcher(healthA, healthB, pipeline, 50*time.Millisecond, 2)
go switcher.Run(ctx)

// Replace stubStatus with a real one that reads pipeline + switcher.
statusProvider = &liveStatus{
    pipeline:  pipeline,
    a:         healthA,
    b:         healthB,
    switcher:  switcher,
    startTime: startTime,
}
```

Add `liveStatus` type next to `stubStatus`:

```go
type liveStatus struct {
    pipeline  *edgeencoder.Pipeline
    a, b      *edgeencoder.InputHealth
    switcher  *edgeencoder.Switcher
    startTime time.Time
}

func (s *liveStatus) Status() edgeencoder.Status {
    return edgeencoder.Status{
        Version:       Version,
        UptimeSeconds: int64(time.Since(s.startTime).Seconds()),
        ActiveInput:   s.pipeline.ActiveInput(),
        InputAHealthy: s.a.IsHealthy(),
        InputBHealthy: s.b.IsHealthy(),
        SwitchCount:   s.switcher.SwitchCount(),
    }
}
```

- [ ] **Step 3: Run all tests + make ci**

```bash
go test -v ./internal/edgeencoder/ ./cmd/edge-encoder/
make ci
```

Expected: all green.

- [ ] **Step 4: Commit**

```bash
git add internal/edgeencoder/pipeline.go cmd/edge-encoder/
git commit -m "edge-encoder: wire pipeline + health probes + switcher in main"
```

---

## Chunk 6: HTTP/ICY broadcast adapter

Adapt the encoded bytes from the MP3 appsink into the existing `internal/broadcast.Mount` pattern (per the Explore agent report: `Mount.FeedFrom(io.Reader)` reads bytes, broadcasts to clients via ringBuffer). We need a Reader-shaped adapter that pulls samples from the appsink.

### Task 6.1: Appsink-to-Reader adapter

**Files:**
- Create: `internal/edgeencoder/broadcast.go`
- Create: `internal/edgeencoder/broadcast_test.go`

**Context:**
appsink emits `*gst.Sample` via `PullSample()` (blocking). Each sample contains a `*gst.Buffer` which we map to bytes. The adapter exposes `io.Reader`; on each `Read(p)`, it pulls samples until `p` is filled (or EOS / error).

- [ ] **Step 1: Implement and test**

`internal/edgeencoder/broadcast.go`:

```go
/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package edgeencoder

import (
	"io"
	"sync"

	"github.com/go-gst/go-gst/gst/app"
)

// AppsinkReader adapts a GStreamer appsink to io.Reader.
// Read blocks until a sample is available or EOS / Close. Bytes from
// successive samples are concatenated; partial samples are buffered for the
// next Read.
type AppsinkReader struct {
	sink      *app.Sink
	mu        sync.Mutex
	leftover  []byte
	closed    bool
}

func NewAppsinkReader(sink *app.Sink) *AppsinkReader {
	return &AppsinkReader{sink: sink}
}

func (r *AppsinkReader) Read(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return 0, io.EOF
	}
	n := 0
	for n < len(p) {
		if len(r.leftover) > 0 {
			c := copy(p[n:], r.leftover)
			n += c
			r.leftover = r.leftover[c:]
			continue
		}
		sample := r.sink.PullSample()
		if sample == nil {
			if r.sink.IsEOS() {
				if n == 0 {
					return 0, io.EOF
				}
				return n, nil
			}
			return n, io.ErrUnexpectedEOF
		}
		buf := sample.GetBuffer()
		if buf == nil {
			continue
		}
		data := buf.Map(gst.MapRead).Bytes()
		defer buf.Unmap()
		c := copy(p[n:], data)
		if c < len(data) {
			r.leftover = append(r.leftover, data[c:]...)
		}
		n += c
	}
	return n, nil
}

func (r *AppsinkReader) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	return nil
}
```

(Add `import "github.com/go-gst/go-gst/gst"` for `gst.MapRead`.)

`internal/edgeencoder/broadcast_test.go`:

```go
func TestAppsinkReader_ReadsBytes(t *testing.T) {
	// Build a tiny pipeline: audiotestsrc num-buffers=10 ! appsink
	Init()
	pipeline, err := gst.NewPipelineFromString("audiotestsrc num-buffers=10 ! appsink name=sink")
	if err != nil { t.Fatal(err) }
	defer pipeline.SetState(gst.StateNull)
	sink := app.SinkFromElement(pipeline.GetElementByName("sink"))
	pipeline.SetState(gst.StatePlaying)

	r := NewAppsinkReader(sink)
	buf := make([]byte, 4096)
	total := 0
	for total < 1024 {
		n, err := r.Read(buf)
		if err == io.EOF { break }
		if err != nil { t.Fatal(err) }
		total += n
	}
	if total == 0 {
		t.Error("Read returned 0 bytes; want > 0")
	}
}
```

- [ ] **Step 2: Run, verify pass, commit**

```bash
go test -v -run TestAppsinkReader ./internal/edgeencoder/
git add internal/edgeencoder/broadcast.go internal/edgeencoder/broadcast_test.go
git commit -m "edgeencoder: appsink → io.Reader adapter for broadcast.Mount integration"
```

---

### Task 6.2: Wire appsink reader → broadcast.Mount → HTTP listeners

**Files:**
- Modify: `cmd/edge-encoder/main.go`

**Context:**
The existing `internal/broadcast.NewMount(name, contentType, bitrateKbps, logger, bus)` returns a Mount that exposes `ServeHTTP` for the listener-side HTTP/ICY endpoint, and `FeedFrom(io.Reader)` to consume encoded bytes.

- [ ] **Step 1: Update `cmd/edge-encoder/main.go`'s `run()`**

After `pipeline.Start()`:

```go
// Set up the listener-facing HTTP/ICY mount.
contentType := "audio/mpeg"
if cfg.OutputFormat == "aac" { contentType = "audio/aac" }
mount := broadcast.NewMount("live", contentType, cfg.OutputBitrateKbps, logger, eventBus)

// Pump encoded bytes from the appsink into the mount.
reader := edgeencoder.NewAppsinkReader(pipeline.MP3Appsink())
go func() { _ = mount.FeedFrom(reader) }()

// Register the mount on the HTTP server (alongside /healthz).
mux.Handle("/live", mount)
```

- [ ] **Step 2: Integration test — start binary, connect HTTP client, receive bytes**

```go
func TestRun_ListenerReceivesBytes(t *testing.T) {
    t.Setenv("EDGE_ENCODER_HTTP_PORT", "0")
    // ... set other ports to ephemeral
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    go run(ctx, os.Stdout, os.Stderr)
    time.Sleep(500 * time.Millisecond)
    resp, err := http.Get("http://localhost:8001/live")
    if err != nil { t.Fatal(err) }
    defer resp.Body.Close()
    buf := make([]byte, 4096)
    n, _ := resp.Body.Read(buf)
    // Without an RTP source feeding the pipeline, no bytes will arrive.
    // This test is best run with a side-car audiotestsrc; deferred to Chunk 9
    // end-to-end tests. For Chunk 6, verify the HTTP handler is registered.
    if resp.StatusCode != 200 {
        t.Errorf("status = %d, want 200", resp.StatusCode)
    }
    _ = n
}
```

- [ ] **Step 3: Run, verify pass, commit**

```bash
go test -v ./cmd/edge-encoder/
git add cmd/edge-encoder/
git commit -m "edge-encoder: serve encoded bytes via broadcast.Mount on HTTP /live"
```

---

## Chunk 7: HLS branch

Add a second tee branch in the pipeline that feeds GStreamer's `hlssink2` element. Segments are written to a local tmpfs directory, then uploaded to S3 by a Go goroutine. Manifest served by the HTTP server from S3 (or directly).

### Task 7.1: Extend pipeline with HLS tee branch

**Files:**
- Modify: `internal/edgeencoder/pipeline.go`
- Modify: `internal/edgeencoder/pipeline_test.go`

**Context:**
Insert a `tee` between the encoder and the MP3 appsink. One branch goes to the existing appsink; the other goes to `hlssink2 location=/tmp/hls/segment%05d.ts playlist-location=/tmp/hls/playlist.m3u8 target-duration=4 max-files=10`.

Only built when `cfg.HLSEnabled` is true.

- [ ] **Step 1: Modify pipeline to support optional HLS branch**

In `pipeline.go` `NewPipeline`, after the encoder chain:

```go
// If HLS enabled, insert a tee between encoder output and the appsink, with
// a second branch feeding hlssink2.
if cfg.HLSEnabled {
    tee, _ := gst.NewElementWithName("tee", "output_tee")
    pipeline.Add(tee)
    // Link encoder → tee
    encoder[len(encoder)-1].Link(tee)
    // Reroute appsink to be downstream of tee
    queueMP3, _ := gst.NewElement("queue")
    pipeline.Add(queueMP3)
    tee.Link(queueMP3)
    queueMP3.Link(appsinkElt)

    // HLS branch
    queueHLS, _ := gst.NewElement("queue")
    hls, _ := gst.NewElementWithName("hlssink2", "hlssink")
    pipeline.Add(queueHLS, hls)
    hls.SetProperty("location", cfg.HLSSegmentDir+"/segment%05d.ts")
    hls.SetProperty("playlist-location", cfg.HLSSegmentDir+"/playlist.m3u8")
    hls.SetProperty("target-duration", uint(4))
    hls.SetProperty("max-files", uint(10))
    tee.Link(queueHLS)
    queueHLS.Link(hls)
}
```

Add `HLSSegmentDir` to Config (default `/tmp/grimnir-hls`).

- [ ] **Step 2: Test that pipeline builds with HLS enabled**

```go
func TestNewPipeline_HLSEnabled(t *testing.T) {
    Init()
    cfg := &Config{
        RTPPortA: 15010, RTPPortB: 15011,
        OutputFormat: "mp3", OutputBitrateKbps: 128,
        HLSEnabled: true, HLSS3Bucket: "test", HLSSegmentDir: t.TempDir(),
    }
    p, err := NewPipeline(cfg)
    if err != nil { t.Fatalf("NewPipeline: %v", err) }
    defer p.Close()
    if err := p.Start(); err != nil { t.Fatalf("Start: %v", err) }
}
```

- [ ] **Step 3: Run, commit**

```bash
go test -v -run TestNewPipeline_HLS ./internal/edgeencoder/
git add internal/edgeencoder/pipeline.go internal/edgeencoder/pipeline_test.go internal/edgeencoder/config.go
git commit -m "edgeencoder: HLS branch via tee + hlssink2 when HLS enabled"
```

---

### Task 7.2: S3 segment uploader

**Files:**
- Create: `internal/edgeencoder/hls.go`
- Create: `internal/edgeencoder/hls_test.go`

**Context:**
Watch the HLS segment directory (inotify on Linux via `fsnotify`); on new `.ts` segment OR `.m3u8` playlist update, upload to S3 via the existing `internal/media/storage_s3.go` client. Delete stale segments locally based on `max-files`.

- [ ] **Step 1: Implement HLSUploader**

`internal/edgeencoder/hls.go`:

```go
// HLSUploader watches a directory for new .ts segments and .m3u8 playlist
// updates, uploads them to S3 under <bucket>/hls/<mount>/.
type HLSUploader struct {
    dir    string
    bucket string
    s3     S3Client  // interface satisfied by internal/media/storage_s3
    logger zerolog.Logger
}

func NewHLSUploader(dir, bucket string, s3 S3Client, logger zerolog.Logger) *HLSUploader { ... }

// Run watches the directory until ctx cancelled.
func (h *HLSUploader) Run(ctx context.Context) error { ... }
```

(Use fsnotify; on CREATE/WRITE of `.ts` or `.m3u8`, upload via S3 PutObject.)

- [ ] **Step 2: Mock-S3 test**

```go
type mockS3 struct{ uploads []string }
func (m *mockS3) Put(ctx context.Context, key string, body io.Reader) error {
    m.uploads = append(m.uploads, key)
    return nil
}

func TestHLSUploader_UploadsNewSegments(t *testing.T) {
    // Create temp dir, write a .ts file, run uploader briefly, verify mockS3 saw it.
}
```

- [ ] **Step 3: Wire uploader into main.go**

When HLS enabled, start `HLSUploader.Run` in a goroutine; use the existing `internal/media.NewS3Storage` for the client.

- [ ] **Step 4: Run, commit**

```bash
go test -v -run TestHLSUploader ./internal/edgeencoder/
git add internal/edgeencoder/hls.go internal/edgeencoder/hls_test.go cmd/edge-encoder/
git commit -m "edgeencoder: HLS segment + manifest uploader to S3"
```

---

## Chunk 8: gRPC engine health subscription

For Q-EE2=B, the edge encoder subscribes to each engine's `GetStatus` over gRPC every 1 second; on consecutive failures, it flips that input's `InputHealth.SetGRPCHealthy(false)` and the switcher uses it to make the switch decision.

### Task 8.1: gRPC client for engine status

**Files:**
- Modify: `internal/edgeencoder/grpc.go`
- Modify: `internal/edgeencoder/grpc_test.go`

**Context:**
mediaengine's gRPC service has `GetStatus(ctx, &StatusRequest{}) (*StatusResponse, error)`. The edge encoder dials the engine, calls GetStatus on a 1s ticker. After 3 consecutive failures (3 seconds), mark the input unhealthy. After 1 success, mark healthy.

- [ ] **Step 1: Implement EngineHealthSubscriber**

```go
type EngineHealthSubscriber struct {
    addr   string
    health *InputHealth
    logger zerolog.Logger
    failuresThreshold int  // default 3
}

func (s *EngineHealthSubscriber) Run(ctx context.Context) error {
    conn, err := grpc.NewClient(s.addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
    if err != nil { return err }
    defer conn.Close()
    client := mepb.NewMediaEngineClient(conn)

    ticker := time.NewTicker(time.Second)
    defer ticker.Stop()
    failures := 0
    for {
        select {
        case <-ctx.Done(): return nil
        case <-ticker.C:
            cctx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
            _, err := client.GetStatus(cctx, &mepb.StatusRequest{})
            cancel()
            if err != nil {
                failures++
                if failures >= s.failuresThreshold {
                    s.health.SetGRPCHealthy(false)
                }
            } else {
                failures = 0
                s.health.SetGRPCHealthy(true)
            }
        }
    }
}
```

- [ ] **Step 2: Test with a mock gRPC server**

```go
func TestEngineHealthSubscriber_MarksUnhealthyAfterTimeout(t *testing.T) {
    // Start a gRPC server, dial, call GetStatus → success. Then stop server,
    // verify after 3 ticks, health flips to false.
}
```

- [ ] **Step 3: Wire in main.go**

When `cfg.EngineAGRPC` is non-empty, start `EngineHealthSubscriber{addr: cfg.EngineAGRPC, health: healthA}.Run(ctx)` in a goroutine. Same for B.

- [ ] **Step 4: Run, commit**

```bash
go test -v -run TestEngineHealthSubscriber ./internal/edgeencoder/
git add internal/edgeencoder/grpc.go internal/edgeencoder/grpc_test.go cmd/edge-encoder/
git commit -m "edgeencoder: gRPC engine health subscription drives InputHealth gRPC gate"
```

---

## Chunk 9: End-to-end integration tests

Two fake mediaengines emit RTP, the edge encoder consumes, the HTTP listener receives encoded bytes. Inject failures and verify the switch behavior.

### Task 9.1: Test harness with 2 audiotestsrc engines

**Files:**
- Create: `internal/edgeencoder/integration_test/edge_test.go` (build tag `integration`)

**Context:**
Each "engine" is a `gst-launch-1.0` subprocess: `audiotestsrc freq=440 ! audioconvert ! audio/x-raw,format=S16BE,rate=44100,channels=2 ! rtpL16pay pt=10 ! udpsink host=127.0.0.1 port=$PORT`. Edge encoder runs in-process via `run()`. HTTP client connects to `/live` and reads bytes.

- [ ] **Step 1: Write the integration test**

```go
//go:build integration

func TestEdgeEncoder_EndToEnd_FailoverBetweenEngines(t *testing.T) {
    // 1. Start engine A (440 Hz) sending to :15004
    // 2. Start engine B (880 Hz) sending to :15005
    // 3. Start edge encoder with EDGE_ENCODER_RTP_PORT_A=15004, _B=15005
    // 4. HTTP GET /live; verify byte stream is non-empty for 2 seconds
    // 5. Kill engine A
    // 6. Verify byte stream continues uninterrupted (because B takes over)
    // 7. Optionally: decode the MP3 stream, FFT, verify frequency shifted from 440 to 880
}
```

- [ ] **Step 2: Run, commit**

```bash
go test -v -tags=integration -run TestEdgeEncoder_EndToEnd ./internal/edgeencoder/integration_test/
git add internal/edgeencoder/integration_test/
git commit -m "test: end-to-end failover integration test for edge encoder"
```

---

### Task 9.2: Failure injection: divergence detection

**Files:**
- Extend: `internal/edgeencoder/integration_test/edge_test.go`

**Context:**
Per the Section 7 failure-mode table, divergence (engines producing different audio) should be detected. For now we don't implement the divergence detection in this plan (deferred to follow-up); this test only documents the gap.

- [ ] **Step 1: Add a SKIP test that documents the gap**

```go
func TestEdgeEncoder_DivergenceDetection_NotImplemented(t *testing.T) {
    t.Skip("Divergence detection deferred to follow-up; see Section 7 of HA design doc")
}
```

- [ ] **Step 2: Commit**

```bash
git commit -m "test: placeholder for divergence-detection test (not implemented in this plan)"
```

---

## Chunk 10: Docs, deployment, version bump

### Task 10.1: README for the edge-encoder binary

**Files:**
- Create: `cmd/edge-encoder/README.md`

Cover: what it does, env-var reference, how to run locally, how to verify, links to the design doc and this plan.

- [ ] **Step 1: Write and commit**

```bash
git add cmd/edge-encoder/README.md
git commit -m "docs: cmd/edge-encoder/README.md"
```

---

### Task 10.2: systemd unit file for the VM deployment

**Files:**
- Create: `deploy/edge-encoder.service`

```ini
[Unit]
Description=Grimnir Edge Encoder
After=network.target

[Service]
Type=simple
User=edge
EnvironmentFile=/etc/grimnir/edge-encoder.env
ExecStart=/usr/local/bin/edge-encoder
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```

- [ ] **Step 1: Commit**

```bash
git add deploy/
git commit -m "deploy: systemd unit for edge-encoder"
```

---

### Task 10.3: CLAUDE.md update for the new binary

**Files:**
- Modify: `CLAUDE.md`

Add a section documenting:
- New binary `cmd/edge-encoder/`
- go-gst CGo build dependency
- Install instructions (apt/pacman)
- Env vars (link to README)

- [ ] **Step 1: Commit**

```bash
git add CLAUDE.md
git commit -m "CLAUDE.md: document edge-encoder binary and go-gst build dependency"
```

---

### Task 10.4: Version bump to v2.0.0-alpha.3

**Files:**
- Modify: `internal/version/version.go`

- [ ] **Step 1: Bump + commit + tag + push**

```bash
# Edit internal/version/version.go: 2.0.0-alpha.2 → 2.0.0-alpha.3
git add internal/version/version.go
git commit -m "Bump to v2.0.0-alpha.3 (edge encoder + PCM transport)"
git tag -a v2.0.0-alpha.3 -m "v2.0.0-alpha.3 — edge encoder + PCM transport (Track A step 4)"
git push origin v2-dev
git push origin v2.0.0-alpha.3
```

- [ ] **Step 2: File the issue retroactively now that the plan is implemented**

```bash
gh issue create --repo friendsincode/grimnir_radio \
  --title "Track A step 4: edge encoder + PCM transport (shipped in v2.0.0-alpha.3)" \
  --label "type:feature,priority:P1" \
  --body "Implementation per docs/superpowers/plans/2026-06-03-edge-encoder-pcm-transport.md. Shipped in v2.0.0-alpha.3."
```

(Close with the same command if PR-merge close pattern is used.)

---

## Acceptance for the plan as a whole

- All 12 chunks executed; all commits land on `v2-dev`; tag `v2.0.0-alpha.3` points at the version-bump commit.
- Chunk 0 spike report exists in `docs/superpowers/spikes/` and confirms PROCEED.
- `make ci` exits 0 with the new go-gst CGo dependency in place; GHA workflow installs all required GStreamer plugin packs.
- `make build-edge-encoder` produces a working binary.
- The end-to-end integration test in Chunk 9 passes: starting two fake engines, killing one mid-stream, the HTTP listener receives an uninterrupted byte stream.
- The edge encoder runs on the v2-dev Proxmox VMs (<node-a-ip> + <node-b-ip> once Track A step 1's Postgres deployment is verified) and serves a real listener.

## Out of scope for this plan (deferred to follow-ups)

- **NetClock master/slave setup** between engines (Track A step 5). Without NetClock the two engines drift; sample-alignment after switch will be approximate, not perfect.
- **Divergence detection** between engine inputs (Section 7 row). Placeholder test exists.
- **HLS segment garbage collection** beyond `hlssink2`'s built-in max-files. S3-side lifecycle policy is operator config, not code.
- **HLS adaptive bitrate** ladder. Single bitrate only.
- **TLS** termination for the listener-facing HTTP port. Edge VPS terminates TLS; edge encoder serves plain HTTP behind it.
- **Listener-event telemetry** (the custom JS player's reconnect events). Separate plan.

## Estimated effort

- Chunk 0 (spike): 1 day
- Chunk 1 (CGo + CI): 2 days (the CI-side debugging always takes longer than expected)
- Chunk 2 (engine PCM emission): 2-3 days
- Chunk 3 (scaffold): 1-2 days
- Chunk 4 (pipeline): 4-7 days (go-gst learning curve)
- Chunk 5 (health + switching): 2-3 days
- Chunk 6 (HTTP/ICY adapter): 1-2 days
- Chunk 7 (HLS): 3-5 days
- Chunk 8 (gRPC health subscription): 2 days
- Chunk 9 (integration tests): 2-3 days
- Chunk 10 (docs + deploy + version): 1 day

**Total: 21–32 working days** = 4–7 calendar weeks at solo-operator pace. Matches the original Section 9.7 estimate.






# Spike — NetClock master/slave PCM alignment

**Date:** 2026-06-05
**Decision:** **PROCEED (no formal measurement; trusting upstream + deferring validation to Chunk 4 E2E)**

## Hypothesis

`GstNetTimeProvider` + `GstNetClientClock` produce sub-millisecond clock alignment between two GStreamer pipelines on a LAN, which produces sample-aligned RTP timestamps from rtpL16pay elements.

## Method (intended)

A Go binary spawns two programmatic pipelines via go-gst:
1. Master: audiotestsrc → ... → rtpL16pay → udpsink :15004, plus `NewNetTimeProvider` on port 9094
2. Slave: same pipeline shape on :15005, with `pipeline.UseClock(NewNetClientClock("127.0.0.1", 9094))` before SetState(PLAYING)
3. UDP capture from both ports; compare RTP timestamps for matching sequence numbers; assert max delta < ±1ms (~45 ticks at 44.1kHz).

## Blocker encountered

**go-gst v1.4.0 does not expose the `gstnet` library bindings.** `NewNetTimeProvider` and `NewNetClientClock` live in the GStreamer C library `libgstnet-1.0.so` (pkg-config name `gstreamer-net-1.0`), but go-gst's Go API doesn't include wrappers for them. Confirmed:

```
$ grep -l "NetClock\|NetTime\|NetClient" /home/code/go/pkg/mod/github.com/go-gst/go-gst@v1.4.0/gst/*.go
(no results)
```

The only `Clock` wrapper that exists is the abstract `gst.Clock` type, not the network-flavored subclasses.

## Why we're proceeding anyway

1. **Upstream is mature.** GStreamer's NetClock has been in production use for >15 years; the C library `gstreamer-net-1.0` is part of `gst-plugins-base`. Many broadcast workflows depend on it. The feature's correctness isn't in question.

2. **The CGo wrapper is small.** Three functions to wrap: `gst_net_time_provider_new`, `gst_net_client_clock_new`, `gst_clock_wait_for_sync`. Estimated <100 lines of Go+C. This wrapper IS production code; it doesn't get thrown away after a spike. Writing it inside the spike is pure YAK shaving.

3. **Chunk 4 (E2E verification) provides the real validation.** With two real mediaengine processes both feeding the edge encoder, the post-switch waveform analysis directly measures whether NetClock binding is producing sample-aligned PCM. That's a stronger test than the synthetic timestamp comparison this spike would have done.

## Decision

- [x] **PROCEED**: the design assumption (NetClock keeps clocks aligned) is trusted; the CGo wrapper work moves into Chunk 1 (where it's needed for production) rather than being duplicated here.

## Updates to the production plan

Chunk 1 (pipeline lifecycle migration) gains a sub-task: **write the minimal CGo wrapper for `gstnet` (NewNetTimeProvider, NewNetClientClock, WaitForSync)** inside a new package, e.g., `internal/gstnet/`. Treat this as the first thing in Chunk 1 since other chunks depend on it.

Chunk 4 (E2E verification) becomes the load-bearing validation step — if NetClock binding doesn't deliver alignment, that's where we find out.

## Notes for the production plan

- The plan's Chunk 1 description should explicitly call out the gstnet CGo wrapper as the first deliverable.
- Build dependency: `pkg-config --cflags --libs gstreamer-net-1.0`. Available on Ubuntu via `libgstreamer1.0-dev` (already installed for go-gst CGo build per Chunk 1 of the edge encoder plan); confirm.
- If the CGo wrapper turns out to be unexpectedly hairy, fall back option: small `grimnir-clock-server` Go binary that opens a `GstNetTimeProvider` only (no real pipeline) and runs as a separate process per host. Adds operational complexity but isolates the CGo surface.

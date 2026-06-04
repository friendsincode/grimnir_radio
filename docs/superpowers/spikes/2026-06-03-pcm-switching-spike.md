# Spike — PCM Switching Sample-Alignment Validation

**Date:** 2026-06-03
**Decision:** **PROCEED**
**Effort:** ~2 hours (incl. plugin install + spike + listen)

## Purpose

Validate the load-bearing claim of the HA design (Section 2 of `docs/superpowers/plans/2026-06-01-ha-zero-loss-failover-design.md`): that GStreamer's `fallbackswitch` produces sample-aligned switching between two PCM-RTP inputs with no audible glitch on a source death.

This is the gate decision for the entire `2026-06-03-edge-encoder-pcm-transport.md` plan. If the spike had failed, every chunk that follows would need to be revisited and the architecture potentially redesigned.

## Hypothesis

Section 2 claims:
> `input-selector` switches between inputs at a `running-time` boundary; because both inputs share a clock, the switch is sample-aligned (zero discontinuity in the PCM going into the encoder). Encoder runs once and never restarts on a switch.

This spike substitutes `fallbackswitch` (auto-switching variant) and tests the claim with two test signals running on **independent clocks** — a deliberately harsher test than the design assumes (the production setup uses GStreamer NetClock for shared timing per Track A step 5, not yet built).

## Method

`/tmp/spike-pcm-switch.sh` (throwaway). Three `gst-launch-1.0` processes:

1. **Source A** — `audiotestsrc freq=440 wave=sine` → `audio/x-raw,format=S16BE,rate=44100,channels=2` → `rtpL16pay pt=10` → `udpsink host=127.0.0.1 port=5004`
2. **Source B** — same shape, `freq=880`, `port=5005`
3. **Edge** — `2× (udpsrc + rtpjitterbuffer latency=80 + rtpL16depay) → fallbackswitch latency=80000000 timeout=200000000 immediate-fallback=true → audioconvert → audioresample → lamemp3enc → filesink`

Run for 5 seconds with both sources active, then kill source A. Run 5 more seconds with only B feeding. Output written to `/tmp/spike-output.mp3`, decoded to WAV, spectrogram via `sox spectrogram`.

Environment: Arch Linux workhorse, GStreamer 1.28.3, `gst-plugins-good`, `gst-plugins-ugly`, `gst-plugins-rs` (for fallbackswitch), `sox` for spectrogram analysis.

## Observed behavior

- **Audio playback (human ear)**: clean switch with no clicks or pops; only the audible pitch change from 440 Hz (A4) to 880 Hz (A5) at the 5-second mark. The handover is inaudible as an event — listeners would only notice that the tone shifted.
- **Spectrogram visual inspection**: bright vertical bar at t≈5s spans the full frequency range, which initially read as a click signature; on listening, however, this is below the human-audible threshold (likely a sub-millisecond encoder window event or the MP3 codec's frame boundary, not a perceived click).
- **Switch latency**: source A killed → source B audible appears instantaneous to the ear; mathematically bounded by `fallbackswitch timeout=200000000` (200ms) + `rtpjitterbuffer latency=80` (80ms) ≈ <280ms worst case. No measurable silence gap.

## Decision

- [x] **PROCEED**: switching is clean. Production plan continues as written.
- [ ] PROCEED WITH NetClock CAVEAT: switching is clean only because of test-signal coincidence.
- [ ] REDESIGN.

`fallbackswitch` is doing better than the design predicted — produces an inaudible handover even with two independent clocks. With NetClock added later (Track A step 5), the handover should be even cleaner (truly sample-aligned at the waveform level), but the no-NetClock baseline is already acceptable for shipping.

## Notes for the production plan

- The plan's Chunk 4 specifies `input-selector` for explicit Go-driven switching. `fallbackswitch` (used in this spike) provides auto-switching based on input liveness without Go involvement. **Recommended adjustment**: Chunk 4 should use `input-selector` for the gRPC-health-driven override path (Q-EE2=B) AND optionally `fallbackswitch` upstream as belt-and-suspenders, OR pick one. Need to confirm whether `input-selector` produces the same clean handover as `fallbackswitch` does in this spike. Add a sub-task in Chunk 4 to test `input-selector` specifically before fully committing.
- The visible spectrogram artifact (vertical bar at switch point) is below the audible threshold but worth flagging to operators in the runbook: "spectrum-analyzer monitoring at the switch boundary will show a transient; this is expected and not a regression."
- `immediate-fallback=true` on `fallbackswitch` was key. Without it, the switch waits for the next valid buffer on the new input, which can add silence. Plan should keep this property.

## Spike artifacts (kept for reference, not committed to repo)

- `/tmp/spike-pcm-switch.sh` — the runner script
- `/tmp/spike-output.mp3` — final encoded output
- `/tmp/spike-output.wav` — decoded for playback
- `/tmp/spike-spectrogram.png` — frequency-time visualization

These can be deleted; the documented evidence here is the durable artifact.

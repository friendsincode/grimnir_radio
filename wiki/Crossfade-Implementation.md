# Crossfade Implementation Guide

This document explains the cue-point aware crossfade implementation in the Grimnir Radio media engine.

## Overview

The crossfade system provides seamless audio transitions between tracks using:
- **Cue-point awareness**: Uses intro/outro markers for optimal fade timing
- **GStreamer audiomixer**: Real-time mixing of multiple audio streams
- **Multiple fade curves**: Linear, logarithmic, exponential, S-curve
- **Configurable durations**: Independent fade-in/fade-out timing

## Architecture

### Components

**CrossfadeManager** (`internal/mediaengine/crossfade.go`)
- Manages the crossfade state machine
- Calculates optimal fade timing based on cue points
- Builds GStreamer audiomixer pipelines
- Monitors fade completion

**Pipeline** (`internal/mediaengine/pipeline.go`)
- Owns a CrossfadeManager instance
- Coordinates crossfade requests from gRPC service
- Maintains current/next track state

**Service** (`internal/mediaengine/service.go`)
- Exposes `Fade()` gRPC endpoint
- Routes requests to appropriate Pipeline

## How It Works

### 1. Fade Request Flow

```
Control Plane                 Media Engine                  GStreamer
     |                              |                            |
     | Fade(next_source, cue_points, fade_config)                |
     |----------------------------->|                            |
     |                              |                            |
     |                              | Calculate fade timing     |
     |                              | based on cue points       |
     |                              |                            |
     |                              | Build audiomixer pipeline |
     |                              | with both tracks          |
     |                              |                            |
     |                              | Launch gst-launch         |
     |                              |--------------------------->|
     |                              |                            |
     |                              |                   [Crossfade executing]
     |                              |                            |
     |                              | Monitor completion        |
     |                              |                            |
     | FadeResponse(success)        |                            |
     |<-----------------------------|                            |
```

### 2. Cue-Point Aware Timing

The system uses cue points to determine optimal fade timing:

**Without Cue Points:**
```
Current Track: |============================================|
                                         [Fade out starts]
Next Track:                                 |============|
                                         [Fade in starts]
```

**With Cue Points:**
```
Current Track: |=====[intro]===[body]==[outro]====|
                                    ^
                                    Outro cue point
                                    [Fade out starts here]

Next Track:        |=====[intro]===[body]===[outro]====|
                      ^
                      Intro end cue point
                      [Skip to here, fade in]
```

### 3. Fade Timing Calculation

```go
type FadeTiming struct {
    CurrentOutroStart time.Duration // When to start fading out current
    NextIntroEnd      time.Duration // When next track intro ends
    FadeOutDuration   time.Duration // How long to fade out
    FadeInDuration    time.Duration // How long to fade in
    OverlapDuration   time.Duration // Simultaneous playback time
}
```

**Algorithm:**
1. If current track has `outro_in` cue point → start fade out there
2. Otherwise → start fade out at `(duration - fade_out_ms)`
3. If next track has `intro_end` cue point → skip intro, start fade in
4. Otherwise → fade in from beginning
5. Overlap = min(fade_out, fade_in)

## GStreamer Pipeline Structure

### Audiomixer Pipeline

The crossfade uses two parallel pipelines mixed together:

```gstreamer
# Current track branch (fading out)
filesrc location="current.mp3" !
decodebin !
audioconvert !
audioresample !
volume name=current_volume volume=1.0 !  # Fades 1.0 → 0.0
queue name=current_queue !
audiomixer name=mix.

# Next track branch (fading in)
filesrc location="next.mp3" !
decodebin !
audioconvert !
audioresample !
volume name=next_volume volume=0.0 !     # Fades 0.0 → 1.0
queue name=next_queue !
mix.

# Mixed output
mix. !
audioconvert !
audioresample !
autoaudiosink
```

### Volume Fade Curves

Four fade curve types are supported:

**Linear:**
```
Volume
1.0 |     /
    |    /
    |   /
0.5 |  /
    | /
0.0 |/___________
    0    0.5    1.0  Progress
```

**Logarithmic (slower start, faster end):**
```
Volume
1.0 |        ___/
    |      _/
    |    _/
0.5 |  _/
    | /
0.0 |/___________
    0    0.5    1.0  Progress
```

**Exponential (faster start, slower end):**
```
Volume
1.0 |\___
    |    \__
    |       \__
0.5 |          \
    |           \
0.0 |____________\
    0    0.5    1.0  Progress
```

**S-Curve (smooth acceleration/deceleration):**
```
Volume
1.0 |        ___/
    |      _/
    |    /
0.5 |   |
    |  /
0.0 | /___
    0    0.5    1.0  Progress
```

## API Usage

### Via gRPC

```go
fadeReq := &pb.FadeRequest{
    StationId: "station-123",
    MountId:   "mount-456",
    NextSource: &pb.SourceConfig{
        Type:     pb.SourceType_SOURCE_TYPE_MEDIA,
        SourceId: "media-789",
        Path:     "/media/track.mp3",
    },
    NextCuePoints: &pb.CuePoints{
        IntroEnd: 3.5,  // Skip 3.5 seconds of intro
        OutroIn:  235.2, // Outro starts at 235.2 seconds
    },
    FadeConfig: &pb.FadeConfig{
        FadeInMs:  3000,
        FadeOutMs: 3000,
        Curve:     pb.FadeCurve_FADE_CURVE_SCURVE,
    },
}

resp, err := mediaEngineClient.Fade(ctx, fadeReq)
```

### From Control Plane

```go
// In executor or playout manager
mediaEngineClient.Fade(ctx, &mediaenginev1.FadeRequest{
    StationId:     entry.StationID,
    MountId:       entry.MountID,
    NextSource:    buildSourceConfig(nextEntry),
    NextCuePoints: nextEntry.CuePoints,
    FadeConfig: &mediaenginev1.FadeConfig{
        FadeInMs:  3000,
        FadeOutMs: 3000,
        Curve:     mediaenginev1.FadeCurve_FADE_CURVE_SCURVE,
    },
})
```

## Configuration

### Fade Config Parameters

- **FadeInMs** (int32): Fade in duration in milliseconds (default: 3000)
- **FadeOutMs** (int32): Fade out duration in milliseconds (default: 3000)
- **Curve** (enum): Fade curve type
  - `FADE_CURVE_LINEAR` - Constant rate
  - `FADE_CURVE_LOGARITHMIC` - Perceptually smooth (recommended)
  - `FADE_CURVE_EXPONENTIAL` - Aggressive fade
  - `FADE_CURVE_SCURVE` - Most natural sounding

### Cue Points

Cue points are extracted during media analysis:

```go
type CuePoints struct {
    IntroEnd float32  // Seconds from start (skip intro)
    OutroIn  float32  // Seconds from start (where outro begins)
}
```

**Best Practices:**
- Set `IntroEnd` to skip DJ intros, countdowns, or silence
- Set `OutroIn` to the point where the song's ending begins
- For songs with cold endings (abrupt stop), set `OutroIn` ~5 seconds before end
- For songs with long fades, set `OutroIn` at the start of the natural fade

## Implementation Status

### ✅ Implemented

- Cue-point aware fade timing calculation
- Audiomixer pipeline generation
- Multiple fade curve algorithms
- Fade state machine
- Integration with Pipeline and Service
- gRPC API endpoints

### ⚠️ Partially Implemented

- GStreamer process management (skeleton exists, needs completion)
- Volume automation (requires GStreamer Go bindings)
- Telemetry collection during fades

### ❌ Not Yet Implemented

- Dynamic volume control via GStreamer controller API
- Real-time fade curve adjustment
- Fade progress reporting via telemetry stream
- Multi-output support (one pipeline per mount)
- Proper Icecast/HTTP output encoding

## Future Enhancements

### 1. GStreamer Go Bindings

Currently using `gst-launch` command-line tool. For production:

```go
// Use github.com/tinyzimmer/go-gst for proper GStreamer integration
pipeline, _ := gst.NewPipelineFromString(pipelineStr)
pipeline.SetState(gst.StatePlaying)

// Enable dynamic volume control
volumeElement := pipeline.GetByName("current_volume")
controller := gst.InterpolationControlSourceNew()
volumeElement.AddControlBinding(controller, "volume")
controller.Set(0*gst.Second, 1.0)  // Start at 1.0
controller.Set(3*gst.Second, 0.0)  // Fade to 0.0 over 3 seconds
```

### 2. Advanced Fade Detection

Analyze track endings to automatically determine optimal fade points:
- Detect silence periods
- Measure energy decay rates
- Identify beat grids for beat-matched crossfades

### 3. Energy Matching

Match energy levels between outgoing and incoming tracks:
```
High Energy → High Energy: Quick fade (1-2 seconds)
High Energy → Low Energy:  Medium fade (3-4 seconds)
Low Energy  → High Energy: Longer fade (4-6 seconds)
```

### 4. Beat-Matched Crossfades

For electronic music, sync fade timing to beat grids:
- Detect BPM of both tracks
- Time crossfade to occur on phrase boundaries (16/32 beats)
- Maintain rhythmic continuity

## Troubleshooting

### Crossfade Not Smooth

**Symptoms:** Audible glitches, pops, or discontinuities

**Solutions:**
1. Increase buffer sizes in queue elements
2. Use S-curve instead of linear fade
3. Ensure cue points are accurately set
4. Check system audio latency

### Fade Timing Off

**Symptoms:** Crossfade starts too early or too late

**Solutions:**
1. Verify cue points are in seconds, not milliseconds
2. Check track duration metadata is accurate
3. Ensure Position tracking is working
4. Add buffer time to fade calculations

### CPU Spikes During Crossfade

**Symptoms:** High CPU usage during transitions

**Solutions:**
1. Reduce DSP graph complexity during fades
2. Use hardware-accelerated audio processing
3. Optimize queue sizes
4. Consider pre-rendering crossfades for repeated transitions

## Examples

### Example 1: Radio Imaging

Song with station ID at start and end:

```go
CuePoints{
    IntroEnd: 5.2,   // Skip 5.2 second station ID
    OutroIn:  234.5, // Outro with DJ voiceover starts
}

FadeConfig{
    FadeInMs:  2000, // Quick 2-second fade in
    FadeOutMs: 4000, // Longer 4-second fade out for voiceover
    Curve:     FADE_CURVE_SCURVE,
}
```

### Example 2: Club Mix

Electronic track with intro and outro segments:

```go
CuePoints{
    IntroEnd: 16.0,  // 16-beat intro (at 120 BPM = 8 seconds)
    OutroIn:  242.0, // 32-beat outro starts
}

FadeConfig{
    FadeInMs:  4000, // 4-second fade (8 beats at 120 BPM)
    FadeOutMs: 4000,
    Curve:     FADE_CURVE_LINEAR, // Linear for rhythmic content
}
```

### Example 3: Classical Music

Track with natural attack and decay:

```go
CuePoints{
    IntroEnd: 0.0,   // No intro to skip
    OutroIn:  0.0,   // No specific outro marker
}

FadeConfig{
    FadeInMs:  6000, // Long, gentle 6-second fade in
    FadeOutMs: 6000,
    Curve:     FADE_CURVE_LOGARITHMIC, // Perceptually smooth
}
```

## References

- [GStreamer Audiomixer Documentation](https://gstreamer.freedomlabs.com/documentation/audiomixer/)
- [EBU R128 Loudness Normalization](https://tech.ebu.ch/docs/r/r128.pdf)
- [Audio Crossfade Techniques](https://en.wikipedia.org/wiki/Fade_(audio_engineering))
- [GStreamer Controller API](https://gstreamer.freedomlabs.com/documentation/gstreamer/gstcontroller.html)

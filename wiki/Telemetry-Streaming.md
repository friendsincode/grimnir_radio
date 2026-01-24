# Telemetry Streaming

This document explains the real-time telemetry streaming system in the Grimnir Radio media engine.

## Overview

The media engine provides real-time audio metrics through a gRPC streaming endpoint. This allows the control plane to monitor:
- Audio levels and peaks
- Buffer health and underruns
- Playback position and state
- Loudness measurements (LUFS)
- Process health

## Architecture

### Data Flow

```
GStreamer Process → Output Parser → Telemetry Collector → gRPC Stream → Control Plane
       ↓                  ↓                  ↓                 ↓              ↓
  [Pipeline]    [Regex Extraction]   [Pipeline State]  [StreamTelemetry] [Monitoring]
```

### Components

**GStreamerProcess** (`internal/mediaengine/gstreamer.go`)
- Parses GStreamer output in real-time
- Extracts audio levels, buffer status, errors
- Calls OnTelemetry callback with updates

**Pipeline** (`internal/mediaengine/pipeline.go`)
- Maintains TelemetryCollector
- Updates telemetry from GStreamer callbacks
- Provides GetTelemetry() method

**Service** (`internal/mediaengine/service.go`)
- Implements StreamTelemetry gRPC endpoint
- Streams telemetry at requested interval
- Handles client disconnections

**Control Plane** (client)
- Connects to media engine via gRPC
- Receives telemetry stream
- Stores metrics for monitoring/alerting

## Telemetry Data

### Message Structure

```protobuf
message TelemetryData {
  string station_id = 1;
  string mount_id = 2;
  google.protobuf.Timestamp timestamp = 3;

  // Audio levels
  float audio_level_l = 4;  // Left channel RMS (-60 to 0 dBFS)
  float audio_level_r = 5;  // Right channel RMS
  float peak_level_l = 6;   // Left channel peak
  float peak_level_r = 7;   // Right channel peak

  // Loudness
  float loudness_lufs = 8;  // Integrated loudness (LUFS)
  float momentary_lufs = 9; // Momentary loudness
  float short_term_lufs = 10; // Short-term loudness

  // Buffer state
  int64 buffer_depth_ms = 11;
  int32 buffer_fill_percent = 12;
  int64 underrun_count = 13;

  // Playback state
  PlaybackState state = 14;
  int64 position_ms = 15; // Current playback position
  int64 duration_ms = 16; // Total duration (if known)
}
```

### Field Descriptions

**Audio Levels**
- `audio_level_l/r`: RMS (Root Mean Square) audio level in dBFS (-60 to 0)
- `peak_level_l/r`: Peak audio level in dBFS
- Updated from GStreamer `level` element
- Updated frequency: ~100-500ms

**Loudness (LUFS)**
- `loudness_lufs`: Integrated loudness per EBU R128
- `momentary_lufs`: 400ms momentary loudness
- `short_term_lufs`: 3-second short-term loudness
- Currently not extracted from GStreamer (placeholder)
- Requires `rgvolume` or `ebur128` element integration

**Buffer Status**
- `buffer_depth_ms`: Current buffer depth in milliseconds
- `buffer_fill_percent`: Queue fill percentage (0-100)
- `underrun_count`: Total number of buffer underruns
- Updated from GStreamer `queue` elements
- Indicates streaming health

**Playback State**
- `state`: Current playback state (idle, loading, playing, fading, etc.)
- `position_ms`: Current position in milliseconds
- `duration_ms`: Total track duration (if known)
- Updated from GStreamer position queries

**Metadata**
- `station_id`: Station identifier
- `mount_id`: Mount point identifier
- `timestamp`: When telemetry was captured

## Usage Examples

### Example 1: Basic Telemetry Streaming (Go Client)

```go
package main

import (
    "context"
    "fmt"
    "io"
    "log"

    "google.golang.org/grpc"
    "google.golang.org/grpc/credentials/insecure"

    pb "github.com/friendsincode/grimnir_radio/proto/mediaengine/v1"
)

func main() {
    // Connect to media engine
    conn, err := grpc.Dial("localhost:9091", grpc.WithTransportCredentials(insecure.NewCredentials()))
    if err != nil {
        log.Fatalf("Failed to connect: %v", err)
    }
    defer conn.Close()

    client := pb.NewMediaEngineClient(conn)

    // Start streaming telemetry
    stream, err := client.StreamTelemetry(context.Background(), &pb.TelemetryRequest{
        StationId:  "station-123",
        MountId:    "mount-456",
        IntervalMs: 1000, // Update every 1 second
    })
    if err != nil {
        log.Fatalf("Failed to start stream: %v", err)
    }

    // Receive telemetry updates
    for {
        telemetry, err := stream.Recv()
        if err == io.EOF {
            break
        }
        if err != nil {
            log.Fatalf("Stream error: %v", err)
        }

        // Process telemetry
        fmt.Printf("Station: %s, State: %s\n", telemetry.StationId, telemetry.State)
        fmt.Printf("Audio Level L: %.2f dBFS, Peak: %.2f dBFS\n",
            telemetry.AudioLevelL, telemetry.PeakLevelL)
        fmt.Printf("Buffer Fill: %d%%, Underruns: %d\n",
            telemetry.BufferFillPercent, telemetry.UnderrunCount)
        fmt.Printf("Position: %d ms / %d ms\n",
            telemetry.PositionMs, telemetry.DurationMs)
        fmt.Println("---")
    }
}
```

### Example 2: Monitoring with Alerts

```go
type TelemetryMonitor struct {
    client      pb.MediaEngineClient
    stationID   string
    mountID     string
    alertChan   chan Alert
}

type Alert struct {
    Level   string // warning, error, critical
    Message string
    Value   float64
}

func (tm *TelemetryMonitor) Monitor(ctx context.Context) {
    stream, err := tm.client.StreamTelemetry(ctx, &pb.TelemetryRequest{
        StationId:  tm.stationID,
        MountId:    tm.mountID,
        IntervalMs: 500, // Check every 500ms
    })
    if err != nil {
        log.Printf("Failed to start telemetry stream: %v", err)
        return
    }

    for {
        telemetry, err := stream.Recv()
        if err != nil {
            log.Printf("Telemetry stream error: %v", err)
            return
        }

        // Check for issues
        tm.checkAudioLevels(telemetry)
        tm.checkBufferHealth(telemetry)
        tm.checkPlaybackState(telemetry)
    }
}

func (tm *TelemetryMonitor) checkAudioLevels(t *pb.TelemetryData) {
    // Check for silence (audio too low)
    if t.PeakLevelL < -50 && t.PeakLevelR < -50 {
        tm.alertChan <- Alert{
            Level:   "warning",
            Message: "Audio levels very low (possible silence)",
            Value:   float64(t.PeakLevelL),
        }
    }

    // Check for clipping (audio too high)
    if t.PeakLevelL > -0.1 || t.PeakLevelR > -0.1 {
        tm.alertChan <- Alert{
            Level:   "error",
            Message: "Audio clipping detected",
            Value:   float64(t.PeakLevelL),
        }
    }
}

func (tm *TelemetryMonitor) checkBufferHealth(t *pb.TelemetryData) {
    // Check for buffer underruns
    if t.UnderrunCount > 0 {
        tm.alertChan <- Alert{
            Level:   "warning",
            Message: fmt.Sprintf("Buffer underruns detected: %d", t.UnderrunCount),
            Value:   float64(t.UnderrunCount),
        }
    }

    // Check for low buffer fill
    if t.BufferFillPercent < 20 {
        tm.alertChan <- Alert{
            Level:   "warning",
            Message: "Low buffer fill",
            Value:   float64(t.BufferFillPercent),
        }
    }
}

func (tm *TelemetryMonitor) checkPlaybackState(t *pb.TelemetryData) {
    // Check for stuck playback
    if t.State == pb.PlaybackState_PLAYBACK_STATE_LOADING {
        tm.alertChan <- Alert{
            Level:   "warning",
            Message: "Pipeline stuck in loading state",
            Value:   0,
        }
    }

    // Check for error state
    if t.State == pb.PlaybackState_PLAYBACK_STATE_ERROR {
        tm.alertChan <- Alert{
            Level:   "critical",
            Message: "Pipeline in error state",
            Value:   0,
        }
    }
}
```

### Example 3: Storing Metrics for Graphing

```go
import (
    "time"
    "github.com/prometheus/client_golang/prometheus"
)

type TelemetryCollector struct {
    audioLevelL   prometheus.Gauge
    audioLevelR   prometheus.Gauge
    peakLevelL    prometheus.Gauge
    peakLevelR    prometheus.Gauge
    bufferFill    prometheus.Gauge
    underruns     prometheus.Counter
    playbackState prometheus.Gauge
}

func NewTelemetryCollector(stationID, mountID string) *TelemetryCollector {
    labels := prometheus.Labels{
        "station_id": stationID,
        "mount_id":   mountID,
    }

    return &TelemetryCollector{
        audioLevelL: prometheus.NewGauge(prometheus.GaugeOpts{
            Name:        "mediaengine_audio_level_l",
            Help:        "Left channel audio level in dBFS",
            ConstLabels: labels,
        }),
        audioLevelR: prometheus.NewGauge(prometheus.GaugeOpts{
            Name:        "mediaengine_audio_level_r",
            Help:        "Right channel audio level in dBFS",
            ConstLabels: labels,
        }),
        bufferFill: prometheus.NewGauge(prometheus.GaugeOpts{
            Name:        "mediaengine_buffer_fill_percent",
            Help:        "Buffer fill percentage",
            ConstLabels: labels,
        }),
        underruns: prometheus.NewCounter(prometheus.CounterOpts{
            Name:        "mediaengine_underruns_total",
            Help:        "Total number of buffer underruns",
            ConstLabels: labels,
        }),
        playbackState: prometheus.NewGauge(prometheus.GaugeOpts{
            Name:        "mediaengine_playback_state",
            Help:        "Current playback state (numeric)",
            ConstLabels: labels,
        }),
    }
}

func (tc *TelemetryCollector) Update(telemetry *pb.TelemetryData) {
    tc.audioLevelL.Set(float64(telemetry.AudioLevelL))
    tc.audioLevelR.Set(float64(telemetry.AudioLevelR))
    tc.bufferFill.Set(float64(telemetry.BufferFillPercent))
    tc.underruns.Add(float64(telemetry.UnderrunCount))
    tc.playbackState.Set(float64(telemetry.State))
}

func (tc *TelemetryCollector) StreamAndCollect(ctx context.Context, client pb.MediaEngineClient, stationID, mountID string) {
    stream, err := client.StreamTelemetry(ctx, &pb.TelemetryRequest{
        StationId:  stationID,
        MountId:    mountID,
        IntervalMs: 1000,
    })
    if err != nil {
        log.Printf("Failed to start stream: %v", err)
        return
    }

    for {
        telemetry, err := stream.Recv()
        if err != nil {
            log.Printf("Stream error: %v", err)
            return
        }

        tc.Update(telemetry)
    }
}
```

### Example 4: WebSocket Forwarding to Frontend

```go
import (
    "encoding/json"
    "github.com/gorilla/websocket"
)

type TelemetryForwarder struct {
    mediaEngine pb.MediaEngineClient
    wsClients   map[*websocket.Conn]bool
    mu          sync.RWMutex
}

func (tf *TelemetryForwarder) StreamToWebSocket(stationID, mountID string) {
    stream, err := tf.mediaEngine.StreamTelemetry(context.Background(), &pb.TelemetryRequest{
        StationId:  stationID,
        MountId:    mountID,
        IntervalMs: 500,
    })
    if err != nil {
        log.Printf("Failed to start stream: %v", err)
        return
    }

    for {
        telemetry, err := stream.Recv()
        if err != nil {
            log.Printf("Stream error: %v", err)
            return
        }

        // Convert to JSON
        data, err := json.Marshal(telemetry)
        if err != nil {
            log.Printf("Failed to marshal telemetry: %v", err)
            continue
        }

        // Forward to all connected WebSocket clients
        tf.mu.RLock()
        for client := range tf.wsClients {
            if err := client.WriteMessage(websocket.TextMessage, data); err != nil {
                log.Printf("Failed to send to WebSocket client: %v", err)
                // Remove dead client
                delete(tf.wsClients, client)
            }
        }
        tf.mu.RUnlock()
    }
}
```

## Update Intervals

### Recommended Intervals

**Real-time Monitoring (UI):**
- Interval: 100-250ms
- Use case: Live audio meters, buffer indicators
- Trade-off: Higher network usage, more responsive

**Dashboard Metrics:**
- Interval: 1000ms (1 second)
- Use case: Station overview, multiple stations
- Trade-off: Balance between responsiveness and resource usage

**Long-term Monitoring:**
- Interval: 5000-10000ms (5-10 seconds)
- Use case: Historical data, trending
- Trade-off: Lower resource usage, less granular

**Health Checks:**
- Interval: 500-1000ms
- Use case: Automated monitoring, alerting
- Trade-off: Fast enough to detect issues, not too noisy

### Performance Impact

| Interval | Updates/min | Network (bytes/min) | CPU Impact |
|----------|-------------|---------------------|------------|
| 100ms    | 600         | ~120 KB             | Medium     |
| 250ms    | 240         | ~48 KB              | Low        |
| 500ms    | 120         | ~24 KB              | Very Low   |
| 1000ms   | 60          | ~12 KB              | Negligible |
| 5000ms   | 12          | ~2.4 KB             | Negligible |

*Note: Network usage based on ~200 bytes per telemetry message*

## Telemetry Sources

### GStreamer Elements

**Audio Level Detection**
```
... ! level name=level_meter post-messages=true interval=100000000 ! ...
```
- Extracts RMS and peak levels
- Configurable interval (nanoseconds)
- Posts messages on GStreamer bus

**Buffer Status**
```
... ! queue name=buffer max-size-buffers=200 ! ...
```
- Current buffer fill (current-level-buffers)
- Maximum buffer size (max-size-buffers)
- Underrun detection

**Position Queries**
- Uses `gst_element_query_position()`
- Requires proper GStreamer Go bindings
- Currently extracted from progress messages

### Future: Enhanced LUFS Measurement

**EBU R128 Loudness**
```
... ! ebur128 ! ...
```
- Integrated loudness (LUFS)
- Momentary loudness (400ms)
- Short-term loudness (3s)
- Loudness range (LRA)

**Current Implementation**
- LUFS fields exist but not populated
- Requires `ebur128` element integration
- Placeholder for future enhancement

## Error Handling

### Stream Interruptions

**Client Disconnection**
```go
for {
    telemetry, err := stream.Recv()
    if err == io.EOF {
        log.Println("Stream ended normally")
        break
    }
    if err != nil {
        log.Printf("Stream error: %v", err)
        // Attempt reconnection with exponential backoff
        time.Sleep(time.Second * time.Duration(math.Pow(2, retries)))
        // Reconnect...
        continue
    }
    // Process telemetry
}
```

**Context Cancellation**
```go
ctx, cancel := context.WithCancel(context.Background())
defer cancel()

stream, err := client.StreamTelemetry(ctx, req)

// Cancel when done
select {
case <-stopChan:
    cancel() // Gracefully closes stream
}
```

### Missing Pipeline

If the pipeline doesn't exist, StreamTelemetry returns:
```
rpc error: code = NotFound desc = pipeline not found: ...
```

Handle this by:
1. Ensuring pipeline is created first (LoadGraph)
2. Retrying after pipeline creation
3. Checking pipeline status before streaming

## Best Practices

### 1. Use Appropriate Intervals

Don't poll faster than needed:
- UI updates: 250-500ms is smooth enough
- Monitoring: 1-5 seconds is sufficient
- Don't use <100ms unless absolutely necessary

### 2. Handle Reconnections

Streams can be interrupted:
- Implement exponential backoff
- Don't spam reconnection attempts
- Log reconnection events

### 3. Buffer Telemetry Data

For batch processing:
```go
buffer := make([]*pb.TelemetryData, 0, 100)

for telemetry := range telemetryChan {
    buffer = append(buffer, telemetry)

    if len(buffer) >= 100 {
        // Process batch
        processBatch(buffer)
        buffer = buffer[:0]
    }
}
```

### 4. Use Context for Lifecycle

```go
ctx, cancel := context.WithTimeout(context.Background(), time.Minute*5)
defer cancel()

stream, err := client.StreamTelemetry(ctx, req)
// Stream will auto-close after 5 minutes
```

### 5. Monitor Telemetry Health

Track when last telemetry was received:
```go
lastTelemetry := time.Now()

for telemetry := range telemetryChan {
    lastTelemetry = time.Now()
    // Process...
}

// In separate goroutine
ticker := time.NewTicker(time.Second * 10)
for range ticker.C {
    if time.Since(lastTelemetry) > time.Second*30 {
        log.Warn("No telemetry received for 30 seconds")
    }
}
```

## Troubleshooting

### No Telemetry Updates

**Symptoms:** StreamTelemetry connects but no data received

**Solutions:**
1. Check pipeline is actually playing
2. Verify GStreamer process is running
3. Check GStreamer output is being parsed
4. Ensure level element is in pipeline
5. Check interval isn't too high

### Stale Position Data

**Symptoms:** position_ms doesn't update

**Solutions:**
1. Verify GStreamer progress messages are being parsed
2. Check CurrentTrack.Position is being updated
3. Ensure process.GetTelemetry() returns valid data
4. Add explicit position queries to GStreamer

### High CPU Usage from Telemetry

**Symptoms:** High CPU when streaming telemetry

**Solutions:**
1. Increase interval (reduce update frequency)
2. Check for telemetry processing bottlenecks
3. Use buffering/batching
4. Profile telemetry processing code

### Memory Leak in Telemetry Stream

**Symptoms:** Memory grows over time with active streams

**Solutions:**
1. Ensure streams are properly closed
2. Check for goroutine leaks in callbacks
3. Verify context cancellation propagates
4. Use pprof to identify leak source

## Future Enhancements

### Planned Features

1. **Enhanced Audio Analysis**
   - True peak detection
   - Phase correlation
   - Spectral analysis
   - Dynamic range measurement

2. **EBU R128 Integration**
   - Accurate LUFS measurement
   - Loudness range (LRA)
   - Program loudness tracking

3. **Historical Telemetry**
   - Store telemetry in time-series database
   - Query historical metrics
   - Trend analysis

4. **Advanced Alerts**
   - Configurable alert rules
   - Alert aggregation
   - Alert routing (email, SMS, webhook)

5. **Multi-Station Telemetry**
   - Single stream for multiple stations
   - Aggregated metrics
   - Cross-station comparison

## References

- [gRPC Streaming](https://grpc.io/docs/languages/go/basics/#server-side-streaming-rpc)
- [Protocol Buffers Timestamp](https://developers.google.com/protocol-buffers/docs/reference/google.protobuf#timestamp)
- [EBU R128 Loudness](https://tech.ebu.ch/docs/r/r128.pdf)
- [GStreamer Level Element](https://gstreamer.freedomlabs.com/documentation/level/index.html)

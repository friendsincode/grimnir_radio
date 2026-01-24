# GStreamer Process Management

This document explains how GStreamer processes are launched, monitored, and managed in the Grimnir Radio media engine.

## Overview

The media engine uses a sophisticated GStreamer process management system that provides:
- **Process Lifecycle Management**: Start, stop, graceful shutdown, force kill
- **Output Monitoring**: Real-time capture and parsing of stdout/stderr
- **Telemetry Extraction**: Extract audio levels, buffer status, and errors from GStreamer output
- **State Tracking**: Monitor process state transitions
- **Callbacks**: React to state changes, telemetry updates, and process exits

## Architecture

### Components

**GStreamerProcess** (`internal/mediaengine/gstreamer.go`)
- Manages a single GStreamer process
- Captures and parses output in real-time
- Extracts telemetry from GStreamer verbose output
- Provides callbacks for monitoring

**Pipeline** (`internal/mediaengine/pipeline.go`)
- Uses GStreamerProcess for playback
- Updates internal telemetry from GStreamer output
- Handles process lifecycle during play/stop/emergency

**CrossfadeManager** (`internal/mediaengine/crossfade.go`)
- Uses GStreamerProcess for crossfade mixer
- Monitors fade completion
- Cleans up processes after fades

**Supervisor** (`internal/mediaengine/supervisor.go`)
- Monitors pipeline health
- Detects stuck processes
- Triggers restarts on failure

## Process States

```
idle → starting → running → stopping → stopped
                       ↓
                    failed
```

### State Descriptions

- **idle**: Process not started
- **starting**: Process is being launched
- **running**: Process is active and healthy
- **stopping**: Graceful shutdown in progress
- **stopped**: Process exited normally
- **failed**: Process exited with error

## Launching Processes

### Basic Launch

```go
process := NewGStreamerProcess(ctx, GStreamerProcessConfig{
    ID:       "station-123-playback",
    Pipeline: "filesrc location=/media/track.mp3 ! decodebin ! autoaudiosink",
    LogLevel: "info",
}, logger)

if err := process.Start(pipelineStr); err != nil {
    return fmt.Errorf("failed to start: %w", err)
}
```

### With Callbacks

```go
process := NewGStreamerProcess(ctx, GStreamerProcessConfig{
    ID:       "station-123-playback",
    Pipeline: pipelineStr,
    LogLevel: "info",
    OnStateChange: func(state ProcessState) {
        logger.Info().Str("state", string(state)).Msg("process state changed")
    },
    OnTelemetry: func(telemetry *GStreamerTelemetry) {
        // Update your internal telemetry
        myTelemetry.AudioLevelL = telemetry.AudioLevelL
        myTelemetry.UnderrunCount = telemetry.UnderrunCount
    },
    OnExit: func(exitCode int, err error) {
        if err != nil {
            logger.Error().Err(err).Msg("process failed")
        }
    },
}, logger)
```

## Stopping Processes

### Graceful Shutdown

Sends SIGTERM and waits up to 5 seconds:

```go
if err := process.Stop(); err != nil {
    logger.Error().Err(err).Msg("graceful stop failed")
}
```

### Force Kill

Immediately kills the process:

```go
if err := process.Kill(); err != nil {
    logger.Error().Err(err).Msg("force kill failed")
}
```

### Best Practice Pattern

```go
// Try graceful stop first
if err := process.Stop(); err != nil {
    logger.Warn().Err(err).Msg("graceful stop failed, force killing")
    if killErr := process.Kill(); killErr != nil {
        return fmt.Errorf("failed to kill process: %w", killErr)
    }
}
```

## Telemetry Extraction

The GStreamerProcess automatically parses GStreamer's verbose output to extract metrics.

### Supported Metrics

**Audio Levels**
- `AudioLevelL`, `AudioLevelR`: Current audio levels (from level element)
- `PeakLevelL`, `PeakLevelR`: Peak levels

**Buffer Status**
- `BufferFillPct`: Queue fill percentage (0-100)
- `BufferDepthMS`: Buffer depth in milliseconds
- `UnderrunCount`: Number of buffer underruns
- `OverrunCount`: Number of buffer overruns

**Playback State**
- `PipelineState`: NULL, READY, PAUSED, PLAYING
- `CurrentPosition`: Current playback position

**Errors**
- `LastError`: Most recent error message
- `LastWarning`: Most recent warning message

### How It Works

GStreamerProcess uses regular expressions to parse GStreamer's output:

```go
// State changes: "Setting pipeline to PAUSED"
stateChangeRegex = regexp.MustCompile(`Setting pipeline to (\w+)`)

// Audio levels from level element
audioLevelRegex = regexp.MustCompile(`level.*?rms=([-0-9.]+).*?peak=([-0-9.]+)`)

// Buffer status
bufferStatusRegex = regexp.MustCompile(`queue.*?current-level-buffers=(\d+).*?max-size-buffers=(\d+)`)

// Errors and warnings
errorRegex = regexp.MustCompile(`ERROR:(.+)`)
warningRegex = regexp.MustCompile(`WARNING:(.+)`)

// Underruns
underrunRegex = regexp.MustCompile(`queue.*?is empty|underrun`)
```

### Getting Telemetry

```go
telemetry := process.GetTelemetry()

fmt.Printf("Audio Level L: %.2f\n", telemetry.AudioLevelL)
fmt.Printf("Buffer Fill: %d%%\n", telemetry.BufferFillPct)
fmt.Printf("Underruns: %d\n", telemetry.UnderrunCount)
```

## Output Monitoring

### Stdout and Stderr Capture

GStreamerProcess captures both stdout and stderr using pipes:

```go
stdout, err := cmd.StdoutPipe()
stderr, err := cmd.StderrPipe()
```

### Line-by-Line Parsing

Each line is parsed in real-time:

```go
scanner := bufio.NewScanner(stdout)
for scanner.Scan() {
    line := scanner.Text()
    parseOutputLine(line, "stdout")
}
```

### Logging Levels

Output is logged at different levels:
- **Trace**: Every line of GStreamer output (verbose)
- **Debug**: State changes, telemetry updates
- **Info**: Process start/stop events
- **Warn**: Warnings, underruns
- **Error**: Errors, process failures

## Process Monitoring

### Health Checks

The Supervisor performs periodic health checks:

```go
// Check every 5 seconds
healthCheckInterval = 5 * time.Second

// Maximum consecutive failures before restart
maxConsecutiveFails = 3

// Heartbeat timeout
heartbeatTimeout = 15 * time.Second
```

### Heartbeat Updates

Update heartbeat to indicate process is healthy:

```go
supervisor.UpdateHeartbeat(stationID)
```

### Automatic Restart

If a process fails health checks, the supervisor automatically restarts it:

```go
func (s *Supervisor) restartPipeline(stationID string, reason string) {
    // Destroy old pipeline
    s.pipelineManager.DestroyPipeline(stationID)

    // Control plane will recreate it automatically
}
```

### Restart Rate Limiting

Prevents restart loops:
- Maximum 5 restarts within 5 minutes
- If limit exceeded, supervisor gives up
- Rate limit resets after 5 minute window

## Usage Examples

### Example 1: Basic Playback

```go
func (p *Pipeline) Play(ctx context.Context, source *pb.SourceConfig) error {
    pipelineStr := "filesrc location=" + source.Path + " ! decodebin ! autoaudiosink"

    p.process = NewGStreamerProcess(ctx, GStreamerProcessConfig{
        ID:       fmt.Sprintf("%s-playback", p.StationID),
        Pipeline: pipelineStr,
        LogLevel: "info",
        OnTelemetry: func(gstTelem *GStreamerTelemetry) {
            // Update pipeline telemetry
            p.telemetry.AudioLevelL = gstTelem.AudioLevelL
            p.telemetry.UnderrunCount = gstTelem.UnderrunCount
        },
        OnExit: func(exitCode int, err error) {
            if err != nil {
                p.logger.Error().Err(err).Msg("playback failed")
                p.State = pb.PlaybackState_PLAYBACK_STATE_IDLE
            }
        },
    }, p.logger)

    return p.process.Start(pipelineStr)
}
```

### Example 2: Crossfade Mixer

```go
func (cfm *CrossfadeManager) launchMixer(ctx context.Context, pipeline string) error {
    cfm.mixerProcess = NewGStreamerProcess(ctx, GStreamerProcessConfig{
        ID:       fmt.Sprintf("%s-crossfade", cfm.stationID),
        Pipeline: pipeline,
        LogLevel: "info",
        OnTelemetry: func(telemetry *GStreamerTelemetry) {
            // Monitor fade progress
            cfm.logger.Trace().
                Float32("audio_level", telemetry.AudioLevelL).
                Msg("crossfade telemetry")
        },
        OnExit: func(exitCode int, err error) {
            // Clean up after fade completes
            cfm.mixerProcess = nil
        },
    }, cfm.logger)

    return cfm.mixerProcess.Start(pipeline)
}
```

### Example 3: Emergency Broadcast

```go
func (p *Pipeline) InsertEmergency(ctx context.Context, source *pb.SourceConfig) error {
    // Kill current process immediately
    if p.process != nil {
        p.process.Kill()
        p.process = nil
    }

    // Start emergency process with minimal latency
    pipelineStr := fmt.Sprintf("filesrc location=%s ! decodebin ! audioconvert ! autoaudiosink", source.Path)

    p.process = NewGStreamerProcess(ctx, GStreamerProcessConfig{
        ID:       fmt.Sprintf("%s-emergency", p.StationID),
        Pipeline: pipelineStr,
        LogLevel: "warning", // Minimal logging for emergency
        OnExit: func(exitCode int, err error) {
            // Return to idle after emergency
            p.State = pb.PlaybackState_PLAYBACK_STATE_IDLE
        },
    }, p.logger)

    return p.process.Start(pipelineStr)
}
```

## Pipeline Examples

### Basic Playback Pipeline

```bash
filesrc location=/media/track.mp3 ! \
decodebin ! \
audioconvert ! \
audioresample ! \
autoaudiosink
```

### With DSP Processing

```bash
filesrc location=/media/track.mp3 ! \
decodebin ! \
audioconvert ! \
audioresample ! \
rgvolume pre-amp=0.0 album-mode=false ! \
audiodynamic mode=compressor threshold=0.5 ratio=0.25 ! \
audioconvert ! \
autoaudiosink
```

### Crossfade Mixer

```bash
# Current track (fading out)
filesrc location=/media/current.mp3 ! \
decodebin ! \
audioconvert ! \
audioresample ! \
volume name=current_volume volume=1.0 ! \
queue name=current_queue ! \
audiomixer name=mix. \

# Next track (fading in)
filesrc location=/media/next.mp3 ! \
decodebin ! \
audioconvert ! \
audioresample ! \
volume name=next_volume volume=0.0 ! \
queue name=next_queue ! \
mix. \

# Mixed output
mix. ! \
audioconvert ! \
audioresample ! \
autoaudiosink
```

## Troubleshooting

### Process Won't Start

**Symptoms:** Start() returns error

**Solutions:**
1. Check GStreamer is installed: `gst-launch-1.0 --version`
2. Test pipeline manually: `gst-launch-1.0 filesrc location=test.mp3 ! decodebin ! autoaudiosink`
3. Check file paths are absolute
4. Ensure required GStreamer plugins are installed

### Process Dies Immediately

**Symptoms:** Process starts but exits within seconds

**Solutions:**
1. Check logs for GStreamer errors
2. Verify media file is valid and not corrupt
3. Check audio device is available (autoaudiosink)
4. Test with minimal pipeline first

### No Telemetry Updates

**Symptoms:** OnTelemetry callback never fires

**Solutions:**
1. Ensure GStreamer is running with `-v` flag (verbose output)
2. Check log level is not filtering GStreamer output
3. Add level element to pipeline: `... ! level ! ...`
3. Verify pipeline includes queue elements for buffer status

### High CPU Usage

**Symptoms:** GStreamer process using excessive CPU

**Solutions:**
1. Reduce output logging (use "warning" or "error" level)
2. Optimize pipeline (remove unnecessary conversions)
3. Use hardware-accelerated elements if available
4. Check for format mismatches causing resampling

### Memory Leaks

**Symptoms:** Memory usage grows over time

**Solutions:**
1. Ensure Stop() or Kill() is called when done
2. Check for goroutine leaks in callbacks
3. Verify context cancellation propagates correctly
4. Use pprof to identify leak source

## Performance Considerations

### Process Startup Time

- Cold start: ~100-300ms
- Warm start (file cached): ~50-100ms
- Emergency preemption: <50ms (force kill + new start)

### Resource Usage

**Per Process:**
- Memory: ~10-30 MB (depends on pipeline complexity)
- CPU: 1-5% (idle), 10-20% (active playback)
- File descriptors: 3 (stdin, stdout, stderr) + pipeline resources

**Recommendations:**
- Limit concurrent processes to 1-2 per station
- Use crossfade manager to coordinate transitions
- Clean up processes promptly after use

### Output Parsing Overhead

- Negligible CPU impact (~0.1%)
- Output parsing runs in separate goroutines
- Line-by-line parsing is efficient

## Future Enhancements

### Planned Features

1. **GStreamer Go Bindings**
   - Replace `gst-launch` with proper Go bindings
   - Direct pipeline control without subprocess overhead
   - Dynamic property changes without restart

2. **Advanced Telemetry**
   - LUFS calculation from raw audio buffers
   - Spectral analysis
   - Phase correlation

3. **Resource Limits**
   - CPU throttling
   - Memory limits
   - Disk I/O prioritization

4. **Process Pooling**
   - Pre-warm processes for faster transitions
   - Reuse processes when possible

5. **Enhanced Recovery**
   - Checkpoint/restore process state
   - Seamless recovery from crashes

## References

- [GStreamer Command Line Tools](https://gstreamer.freedomlabs.com/documentation/tools/gst-launch.html)
- [GStreamer Application Development Manual](https://gstreamer.freedomlabs.com/documentation/application-development/)
- [GStreamer Plugin Reference](https://gstreamer.freedomlabs.com/documentation/plugins_doc.html)

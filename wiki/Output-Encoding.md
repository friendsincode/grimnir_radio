# Output Encoding and Streaming

This document explains how audio encoding and output streaming work in the Grimnir Radio media engine.

## Overview

The media engine supports multiple audio formats and output destinations:

**Supported Formats:**
- MP3 (MPEG-1/2 Audio Layer 3)
- AAC (Advanced Audio Coding)
- Opus
- Vorbis
- FLAC (lossless)

**Supported Outputs:**
- Icecast2 servers
- Shoutcast servers
- Generic HTTP streaming
- File output
- Test sink (no output)

## Architecture

### Components

**EncoderBuilder** (`internal/mediaengine/encoder.go`)
- Builds GStreamer encoder and output elements
- Supports multiple audio formats
- Configures bitrate, sample rate, channels
- Handles Icecast/Shoutcast authentication and metadata

**EncoderConfig** (`internal/mediaengine/encoder.go`)
- Configuration for encoding and output
- Contains output URL, credentials, format settings
- Validated before pipeline creation

**Pipeline** (`internal/mediaengine/pipeline.go`)
- Uses EncoderBuilder to create output chain
- Appends encoder/output to DSP processing
- Each pipeline has its own encoder configuration

## Configuration

### EncoderConfig Structure

```go
type EncoderConfig struct {
    // Output settings
    OutputType OutputType // icecast, shoutcast, http, file, test
    OutputURL  string     // Full URL for streaming

    // Icecast/Shoutcast settings
    Username    string // Usually "source" for Icecast
    Password    string
    Mount       string // Mount point (e.g., "/stream.mp3")
    StreamName  string // Station name
    Description string
    Genre       string
    URL         string // Station URL

    // Encoder settings
    Format     AudioFormat // mp3, aac, opus, vorbis, flac
    Bitrate    int         // Bitrate in kbps (e.g., 128, 192, 256)
    SampleRate int         // Sample rate in Hz (e.g., 44100, 48000)
    Channels   int         // Number of channels (1 = mono, 2 = stereo)
    Quality    float32     // Quality setting for VBR codecs (0.0-1.0)

    // Advanced encoder options
    EncoderOptions map[string]string
}
```

### Output Types

**Icecast** (`OutputTypeIcecast`)
- Streams to Icecast2 server
- Uses HTTP protocol
- Supports metadata updates
- Requires mount point, username, password

**Shoutcast** (`OutputTypeShoutcast`)
- Streams to Shoutcast server
- Uses ICY protocol
- Requires password (no mount point)

**HTTP** (`OutputTypeHTTP`)
- Generic HTTP streaming
- Uses souphttpclientsink
- No authentication or metadata

**File** (`OutputTypeFile`)
- Writes to local file
- Useful for recording streams

**Test** (`OutputTypeTest`)
- Discards output (fakesink)
- Useful for testing without actual streaming

### Audio Formats

**MP3** (`AudioFormatMP3`)
- Most widely supported
- Uses lamemp3enc encoder
- CBR (constant bitrate) mode
- Recommended bitrates: 128, 192, 256 kbps

**AAC** (`AudioFormatAAC`)
- Better quality than MP3 at same bitrate
- Uses avenc_aac encoder
- Recommended bitrates: 96, 128, 192 kbps

**Opus** (`AudioFormatOpus`)
- Modern, efficient codec
- Best for low latency
- Uses opusenc encoder
- Recommended bitrates: 64, 96, 128 kbps

**Vorbis** (`AudioFormatVorbis`)
- Open-source alternative to MP3
- Uses vorbisenc encoder
- Supports VBR (variable bitrate)
- Recommended bitrates: 128, 192, 256 kbps

**FLAC** (`AudioFormatFLAC`)
- Lossless compression
- Uses flacenc encoder
- Larger file sizes
- Quality setting: 0-8

## Usage Examples

### Example 1: Stream MP3 to Icecast

```go
outputConfig := &EncoderConfig{
    OutputType:  OutputTypeIcecast,
    OutputURL:   "http://icecast.example.com:8000/stream.mp3",
    Username:    "source",
    Password:    "hackme",
    Mount:       "/stream.mp3",
    StreamName:  "My Radio Station",
    Description: "Playing the best music 24/7",
    Genre:       "Various",
    URL:         "https://mystation.example.com",
    Format:      AudioFormatMP3,
    Bitrate:     192,
    SampleRate:  44100,
    Channels:    2,
}

// Create encoder builder
encoder := NewEncoderBuilder(*outputConfig)

// Validate configuration
if err := encoder.ValidateConfig(); err != nil {
    return fmt.Errorf("invalid config: %w", err)
}

// Build encoder pipeline string
outputChain, err := encoder.Build()
if err != nil {
    return fmt.Errorf("build encoder: %w", err)
}

// outputChain contains:
// "audioconvert ! audioresample ! audio/x-raw,rate=44100,channels=2 ! lamemp3enc target=1 bitrate=192 cbr=true ! shout2send ip=icecast.example.com port=8000 mount=/stream.mp3 username=source password=hackme protocol=http streamname=\"My Radio Station\" ..."
```

### Example 2: Stream AAC to Shoutcast

```go
outputConfig := &EncoderConfig{
    OutputType:  OutputTypeShoutcast,
    OutputURL:   "http://shoutcast.example.com:8000",
    Password:    "hackme",
    StreamName:  "My AAC Stream",
    Format:      AudioFormatAAC,
    Bitrate:     128,
    SampleRate:  48000,
    Channels:    2,
}

encoder := NewEncoderBuilder(*outputConfig)
outputChain, err := encoder.Build()

// outputChain contains:
// "audioconvert ! audioresample ! audio/x-raw,rate=48000,channels=2 ! avenc_aac bitrate=128000 ! shout2send ip=shoutcast.example.com port=8000 password=hackme protocol=icy streamname=\"My AAC Stream\""
```

### Example 3: Record to File

```go
outputConfig := &EncoderConfig{
    OutputType:  OutputTypeFile,
    OutputURL:   "/recordings/stream.mp3",
    Format:      AudioFormatMP3,
    Bitrate:     256,
    SampleRate:  44100,
    Channels:    2,
}

encoder := NewEncoderBuilder(*outputConfig)
outputChain, err := encoder.Build()

// outputChain contains:
// "audioconvert ! audioresample ! audio/x-raw,rate=44100,channels=2 ! lamemp3enc target=1 bitrate=256 cbr=true ! filesink location=\"/recordings/stream.mp3\""
```

### Example 4: Opus Streaming (Low Latency)

```go
outputConfig := &EncoderConfig{
    OutputType:  OutputTypeIcecast,
    OutputURL:   "http://icecast.example.com:8000/opus",
    Username:    "source",
    Password:    "hackme",
    Mount:       "/opus",
    StreamName:  "Low Latency Opus Stream",
    Format:      AudioFormatOpus,
    Bitrate:     96,
    SampleRate:  48000, // Opus works best at 48kHz
    Channels:    2,
}

encoder := NewEncoderBuilder(*outputConfig)
outputChain, err := encoder.Build()

// outputChain contains:
// "audioconvert ! audioresample ! audio/x-raw,rate=48000,channels=2 ! opusenc bitrate=96000 ! oggmux ! shout2send ..."
```

## Pipeline Integration

### Complete Playback Pipeline

A full GStreamer pipeline includes:
1. **Source**: File, HTTP stream, or live input
2. **Decoder**: Decodebin for automatic format detection
3. **DSP Processing**: Loudness, EQ, compression, limiting
4. **Encoder**: MP3, AAC, Opus, etc.
5. **Muxer** (if needed): Ogg for Opus/Vorbis
6. **Output**: Icecast, Shoutcast, HTTP, file

```
filesrc location=/media/track.mp3 !
decodebin !
audioconvert !
audioresample !
rgvolume pre-amp=0.0 !                    # DSP: Loudness normalization
audiodynamic mode=compressor ratio=0.25 ! # DSP: Compression
audioconvert !
audioresample !
audio/x-raw,rate=44100,channels=2 !       # Encoder input format
lamemp3enc target=1 bitrate=192 cbr=true !# MP3 encoder
shout2send                                 # Icecast output
    ip=icecast.example.com
    port=8000
    mount=/stream.mp3
    username=source
    password=hackme
    protocol=http
    streamname="My Station"
```

### How Pipeline.buildPlaybackPipeline() Works

```go
func (p *Pipeline) buildPlaybackPipeline(track *Track) (string, error) {
    // 1. Build source (filesrc, souphttpsrc, tcpserversrc)
    source := "filesrc location=/media/track.mp3 ! decodebin"

    // 2. Add DSP graph processing (optional)
    dspChain := ""
    if p.Graph != nil && p.Graph.Pipeline != "" {
        dspChain = " ! " + p.Graph.Pipeline
    }

    // 3. Build encoder and output using EncoderBuilder
    encoder := NewEncoderBuilder(*p.OutputConfig)
    encoder.ValidateConfig()
    outputChain, err := encoder.Build()

    // 4. Combine all parts
    pipeline := source + dspChain + " ! " + outputChain
    return pipeline, nil
}
```

## Metadata Updates

### ICY Metadata

For Icecast/Shoutcast, you can update the "Now Playing" metadata without restarting the stream:

```go
metadata := UpdateMetadata("Artist Name", "Song Title")
// Returns: "Artist Name - Song Title"

// To actually update in GStreamer (requires GObject bindings):
// g_object_set(shout2send_element, "meta", metadata, NULL)
```

**Note**: Metadata updates currently require proper GStreamer Go bindings. The command-line `gst-launch` approach doesn't support dynamic metadata updates.

### Future: Dynamic Metadata with Go Bindings

```go
// With github.com/tinyzimmer/go-gst
shout2send := pipeline.GetByName("shout2send")
shout2send.SetProperty("meta", fmt.Sprintf("%s - %s", artist, title))
```

## Encoder Settings Guide

### Bitrate Recommendations

**MP3:**
- 96 kbps: Acceptable for talk radio
- 128 kbps: Standard web streaming quality
- 192 kbps: High quality for music
- 256-320 kbps: Audiophile quality

**AAC:**
- 64 kbps: Acceptable for talk radio
- 96 kbps: Standard web streaming (equivalent to MP3 128)
- 128 kbps: High quality (equivalent to MP3 192)
- 192 kbps: Audiophile quality

**Opus:**
- 48 kbps: Good for voice
- 64 kbps: Acceptable for music
- 96 kbps: High quality music
- 128 kbps: Excellent quality

**Vorbis:**
- Similar to MP3 bitrates
- 128-192 kbps recommended for music

### Sample Rate Selection

**8000 Hz**: Telephone quality (not recommended)
**11025 Hz**: AM radio quality
**16000 Hz**: Wideband voice (VoIP)
**22050 Hz**: FM radio quality
**32000 Hz**: Miniature disc quality
**44100 Hz**: CD quality (standard for music)
**48000 Hz**: Professional audio (best for Opus)
**96000 Hz**: High-resolution audio (rare)

### Channels

**Mono (1 channel):**
- Uses half the bitrate
- Suitable for voice/talk radio
- Smaller file sizes

**Stereo (2 channels):**
- Full spatial audio
- Required for music
- Standard for broadcasting

## GStreamer Elements Used

### Encoders

- **lamemp3enc**: MP3 encoder (LAME library)
- **avenc_aac**: AAC encoder (FFmpeg libavcodec)
- **fdkaacenc**: High-quality AAC encoder (Fraunhofer FDK)
- **opusenc**: Opus encoder
- **vorbisenc**: Vorbis encoder
- **flacenc**: FLAC encoder (lossless)

### Muxers

- **oggmux**: Ogg container (for Opus/Vorbis)
- **mp4mux**: MP4 container (for AAC, if needed)

### Outputs

- **shout2send**: Icecast/Shoutcast streaming
- **souphttpclientsink**: Generic HTTP streaming
- **filesink**: File output
- **fakesink**: Discard output (testing)

## Required GStreamer Plugins

To use all features, install these GStreamer plugins:

```bash
# Debian/Ubuntu
sudo apt-get install \
    gstreamer1.0-plugins-base \
    gstreamer1.0-plugins-good \
    gstreamer1.0-plugins-bad \
    gstreamer1.0-plugins-ugly \
    gstreamer1.0-libav

# Arch Linux
sudo pacman -S \
    gstreamer \
    gst-plugins-base \
    gst-plugins-good \
    gst-plugins-bad \
    gst-plugins-ugly \
    gst-libav

# Fedora/RHEL
sudo dnf install \
    gstreamer1-plugins-base \
    gstreamer1-plugins-good \
    gstreamer1-plugins-bad-free \
    gstreamer1-plugins-ugly \
    gstreamer1-libav
```

### Plugin Requirements by Format

**MP3**: `gst-plugins-ugly` (lamemp3enc)
**AAC**: `gst-libav` (avenc_aac) or `gst-plugins-bad` (fdkaacenc)
**Opus**: `gst-plugins-base` (opusenc)
**Vorbis**: `gst-plugins-base` (vorbisenc)
**FLAC**: `gst-plugins-good` (flacenc)
**Icecast/Shoutcast**: `gst-plugins-good` (shout2send)

## Troubleshooting

### Encoder Not Found

**Symptoms:** Pipeline fails with "no element named 'lamemp3enc'"

**Solutions:**
1. Check GStreamer plugins are installed
2. Verify plugin with: `gst-inspect-1.0 lamemp3enc`
3. Install missing plugin package
4. Rebuild GStreamer plugin cache: `gst-inspect-1.0`

### Connection Refused to Icecast

**Symptoms:** shout2send fails with connection error

**Solutions:**
1. Check Icecast server is running
2. Verify URL, port, and mount point
3. Check firewall allows outbound connection
4. Test connection: `telnet icecast.example.com 8000`
5. Verify username/password are correct

### Poor Audio Quality

**Symptoms:** Audio sounds distorted or compressed

**Solutions:**
1. Increase bitrate (e.g., 128 → 192 kbps)
2. Check DSP processing isn't over-compressing
3. Ensure source audio quality is good
4. Use better codec (AAC or Opus instead of MP3)
5. Check sample rate matches source (44.1kHz or 48kHz)

### Stream Buffering Issues

**Symptoms:** Clients experience frequent buffering

**Solutions:**
1. Increase network bandwidth
2. Reduce bitrate
3. Add queue elements for buffering
4. Check Icecast server resources
5. Use more efficient codec (Opus)

### Metadata Not Updating

**Symptoms:** Song titles don't change on Icecast

**Solutions:**
1. Use proper GStreamer Go bindings (not gst-launch)
2. Check shout2send element name is correct
3. Verify Icecast supports ICY metadata
4. Check metadata format is "Artist - Title"

## Performance Considerations

### CPU Usage by Format

**FLAC (lossless)**: Highest CPU (~15-20%)
**MP3 (LAME)**: Medium-high CPU (~10-15%)
**AAC (avenc_aac)**: Medium CPU (~8-12%)
**Vorbis**: Medium CPU (~8-12%)
**Opus**: Lowest CPU (~5-8%)

### Bandwidth Usage

At 44.1kHz stereo:

| Bitrate | MB/hour | MB/day (24h) |
|---------|---------|--------------|
| 64 kbps | 28.8    | 691          |
| 96 kbps | 43.2    | 1,037        |
| 128 kbps| 57.6    | 1,382        |
| 192 kbps| 86.4    | 2,074        |
| 256 kbps| 115.2   | 2,765        |

### Latency

- **File → Encoder → Icecast**: ~2-4 seconds (typical)
- **Opus with low latency**: ~1-2 seconds
- **Emergency broadcasts**: <1 second (bypasses DSP)

## Future Enhancements

### Planned Features

1. **Dynamic Bitrate Switching**
   - Adaptive bitrate based on network conditions
   - Multiple bitrate streams from single pipeline

2. **HLS/DASH Support**
   - HTTP Live Streaming (HLS)
   - MPEG-DASH for adaptive streaming

3. **Enhanced Metadata**
   - Cover art (Base64 in ICY metadata)
   - Rich metadata (JSON)
   - Track timing information

4. **Multi-Output**
   - Single source → multiple formats/bitrates
   - Use tee element for branching

5. **Advanced Codecs**
   - HE-AAC (High Efficiency AAC)
   - LC3 (Low Complexity Communication Codec)

## References

- [GStreamer Encoding Tutorial](https://gstreamer.freedomlabs.com/documentation/tutorials/basic/media-formats-and-pad-capabilities.html)
- [Icecast Server Documentation](https://icecast.org/docs/)
- [Shoutcast Protocol Specification](http://www.smackfu.com/stuff/programming/shoutcast.html)
- [LAME MP3 Encoder](https://lame.sourceforge.io/)
- [Opus Codec](https://opus-codec.org/)
- [AAC Audio Codec](https://en.wikipedia.org/wiki/Advanced_Audio_Coding)

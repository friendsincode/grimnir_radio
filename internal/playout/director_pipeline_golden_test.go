/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package playout

import (
	"testing"

	"github.com/rs/zerolog"

	"github.com/friendsincode/grimnir_radio/internal/config"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

// Golden-string assertions for the director's GStreamer pipeline builders
// (issue #254). A wrong cap, a dropped element, or a wrong port compiles fine
// & only fails at runtime on the engine; freezing the exact strings turns any
// graph edit into a reviewable test diff. Update a golden deliberately when
// the graph change is intentional — never to silence the test.
//
// Known quirk pinned as-is: webstream relays encode AAC with faac while file
// playback uses avenc_aac. If that is ever unified, both goldens change in
// one reviewable diff.

func goldenDirector(cfg *config.Config, webrtc bool) *Director {
	return &Director{cfg: cfg, webrtcEnabled: webrtc, logger: zerolog.Nop()}
}

var (
	goldenMountMP3 = models.Mount{Format: "mp3", SampleRate: 44100, Channels: 2, Bitrate: 128, Name: "main"}
	goldenMountAAC = models.Mount{Format: "aac", SampleRate: 48000, Channels: 2, Bitrate: 96, Name: "main"}
	goldenMountOGG = models.Mount{Format: "ogg", SampleRate: 44100, Channels: 2, Bitrate: 128, Name: "main"}
)

func TestGolden_WebstreamBroadcastPipeline(t *testing.T) {
	plain := goldenDirector(&config.Config{}, false)
	full := goldenDirector(&config.Config{
		LiveInputEnabled: true, LiveInputPort: 5100,
		HAPCMRTPEnabled: true, HAPCMRTPTargets: []string{"10.0.0.5:5004", "10.0.0.6:5004"},
	}, true)

	cases := []struct {
		name    string
		d       *Director
		mount   models.Mount
		ws      *models.Webstream
		hq, lq  int
		rtpPort int
		want    string
	}{
		{
			name:  "mp3 with metadata passthrough and jitter buffer",
			d:     plain,
			mount: goldenMountMP3,
			ws:    &models.Webstream{Name: "relay", PassthroughMetadata: true, BufferSizeMS: 2000},
			hq:    128, lq: 64,
			want: `souphttpsrc location="https://up.example/live" is-live=true do-timestamp=true retries=3 timeout=10 iradio-mode=true ! queue max-size-time=2000000000 ! watchdog timeout=15000 ! decodebin ! audioconvert ! audioresample ! audio/x-raw,rate=44100,channels=2 ! tee name=t t. ! queue ! lamemp3enc target=1 bitrate=128 cbr=true ! fdsink fd=3 t. ! queue ! lamemp3enc target=1 bitrate=64 cbr=true ! fdsink fd=4`,
		},
		{
			name:  "aac no buffer no metadata",
			d:     plain,
			mount: goldenMountAAC,
			ws:    &models.Webstream{Name: "relay"},
			hq:    96, lq: 48,
			want: `souphttpsrc location="https://up.example/live" is-live=true do-timestamp=true retries=3 timeout=10 ! watchdog timeout=15000 ! decodebin ! audioconvert ! audioresample ! audio/x-raw,rate=48000,channels=2 ! tee name=t t. ! queue ! faac bitrate=96000 ! audio/mpeg,mpegversion=4 ! fdsink fd=3 t. ! queue ! faac bitrate=48000 ! audio/mpeg,mpegversion=4 ! fdsink fd=4`,
		},
		{
			name:  "ogg vorbis",
			d:     plain,
			mount: goldenMountOGG,
			ws:    &models.Webstream{Name: "relay"},
			hq:    128, lq: 64,
			want: `souphttpsrc location="https://up.example/live" is-live=true do-timestamp=true retries=3 timeout=10 ! watchdog timeout=15000 ! decodebin ! audioconvert ! audioresample ! audio/x-raw,rate=44100,channels=2 ! tee name=t t. ! queue ! vorbisenc bitrate=128000 ! oggmux ! fdsink fd=3 t. ! queue ! vorbisenc bitrate=64000 ! oggmux ! fdsink fd=4`,
		},
		{
			name:  "everything on: live mixer, webrtc branch, HA PCM-RTP fan-out",
			d:     full,
			mount: goldenMountMP3,
			ws:    &models.Webstream{Name: "relay", PassthroughMetadata: true, BufferSizeMS: 2000},
			hq:    128, lq: 64, rtpPort: 5204,
			want: `souphttpsrc location="https://up.example/live" is-live=true do-timestamp=true retries=3 timeout=10 iradio-mode=true ! queue max-size-time=2000000000 ! watchdog timeout=15000 ! decodebin ! audioconvert ! audioresample ! audio/x-raw,rate=44100,channels=2 ! audiomixer name=mix ! tee name=t t. ! queue ! lamemp3enc target=1 bitrate=128 cbr=true ! fdsink fd=3 t. ! queue ! lamemp3enc target=1 bitrate=64 cbr=true ! fdsink fd=4 t. ! queue ! audioresample ! audio/x-raw,rate=48000 ! opusenc bitrate=128000 ! rtpopuspay pt=111 ! udpsink host=127.0.0.1 port=5204 udpsrc port=5100 caps="application/x-rtp,media=audio,clock-rate=44100,encoding-name=L16,channels=2,payload=10" ! rtpjitterbuffer latency=80 ! rtpL16depay ! audioconvert ! audioresample ! audio/x-raw,rate=44100,channels=2 ! mix.sink_1 t. ! queue ! audioconvert ! audio/x-raw,format=S16BE,rate=44100,channels=2 ! rtpL16pay pt=10 mtu=1400 ! multiudpsink clients=10.0.0.5:5004,10.0.0.6:5004 sync=true`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.d.buildWebstreamBroadcastPipeline("https://up.example/live", tc.mount, tc.ws, tc.hq, tc.lq, tc.rtpPort)
			if err != nil {
				t.Fatalf("build: %v", err)
			}
			if got != tc.want {
				t.Errorf("pipeline drifted from golden.\n got: %s\nwant: %s", got, tc.want)
			}
		})
	}
}

func TestGolden_BroadcastPipeline(t *testing.T) {
	d := goldenDirector(&config.Config{}, false)

	cases := []struct {
		name  string
		mount models.Mount
		want  string
	}{
		{
			name:  "mp3",
			mount: goldenMountMP3,
			want:  `filesrc location="/media/st/ab/cd/track.mp3" ! decodebin ! audioconvert ! audioresample ! audio/x-raw,rate=44100,channels=2 ! lamemp3enc target=1 bitrate=128 cbr=true ! fdsink fd=1`,
		},
		{
			name:  "aac uses avenc_aac (webstream relays use faac — pinned quirk)",
			mount: goldenMountAAC,
			want:  `filesrc location="/media/st/ab/cd/track.mp3" ! decodebin ! audioconvert ! audioresample ! audio/x-raw,rate=48000,channels=2 ! avenc_aac bitrate=96000 ! fdsink fd=1`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := d.buildBroadcastPipeline("/media/st/ab/cd/track.mp3", tc.mount)
			if err != nil {
				t.Fatalf("build: %v", err)
			}
			if got != tc.want {
				t.Errorf("pipeline drifted from golden.\n got: %s\nwant: %s", got, tc.want)
			}
		})
	}
}

func TestGolden_PCMEncoderPipeline(t *testing.T) {
	plain := goldenDirector(&config.Config{}, false)
	webrtcOn := goldenDirector(&config.Config{}, true)

	got, err := plain.buildPCMEncoderPipeline(goldenMountMP3, 128, 64, 0)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	want := `fdsrc fd=0 ! queue ! audio/x-raw,format=S16LE,rate=44100,channels=2,layout=interleaved ! audioconvert ! audioresample ! audio/x-raw,format=S16LE,rate=44100,channels=2,layout=interleaved ! tee name=t t. ! queue ! lamemp3enc target=1 bitrate=128 cbr=true ! fdsink fd=3 t. ! queue ! lamemp3enc target=1 bitrate=64 cbr=true ! fdsink fd=4`
	if got != want {
		t.Errorf("pcm pipeline drifted from golden.\n got: %s\nwant: %s", got, want)
	}

	got, err = webrtcOn.buildPCMEncoderPipeline(goldenMountMP3, 128, 64, 5204)
	if err != nil {
		t.Fatalf("build webrtc: %v", err)
	}
	want += ` t. ! queue ! audioresample ! audio/x-raw,rate=48000 ! opusenc bitrate=128000 ! rtpopuspay pt=111 ! udpsink host=127.0.0.1 port=5204`
	if got != want {
		t.Errorf("pcm+webrtc pipeline drifted from golden.\n got: %s\nwant: %s", got, want)
	}
}

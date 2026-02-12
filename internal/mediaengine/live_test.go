package mediaengine

import (
	"context"
	"strings"
	"testing"

	pb "github.com/friendsincode/grimnir_radio/proto/mediaengine/v1"
	"github.com/rs/zerolog"
)

func TestRouteLiveWebRTCDefaultBridge(t *testing.T) {
	lim := NewLiveInputManager(zerolog.Nop())

	resp, err := lim.RouteLive(context.Background(), &pb.RouteLiveRequest{
		StationId: "station-a",
		MountId:   "mount-a",
		SessionId: "sess-a",
		InputType: pb.LiveInputType_LIVE_INPUT_TYPE_WEBRTC,
	})
	if err != nil {
		t.Fatalf("RouteLive() error = %v", err)
	}
	if !resp.Success {
		t.Fatalf("expected success response")
	}

	in, ok := lim.GetInput("sess-a")
	if !ok {
		t.Fatalf("expected live input to be tracked")
	}
	if in.SourceURL != "udp://127.0.0.1:5006" {
		t.Fatalf("unexpected SourceURL: %s", in.SourceURL)
	}
	if !strings.Contains(in.Pipeline, "udpsrc address=127.0.0.1 port=5006") {
		t.Fatalf("unexpected pipeline: %s", in.Pipeline)
	}
	if !strings.Contains(in.Pipeline, "rtpopusdepay ! opusdec") {
		t.Fatalf("expected RTP/Opus depay+decode in pipeline: %s", in.Pipeline)
	}
}

func TestRouteLiveWebRTCBridgeFromURLAndPortOverride(t *testing.T) {
	lim := NewLiveInputManager(zerolog.Nop())

	resp, err := lim.RouteLive(context.Background(), &pb.RouteLiveRequest{
		StationId: "station-b",
		MountId:   "mount-b",
		SessionId: "sess-b",
		InputType: pb.LiveInputType_LIVE_INPUT_TYPE_WEBRTC,
		InputUrl:  "udp://10.0.0.8:7000",
		Port:      7100, // explicit grpc port should win over URL port
	})
	if err != nil {
		t.Fatalf("RouteLive() error = %v", err)
	}
	if !resp.Success {
		t.Fatalf("expected success response")
	}

	in, ok := lim.GetInput("sess-b")
	if !ok {
		t.Fatalf("expected live input to be tracked")
	}
	if in.SourceURL != "udp://10.0.0.8:7100" {
		t.Fatalf("unexpected SourceURL: %s", in.SourceURL)
	}
	if !strings.Contains(in.Pipeline, "udpsrc address=10.0.0.8 port=7100") {
		t.Fatalf("unexpected pipeline: %s", in.Pipeline)
	}
}

func TestRouteLiveWebRTCLegacyInputURL(t *testing.T) {
	lim := NewLiveInputManager(zerolog.Nop())

	resp, err := lim.RouteLive(context.Background(), &pb.RouteLiveRequest{
		StationId: "station-c",
		MountId:   "mount-c",
		SessionId: "sess-c",
		InputType: pb.LiveInputType_LIVE_INPUT_TYPE_WEBRTC,
		Input: &pb.LiveInputConfig{
			InputUrl: "udp://192.168.10.4:6200",
		},
	})
	if err != nil {
		t.Fatalf("RouteLive() error = %v", err)
	}
	if !resp.Success {
		t.Fatalf("expected success response")
	}

	in, ok := lim.GetInput("sess-c")
	if !ok {
		t.Fatalf("expected live input to be tracked")
	}
	if in.SourceURL != "udp://192.168.10.4:6200" {
		t.Fatalf("unexpected SourceURL: %s", in.SourceURL)
	}
}

/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package mediaengine

import (
	"context"
	"strings"
	"testing"

	"github.com/rs/zerolog"

	pb "github.com/friendsincode/grimnir_radio/proto/mediaengine/v1"
)

// TestRouteLive_InputTypePipelines guards #85: only the WEBRTC arm of RouteLive
// was tested. ICECAST/RTP/SRT each build a distinct source pipeline (and RTP
// defaults the port to 5004); a wrong element or port ships a broadcaster a
// stream that never reaches air.
func TestRouteLive_InputTypePipelines(t *testing.T) {
	cases := []struct {
		name string
		req  *pb.RouteLiveRequest
		want string
	}{
		{
			"icecast",
			&pb.RouteLiveRequest{SessionId: "s1", InputType: pb.LiveInputType_LIVE_INPUT_TYPE_ICECAST, InputUrl: "http://src:pw@h:8000/m"},
			`souphttpsrc location="http://src:pw@h:8000/m"`,
		},
		{
			"rtp default port",
			&pb.RouteLiveRequest{SessionId: "s2", InputType: pb.LiveInputType_LIVE_INPUT_TYPE_RTP},
			"udpsrc port=5004 ! application/x-rtp",
		},
		{
			"rtp custom port",
			&pb.RouteLiveRequest{SessionId: "s3", InputType: pb.LiveInputType_LIVE_INPUT_TYPE_RTP, Port: 6000},
			"udpsrc port=6000 ! application/x-rtp",
		},
		{
			"srt",
			&pb.RouteLiveRequest{SessionId: "s4", InputType: pb.LiveInputType_LIVE_INPUT_TYPE_SRT, InputUrl: "srt://h:9000"},
			`srtsrc uri="srt://h:9000"`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			lim := NewLiveInputManager(zerolog.Nop())
			resp, err := lim.RouteLive(context.Background(), c.req)
			if err != nil {
				t.Fatalf("RouteLive: %v", err)
			}
			in, ok := lim.GetInput(resp.SessionId)
			if !ok {
				t.Fatal("input not tracked")
			}
			if !strings.Contains(in.Pipeline, c.want) {
				t.Fatalf("pipeline %q does not contain %q", in.Pipeline, c.want)
			}
		})
	}
}

// TestRouteLive_Errors covers the input-validation branches.
func TestRouteLive_Errors(t *testing.T) {
	lim := NewLiveInputManager(zerolog.Nop())
	ctx := context.Background()
	for _, req := range []*pb.RouteLiveRequest{
		{SessionId: "e1", InputType: pb.LiveInputType_LIVE_INPUT_TYPE_ICECAST}, // no input_url
		{SessionId: "e2", InputType: pb.LiveInputType_LIVE_INPUT_TYPE_SRT},     // no input_url
		{SessionId: "e3", InputType: pb.LiveInputType(999)},                    // unknown type
	} {
		if _, err := lim.RouteLive(ctx, req); err == nil {
			t.Errorf("RouteLive(%v) expected an error, got nil", req.InputType)
		}
	}
}

// TestRouteLive_FadeIn covers the fade-in envelope append.
func TestRouteLive_FadeIn(t *testing.T) {
	lim := NewLiveInputManager(zerolog.Nop())
	resp, err := lim.RouteLive(context.Background(), &pb.RouteLiveRequest{
		SessionId: "f1", InputType: pb.LiveInputType_LIVE_INPUT_TYPE_RTP, FadeInMs: 500,
	})
	if err != nil {
		t.Fatalf("RouteLive: %v", err)
	}
	in, _ := lim.GetInput(resp.SessionId)
	if !strings.Contains(in.Pipeline, "volumeenvelope attack=0.500") {
		t.Fatalf("fade-in not applied: %q", in.Pipeline)
	}
}

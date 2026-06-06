/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package grimnirfanout

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

// When Authenticator is set, the ingress demands the offer body include
// {mount, token} & rejects the request with 401 if validation fails.
func TestWebRTCIngress_AuthRequired_Rejects(t *testing.T) {
	srv := &fakeDJAuthServer{
		verdict: func(req ValidateTokenRequest) (*ValidateTokenResponse, error) {
			return nil, status.Error(codes.PermissionDenied, "bad token")
		},
	}
	addr, stop := startFakeDJAuthServer(t, srv)
	defer stop()

	c, err := NewDJAuthClient(DJAuthClientConfig{
		Addr:        addr,
		Timeout:     2 * time.Second,
		MaxTTL:      time.Minute,
		DialOptions: []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())},
	})
	if err != nil {
		t.Fatalf("NewDJAuthClient: %v", err)
	}
	defer c.Close()

	mgr := NewSessionMgr()
	ing, err := NewWebRTCIngress(WebRTCIngressConfig{
		BindAddr:        "127.0.0.1",
		Port:            freePortForWebRTC(t),
		Engines:         []string{"127.0.0.1:65000"},
		SessionMgr:      mgr,
		PipelineBuilder: stubPipelineBuilder,
		Authenticator:   c,
	})
	if err != nil {
		t.Fatalf("NewWebRTCIngress: %v", err)
	}

	hsrv := httptest.NewServer(ing.Handler())
	defer hsrv.Close()

	// Wrapped offer with token; server says "no".
	body, _ := json.Marshal(map[string]any{
		"sdp":   map[string]string{"type": "offer", "sdp": "v=0\r\n"},
		"mount": "/live",
		"token": "bad",
	})
	resp, err := http.Post(hsrv.URL+"/offer", "application/json", bytes.NewBuffer(body))
	if err != nil {
		t.Fatalf("POST /offer: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("rejected offer status = %d, want 401", resp.StatusCode)
	}
	if mgr.CountByProtocol(ProtocolWebRTC) != 0 {
		t.Errorf("CountByProtocol(WebRTC) = %d, want 0 (rejected sessions must not be tracked)",
			mgr.CountByProtocol(ProtocolWebRTC))
	}
}

// When Authenticator is set & no token is in the offer body, the ingress
// rejects with 401 before touching the PeerConnection.
func TestWebRTCIngress_AuthRequired_MissingToken(t *testing.T) {
	fakeSrv := &fakeDJAuthServer{}
	addr, stop := startFakeDJAuthServer(t, fakeSrv)
	defer stop()
	c, err := NewDJAuthClient(DJAuthClientConfig{
		Addr:        addr,
		Timeout:     2 * time.Second,
		MaxTTL:      time.Minute,
		DialOptions: []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())},
	})
	if err != nil {
		t.Fatalf("NewDJAuthClient: %v", err)
	}
	defer c.Close()

	mgr := NewSessionMgr()
	ing, err := NewWebRTCIngress(WebRTCIngressConfig{
		BindAddr:        "127.0.0.1",
		Port:            freePortForWebRTC(t),
		Engines:         []string{"127.0.0.1:65000"},
		SessionMgr:      mgr,
		PipelineBuilder: stubPipelineBuilder,
		Authenticator:   c,
	})
	if err != nil {
		t.Fatalf("NewWebRTCIngress: %v", err)
	}
	hsrv := httptest.NewServer(ing.Handler())
	defer hsrv.Close()

	// Bare offer (no wrapper, no token).
	body, _ := json.Marshal(webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: "v=0\r\n"})
	resp, err := http.Post(hsrv.URL+"/offer", "application/json", bytes.NewBuffer(body))
	if err != nil {
		t.Fatalf("POST /offer: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("missing token status = %d, want 401", resp.StatusCode)
	}
	if fakeSrv.callCount() != 0 {
		t.Errorf("server call count = %d, want 0 (short-circuit before dial)", fakeSrv.callCount())
	}
}

// When Authenticator is nil, the ingress accepts unauth offers (backward
// compatibility — single-node / dev mode).
func TestWebRTCIngress_NoAuth_StillAccepts(t *testing.T) {
	mgr := NewSessionMgr()
	ing, err := NewWebRTCIngress(WebRTCIngressConfig{
		BindAddr:        "127.0.0.1",
		Port:            freePortForWebRTC(t),
		Engines:         []string{"127.0.0.1:65000"},
		SessionMgr:      mgr,
		PipelineBuilder: stubPipelineBuilder,
		// Authenticator deliberately nil.
	})
	if err != nil {
		t.Fatalf("NewWebRTCIngress: %v", err)
	}

	hsrv := httptest.NewServer(ing.Handler())
	defer hsrv.Close()
	defer ing.Shutdown(context.Background())

	body, _ := json.Marshal(webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: "v=0\r\nm=audio 9 UDP/TLS/RTP/SAVPF 111\r\n"})
	resp, err := http.Post(hsrv.URL+"/offer", "application/json", bytes.NewBuffer(body))
	if err != nil {
		t.Fatalf("POST /offer: %v", err)
	}
	defer resp.Body.Close()
	// SDP is malformed enough that pion returns 400, NOT 401 — confirms the
	// auth gate is skipped.
	if resp.StatusCode == http.StatusUnauthorized {
		t.Error("status = 401 even though Authenticator is nil")
	}
}

/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package grimnirfanout

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"
)

// freePortForWebRTC grabs an OS-assigned TCP port so parallel tests don't
// collide. Named so it can't clash with any sibling ingress test helper.
func freePortForWebRTC(t *testing.T) int {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := lis.Addr().(*net.TCPAddr).Port
	_ = lis.Close()
	return port
}

func TestWebRTCIngress_RejectsMissingSessionMgr(t *testing.T) {
	_, err := NewWebRTCIngress(WebRTCIngressConfig{
		BindAddr: "127.0.0.1",
		Port:     freePortForWebRTC(t),
		Engines:  []string{"127.0.0.1:65000"},
	})
	if err == nil {
		t.Fatal("NewWebRTCIngress without SessionMgr: want error, got nil")
	}
	if !strings.Contains(err.Error(), "SessionMgr") {
		t.Errorf("error %q should mention SessionMgr", err.Error())
	}
}

func TestWebRTCIngress_RejectsEmptyEngines(t *testing.T) {
	mgr := NewSessionMgr()
	_, err := NewWebRTCIngress(WebRTCIngressConfig{
		BindAddr:   "127.0.0.1",
		Port:       freePortForWebRTC(t),
		SessionMgr: mgr,
	})
	if err == nil {
		t.Fatal("NewWebRTCIngress without engines: want error, got nil")
	}
	if !strings.Contains(err.Error(), "engine") {
		t.Errorf("error %q should mention engine", err.Error())
	}
}

func TestWebRTCIngress_OfferRequiresPOST(t *testing.T) {
	mgr := NewSessionMgr()
	ing, err := NewWebRTCIngress(WebRTCIngressConfig{
		BindAddr:   "127.0.0.1",
		Port:       freePortForWebRTC(t),
		Engines:    []string{"127.0.0.1:65000"},
		SessionMgr: mgr,
	})
	if err != nil {
		t.Fatalf("NewWebRTCIngress: %v", err)
	}

	srv := httptest.NewServer(ing.Handler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/offer")
	if err != nil {
		t.Fatalf("GET /offer: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("GET /offer status = %d, want 405", resp.StatusCode)
	}
}

func TestWebRTCIngress_OfferRejectsInvalidJSON(t *testing.T) {
	mgr := NewSessionMgr()
	ing, err := NewWebRTCIngress(WebRTCIngressConfig{
		BindAddr:   "127.0.0.1",
		Port:       freePortForWebRTC(t),
		Engines:    []string{"127.0.0.1:65000"},
		SessionMgr: mgr,
	})
	if err != nil {
		t.Fatalf("NewWebRTCIngress: %v", err)
	}

	srv := httptest.NewServer(ing.Handler())
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/offer", "application/json", bytes.NewBufferString("{not json"))
	if err != nil {
		t.Fatalf("POST /offer: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("invalid JSON status = %d, want 400", resp.StatusCode)
	}
}

func TestWebRTCIngress_OfferRejectsWrongSDPType(t *testing.T) {
	mgr := NewSessionMgr()
	ing, err := NewWebRTCIngress(WebRTCIngressConfig{
		BindAddr:   "127.0.0.1",
		Port:       freePortForWebRTC(t),
		Engines:    []string{"127.0.0.1:65000"},
		SessionMgr: mgr,
	})
	if err != nil {
		t.Fatalf("NewWebRTCIngress: %v", err)
	}

	srv := httptest.NewServer(ing.Handler())
	defer srv.Close()

	// An "answer" sent to /offer is a protocol error.
	body, _ := json.Marshal(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: "v=0\r\n"})
	resp, err := http.Post(srv.URL+"/offer", "application/json", bytes.NewBuffer(body))
	if err != nil {
		t.Fatalf("POST /offer: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("wrong SDP type status = %d, want 400", resp.StatusCode)
	}
}

// stubPipelineBuilder returns a Pipeline that uses a finite audiotestsrc so
// the test doesn't depend on the decoder subprocess (which isn't exercised
// without real Opus RTP flowing). PipelineBuilder injection keeps the test
// runnable on CI hosts without a working gst-launch in PATH.
func stubPipelineBuilder(cfg PipelineConfig) (*Pipeline, error) {
	cfg.SourceLaunch = "audiotestsrc num-buffers=1 samplesperbuffer=480"
	return NewPipeline(cfg)
}

// TestWebRTCIngress_OfferAnswerRoundTrip uses a real pion PeerConnection on
// the client side to generate a valid Opus-audio offer, POSTs it to the
// ingress, parses the answer, and verifies the signaling layer is wired end
// to end. The pipeline target is a black-hole UDP port; multiudpsink drops
// silently.
func TestWebRTCIngress_OfferAnswerRoundTrip(t *testing.T) {
	gstInit()
	mgr := NewSessionMgr()
	ing, err := NewWebRTCIngress(WebRTCIngressConfig{
		BindAddr:        "127.0.0.1",
		Port:            freePortForWebRTC(t),
		Engines:         []string{"127.0.0.1:65000"},
		SessionMgr:      mgr,
		PipelineBuilder: stubPipelineBuilder,
	})
	if err != nil {
		t.Fatalf("NewWebRTCIngress: %v", err)
	}

	srv := httptest.NewServer(ing.Handler())
	defer srv.Close()
	defer ing.Shutdown(context.Background())

	// Client side: build a peer connection that wants to SEND audio.
	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatalf("client NewPeerConnection: %v", err)
	}
	defer pc.Close()

	track, err := webrtc.NewTrackLocalStaticRTP(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus},
		"audio", "djconsole",
	)
	if err != nil {
		t.Fatalf("NewTrackLocalStaticRTP: %v", err)
	}
	if _, err := pc.AddTrack(track); err != nil {
		t.Fatalf("AddTrack: %v", err)
	}

	offer, err := pc.CreateOffer(nil)
	if err != nil {
		t.Fatalf("CreateOffer: %v", err)
	}
	if err := pc.SetLocalDescription(offer); err != nil {
		t.Fatalf("SetLocalDescription: %v", err)
	}
	<-webrtc.GatheringCompletePromise(pc)

	body, _ := json.Marshal(pc.LocalDescription())
	resp, err := http.Post(srv.URL+"/offer", "application/json", bytes.NewBuffer(body))
	if err != nil {
		t.Fatalf("POST /offer: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("POST /offer status=%d body=%s", resp.StatusCode, string(raw))
	}

	var answer webrtc.SessionDescription
	if err := json.NewDecoder(resp.Body).Decode(&answer); err != nil {
		t.Fatalf("decode answer: %v", err)
	}
	if answer.Type != webrtc.SDPTypeAnswer {
		t.Errorf("answer.Type = %v, want answer", answer.Type)
	}
	if !strings.Contains(strings.ToLower(answer.SDP), "opus") {
		t.Errorf("answer SDP missing opus codec line:\n%s", answer.SDP)
	}

	// Apply the answer client-side; this verifies the ingress emitted a
	// syntactically valid SDP.
	if err := pc.SetRemoteDescription(answer); err != nil {
		t.Fatalf("SetRemoteDescription(answer): %v", err)
	}

	// A WebRTC session should now exist in the manager.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if mgr.CountByProtocol(ProtocolWebRTC) >= 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got := mgr.CountByProtocol(ProtocolWebRTC); got < 1 {
		t.Errorf("CountByProtocol(WebRTC) = %d, want >= 1", got)
	}
}

func TestWebRTCIngress_ListenAndShutdown(t *testing.T) {
	mgr := NewSessionMgr()
	port := freePortForWebRTC(t)
	ing, err := NewWebRTCIngress(WebRTCIngressConfig{
		BindAddr:        "127.0.0.1",
		Port:            port,
		Engines:         []string{"127.0.0.1:65000"},
		SessionMgr:      mgr,
		PipelineBuilder: stubPipelineBuilder,
	})
	if err != nil {
		t.Fatalf("NewWebRTCIngress: %v", err)
	}

	errCh := make(chan error, 1)
	go func() { errCh <- ing.ListenAndServe() }()

	// Wait until the server is responsive on /offer.
	url := fmt.Sprintf("http://127.0.0.1:%d/offer", port)
	deadline := time.Now().Add(2 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			resp.Body.Close()
			lastErr = nil
			break
		}
		lastErr = err
		time.Sleep(25 * time.Millisecond)
	}
	if lastErr != nil {
		t.Fatalf("server never came up: %v", lastErr)
	}

	if err := ing.Shutdown(context.Background()); err != nil {
		t.Errorf("Shutdown: %v", err)
	}

	select {
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			t.Errorf("ListenAndServe returned: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("ListenAndServe did not return after Shutdown")
	}
}

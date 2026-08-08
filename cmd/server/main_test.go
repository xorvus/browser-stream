package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"browser-stream/internal/config"
	"browser-stream/internal/stream"

	"github.com/pion/webrtc/v4"
)

func TestHealthzReportsServerReadiness(t *testing.T) {
	handler := newServer(config.Config{}, stream.NewBroadcaster(config.Config{}), http.NotFoundHandler())
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("got status %d, want %d", recorder.Code, http.StatusOK)
	}
	if got := recorder.Body.String(); got != "ok\n" {
		t.Fatalf("got body %q, want %q", got, "ok\n")
	}
}

func TestClipboardReturnsSelectedText(t *testing.T) {
	handler := newServerWithClipboard(
		config.Config{}, stream.NewBroadcaster(config.Config{}), http.NotFoundHandler(),
		func(context.Context) (string, error) { return "copied text", nil },
	)
	request := httptest.NewRequest(http.MethodGet, "/clipboard", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("got status %d, want %d: %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var got struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Text != "copied text" {
		t.Fatalf("text = %q, want copied text", got.Text)
	}
}

func TestQualityUpdatesSharedVideoProfile(t *testing.T) {
	cfg := config.Config{Profile: config.VideoProfile1080p60}
	broadcaster := stream.NewBroadcaster(cfg)
	handler := newServerWithResize(cfg, broadcaster, http.NotFoundHandler(), func(context.Context, int, int) error { return nil })
	request := httptest.NewRequest(http.MethodPost, "/quality", strings.NewReader(`{"profile":"720p30"}`))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("got status %d, want %d: %s", recorder.Code, http.StatusNoContent, recorder.Body.String())
	}
	if got := broadcaster.Profile(); got != config.VideoProfile720p30 {
		t.Fatalf("profile = %q, want %q", got, config.VideoProfile720p30)
	}
}

func TestQualityRejectsUnknownProfile(t *testing.T) {
	cfg := config.Config{Profile: config.VideoProfile1080p60}
	handler := newServerWithResize(cfg, stream.NewBroadcaster(cfg), http.NotFoundHandler(), func(context.Context, int, int) error { return nil })
	request := httptest.NewRequest(http.MethodPost, "/quality", strings.NewReader(`{"profile":"4k120"}`))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("got status %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}

func TestQualityRejectsOversizedRequestBody(t *testing.T) {
	cfg := config.Config{Profile: config.VideoProfile1080p60}
	var resizeCalls atomic.Int32
	handler := newServerWithResize(cfg, stream.NewBroadcaster(cfg), http.NotFoundHandler(), func(context.Context, int, int) error {
		resizeCalls.Add(1)
		return nil
	})
	body := `{"profile":"720p30"}` + strings.Repeat(" ", 65*1024)
	request := httptest.NewRequest(http.MethodPost, "/quality", strings.NewReader(body))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("got status %d, want %d", recorder.Code, http.StatusBadRequest)
	}
	if resizeCalls.Load() != 0 {
		t.Fatalf("resize calls = %d, want 0", resizeCalls.Load())
	}
}

func TestStatsReportsProfileAndBroadcasters(t *testing.T) {
	cfg := config.Config{Profile: config.VideoProfile1080p30}
	handler := newServer(cfg, stream.NewBroadcaster(cfg), http.NotFoundHandler())
	request := httptest.NewRequest(http.MethodGet, "/stats", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("got status %d, want %d", recorder.Code, http.StatusOK)
	}
	var got struct {
		Profile string                  `json:"profile"`
		Video   stream.BroadcasterStats `json:"video"`
		Audio   stream.BroadcasterStats `json:"audio"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Profile != "1080p30" || got.Video.Subscribers != 0 || got.Audio.Subscribers != 0 {
		t.Fatalf("unexpected stats: %#v", got)
	}
}

func TestStatsDistinguishesCaptureAndStreamDimensions(t *testing.T) {
	cfg := config.Config{Width: 1920, Height: 1080, FPS: 60, Profile: config.VideoProfile720p60}
	handler := newServer(cfg, stream.NewBroadcaster(cfg), http.NotFoundHandler())
	request := httptest.NewRequest(http.MethodGet, "/stats", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	var got struct {
		CaptureWidth  int `json:"captureWidth"`
		CaptureHeight int `json:"captureHeight"`
		StreamWidth   int `json:"streamWidth"`
		StreamHeight  int `json:"streamHeight"`
		StreamFPS     int `json:"streamFPS"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.CaptureWidth != 1280 || got.CaptureHeight != 720 {
		t.Fatalf("capture dimensions = %dx%d, want 1280x720", got.CaptureWidth, got.CaptureHeight)
	}
	if got.StreamWidth != 1280 || got.StreamHeight != 720 || got.StreamFPS != 60 {
		t.Fatalf("stream = %dx%d@%d, want 1280x720@60", got.StreamWidth, got.StreamHeight, got.StreamFPS)
	}
}

func TestDisconnectGraceAllowsRecovery(t *testing.T) {
	var cleanups atomic.Int32
	grace := newDisconnectGrace(20*time.Millisecond, func() { cleanups.Add(1) })
	grace.Update(webrtc.PeerConnectionStateDisconnected)
	time.Sleep(5 * time.Millisecond)
	grace.Update(webrtc.PeerConnectionStateConnected)
	time.Sleep(30 * time.Millisecond)
	if got := cleanups.Load(); got != 0 {
		t.Fatalf("cleanup count = %d, want 0", got)
	}
}

func TestDisconnectGraceCleansUpExpiredDisconnect(t *testing.T) {
	cleaned := make(chan struct{}, 1)
	grace := newDisconnectGrace(10*time.Millisecond, func() { cleaned <- struct{}{} })
	grace.Update(webrtc.PeerConnectionStateDisconnected)
	select {
	case <-cleaned:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("disconnected peer was not cleaned up after grace period")
	}
}

func TestOfferRejectsMalformedJSON(t *testing.T) {
	handler := newServer(config.Config{}, stream.NewBroadcaster(config.Config{}), http.NotFoundHandler())
	request := httptest.NewRequest(http.MethodPost, "/offer", strings.NewReader("not json"))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("got status %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}

func TestWaitForGatheringStopsWhenContextExpires(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	err := waitForGathering(ctx, make(chan struct{}))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got error %v, want deadline exceeded", err)
	}
}

func TestOfferAnswerIncludesOpusAudio(t *testing.T) {
	cfg := config.Config{ICEHost: "127.0.0.1", UDPPortMin: 60000, UDPPortMax: 60100}
	handler := newServer(cfg, stream.NewBroadcaster(cfg), http.NotFoundHandler())
	viewer, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	defer viewer.Close()
	if _, err = viewer.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo, webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly}); err != nil {
		t.Fatal(err)
	}
	if _, err = viewer.AddTransceiverFromKind(webrtc.RTPCodecTypeAudio, webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly}); err != nil {
		t.Fatal(err)
	}
	offer, err := viewer.CreateOffer(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := viewer.SetLocalDescription(offer); err != nil {
		t.Fatal(err)
	}

	body, err := json.Marshal(offer)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/offer", bytes.NewReader(body))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("got status %d, want %d: %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var answer webrtc.SessionDescription
	if err := json.NewDecoder(recorder.Body).Decode(&answer); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(answer.SDP, "m=audio") || !strings.Contains(answer.SDP, "opus/48000/2") {
		t.Fatalf("expected Opus audio in answer SDP, got %q", answer.SDP)
	}
	audioSection := strings.SplitN(answer.SDP, "m=audio", 2)[1]
	if !strings.Contains(audioSection, "a=sendonly") {
		t.Fatalf("expected server to send audio, got %q", audioSection)
	}
}

func TestPeerConnectionAdvertisesConfiguredDockerICEAddressAndPortRange(t *testing.T) {
	pc, err := newPeerConnection(config.Config{
		ICEHost: "127.0.0.1", UDPPortMin: 60000, UDPPortMax: 60100,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()

	if _, err := pc.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo); err != nil {
		t.Fatal(err)
	}
	offer, err := pc.CreateOffer(nil)
	if err != nil {
		t.Fatal(err)
	}
	gathered := webrtc.GatheringCompletePromise(pc)
	if err := pc.SetLocalDescription(offer); err != nil {
		t.Fatal(err)
	}
	<-gathered

	match := regexp.MustCompile(`candidate:[^ ]+ 1 udp [0-9]+ 127\.0\.0\.1 ([0-9]+)`).FindStringSubmatch(pc.LocalDescription().SDP)
	if len(match) != 2 {
		t.Fatalf("expected an ICE candidate for 127.0.0.1, got %q", pc.LocalDescription().SDP)
	}
	port, err := strconv.Atoi(match[1])
	if err != nil {
		t.Fatal(err)
	}
	if port < 60000 || port > 60100 {
		t.Fatalf("got candidate port %d outside 60000-60100", port)
	}
}

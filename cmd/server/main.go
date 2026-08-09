package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"browser-stream/internal/browser"
	"browser-stream/internal/config"
	"browser-stream/internal/stream"

	"github.com/pion/webrtc/v4"
)

const (
	maxJSONBodyBytes    = 64 * 1024
	iceGatheringTimeout = 10 * time.Second
)

type server struct {
	config       config.Config
	broadcaster  *stream.Broadcaster
	audio        *stream.AudioBroadcaster
	input        *browser.Dispatcher
	lifecycle    viewerLifecyclePort
	resize       func(context.Context, int, int) error
	selectedText func(context.Context) (string, error)
}

func main() {
	cfg, err := config.FromEnv(os.Getenv)
	if err != nil {
		log.Fatal(err)
	}

	lifecycle := newViewerLifecycle(browser.SetPageLifecycleState)
	handler := newServerWithLifecycle(cfg, stream.NewBroadcaster(cfg), http.FileServer(http.Dir("./web")), lifecycle)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := lifecycle.freezeIfIdle(ctx); err != nil {
			log.Printf("pause browser page while idle: %v", err)
		}
	}()
	srv := &http.Server{
		Addr:              ":8080",
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	log.Printf("server listening on http://localhost:8080")
	if err := srv.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}

func newServer(cfg config.Config, broadcaster *stream.Broadcaster, static http.Handler) http.Handler {
	return newServerWithDependencies(cfg, broadcaster, static, browser.ResizeViewport, browser.SelectedText)
}

func newServerWithResize(cfg config.Config, broadcaster *stream.Broadcaster, static http.Handler, resize func(context.Context, int, int) error) http.Handler {
	return newServerWithDependencies(cfg, broadcaster, static, resize, browser.SelectedText)
}

func newServerWithClipboard(cfg config.Config, broadcaster *stream.Broadcaster, static http.Handler, selectedText func(context.Context) (string, error)) http.Handler {
	return newServerWithDependencies(cfg, broadcaster, static, browser.ResizeViewport, selectedText)
}

func newServerWithDependencies(cfg config.Config, broadcaster *stream.Broadcaster, static http.Handler, resize func(context.Context, int, int) error, selectedText func(context.Context) (string, error)) http.Handler {
	return newServerWithLifecycleAndDependencies(cfg, broadcaster, static, resize, selectedText, noopViewerLifecycle{})
}

func newServerWithLifecycle(cfg config.Config, broadcaster *stream.Broadcaster, static http.Handler, lifecycle viewerLifecyclePort) http.Handler {
	return newServerWithLifecycleAndDependencies(cfg, broadcaster, static, browser.ResizeViewport, browser.SelectedText, lifecycle)
}

func newServerWithLifecycleAndDependencies(cfg config.Config, broadcaster *stream.Broadcaster, static http.Handler, resize func(context.Context, int, int) error, selectedText func(context.Context) (string, error), lifecycle viewerLifecyclePort) http.Handler {
	s := &server{config: cfg, broadcaster: broadcaster, audio: stream.NewAudioBroadcaster(cfg), input: browser.NewInputDispatcher(), lifecycle: lifecycle, resize: resize, selectedText: selectedText}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /stats", s.handleStats)
	mux.HandleFunc("POST /offer", s.handleOffer)
	mux.HandleFunc("POST /browser-url", s.handleBrowserURL)
	mux.HandleFunc("GET /clipboard", s.handleClipboard)
	mux.HandleFunc("POST /input", s.handleInput)
	mux.Handle("GET /input/ws", newInputWebSocketHandler(cfg.Width, cfg.Height, s.input.Dispatch))
	mux.HandleFunc("POST /quality", s.handleQuality)
	mux.Handle("/", noCacheStatic(static))
	return mux
}

func noCacheStatic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func (s *server) handleClipboard(w http.ResponseWriter, r *http.Request) {
	text, err := s.selectedText(r.Context())
	if err != nil {
		log.Printf("read selected browser text: %v", err)
		http.Error(w, "could not read selected browser text", http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"text": text})
}

func (s *server) handleInput(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var input browser.Input
	if err := decodeJSON(w, r, &input); err != nil {
		http.Error(w, "invalid input event", http.StatusBadRequest)
		return
	}
	if err := s.input.Dispatch(r.Context(), input); err != nil {
		http.Error(w, "could not send browser input", http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok\n"))
}

func (s *server) handleStats(w http.ResponseWriter, _ *http.Request) {
	runtimeConfig := s.config
	runtimeConfig.Profile = s.broadcaster.Profile()
	streamWidth, streamHeight, streamFPS := runtimeConfig.VideoOutput()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(struct {
		Profile       config.VideoProfile     `json:"profile"`
		CaptureWidth  int                     `json:"captureWidth"`
		CaptureHeight int                     `json:"captureHeight"`
		StreamWidth   int                     `json:"streamWidth"`
		StreamHeight  int                     `json:"streamHeight"`
		StreamFPS     int                     `json:"streamFPS"`
		Video         stream.BroadcasterStats `json:"video"`
		Audio         stream.BroadcasterStats `json:"audio"`
	}{
		Profile:       runtimeConfig.Profile,
		CaptureWidth:  streamWidth,
		CaptureHeight: streamHeight,
		StreamWidth:   streamWidth,
		StreamHeight:  streamHeight,
		StreamFPS:     streamFPS,
		Video:         s.broadcaster.Stats(),
		Audio:         s.audio.Stats(),
	})
}

func (s *server) handleQuality(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var request struct {
		Profile string `json:"profile"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		http.Error(w, "invalid quality request", http.StatusBadRequest)
		return
	}
	profile, err := config.ParseVideoProfile(request.Profile)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	runtimeConfig := s.config
	runtimeConfig.Profile = profile
	width, height, _ := runtimeConfig.VideoOutput()
	if err := s.resize(r.Context(), width, height); err != nil {
		http.Error(w, "could not resize browser viewport", http.StatusBadGateway)
		return
	}
	s.broadcaster.SetProfile(profile)
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) handleOffer(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	var offer webrtc.SessionDescription
	if err := decodeJSON(w, r, &offer); err != nil {
		http.Error(w, "invalid WebRTC offer", http.StatusBadRequest)
		return
	}
	if offer.Type != webrtc.SDPTypeOffer || offer.SDP == "" {
		http.Error(w, "invalid WebRTC offer", http.StatusBadRequest)
		return
	}

	peerConnection, err := newPeerConnection(s.config)
	if err != nil {
		log.Printf("create peer connection: %v", err)
		http.Error(w, "could not create WebRTC session", http.StatusInternalServerError)
		return
	}

	var cleanupOnce sync.Once
	var unsubMu sync.Mutex
	var unsubscribeVideo func()
	var unsubscribeAudio func()
	cleanup := func() {
		cleanupOnce.Do(func() {
			_ = peerConnection.Close()
			unsubMu.Lock()
			unsV, unsA := unsubscribeVideo, unsubscribeAudio
			unsubMu.Unlock()
			if unsV != nil {
				unsV()
			}
			if unsA != nil {
				unsA()
			}
		})
	}

	videoTrack, err := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8, ClockRate: 90000},
		"video",
		"browser-stream",
	)
	if err != nil {
		cleanup()
		log.Printf("create video track: %v", err)
		http.Error(w, "could not create video stream", http.StatusInternalServerError)
		return
	}

	sender, err := peerConnection.AddTrack(videoTrack)
	if err != nil {
		cleanup()
		log.Printf("add video track: %v", err)
		http.Error(w, "could not create video stream", http.StatusInternalServerError)
		return
	}
	go drainRTCP(sender)

	audioTrack, err := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2},
		"audio",
		"browser-stream",
	)
	if err != nil {
		cleanup()
		log.Printf("create audio track: %v", err)
		http.Error(w, "could not create audio stream", http.StatusInternalServerError)
		return
	}
	audioSender, err := peerConnection.AddTrack(audioTrack)
	if err != nil {
		cleanup()
		log.Printf("add audio track: %v", err)
		http.Error(w, "could not create audio stream", http.StatusInternalServerError)
		return
	}
	go drainRTCP(audioSender)

	grace := newDisconnectGrace(5*time.Second, cleanup)
	peerConnection.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		log.Printf("peer connection state: %s", state)
		grace.Update(state)
	})
	if err := peerConnection.SetRemoteDescription(offer); err != nil {
		cleanup()
		log.Printf("set remote description: %v", err)
		http.Error(w, "invalid WebRTC offer", http.StatusBadRequest)
		return
	}

	answer, err := peerConnection.CreateAnswer(nil)
	if err != nil {
		cleanup()
		log.Printf("create answer: %v", err)
		http.Error(w, "could not create WebRTC answer", http.StatusInternalServerError)
		return
	}

	gatherComplete := webrtc.GatheringCompletePromise(peerConnection)
	if err := peerConnection.SetLocalDescription(answer); err != nil {
		cleanup()
		log.Printf("set local description: %v", err)
		http.Error(w, "could not create WebRTC answer", http.StatusInternalServerError)
		return
	}
	gatherContext, cancelGathering := context.WithTimeout(r.Context(), iceGatheringTimeout)
	defer cancelGathering()
	if err := waitForGathering(gatherContext, gatherComplete); err != nil {
		cleanup()
		log.Printf("gather ICE candidates: %v", err)
		http.Error(w, "could not gather WebRTC candidates", http.StatusGatewayTimeout)
		return
	}
	if err := s.lifecycle.connect(r.Context()); err != nil {
		cleanup()
		log.Printf("resume browser page: %v", err)
		http.Error(w, "could not resume browser page", http.StatusBadGateway)
		return
	}
	viewerConnected := true
	unsubMu.Lock()
	videoUnsubscribe := s.broadcaster.Subscribe(videoTrack)
	unsubscribeVideo = func() {
		videoUnsubscribe()
		if !viewerConnected {
			return
		}
		viewerConnected = false
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s.lifecycle.disconnect(ctx)
	}
	unsubscribeAudio = s.audio.Subscribe(audioTrack)
	unsubMu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(peerConnection.LocalDescription()); err != nil {
		log.Printf("encode WebRTC answer: %v", err)
		cleanup()
	}
}

func waitForGathering(ctx context.Context, complete <-chan struct{}) error {
	select {
	case <-complete:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *server) handleBrowserURL(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var request struct {
		URL string `json:"url"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		http.Error(w, "invalid URL request", http.StatusBadRequest)
		return
	}
	location, err := browser.Navigate(r.Context(), request.URL)
	if err != nil {
		if len(request.URL) == 0 {
			http.Error(w, "invalid browser URL", http.StatusBadRequest)
		} else {
			http.Error(w, "could not navigate browser", http.StatusBadGateway)
		}
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"url": location})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, value any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("request body must contain one JSON value")
		}
		return err
	}
	return nil
}

type disconnectGrace struct {
	mu       sync.Mutex
	duration time.Duration
	cleanup  func()
	timer    *time.Timer
	done     bool
}

func newDisconnectGrace(duration time.Duration, cleanup func()) *disconnectGrace {
	return &disconnectGrace{duration: duration, cleanup: cleanup}
}

func (g *disconnectGrace) Update(state webrtc.PeerConnectionState) {
	g.mu.Lock()
	if g.done {
		g.mu.Unlock()
		return
	}
	switch state {
	case webrtc.PeerConnectionStateConnected:
		if g.timer != nil {
			g.timer.Stop()
			g.timer = nil
		}
		g.mu.Unlock()
	case webrtc.PeerConnectionStateDisconnected:
		if g.timer == nil {
			g.timer = time.AfterFunc(g.duration, g.expire)
		}
		g.mu.Unlock()
	case webrtc.PeerConnectionStateFailed, webrtc.PeerConnectionStateClosed:
		g.done = true
		if g.timer != nil {
			g.timer.Stop()
			g.timer = nil
		}
		cleanup := g.cleanup
		g.mu.Unlock()
		cleanup()
	default:
		g.mu.Unlock()
	}
}

func (g *disconnectGrace) expire() {
	g.mu.Lock()
	if g.done {
		g.mu.Unlock()
		return
	}
	g.done = true
	g.timer = nil
	cleanup := g.cleanup
	g.mu.Unlock()
	cleanup()
}

func newPeerConnection(cfg config.Config) (*webrtc.PeerConnection, error) {
	settingEngine := webrtc.SettingEngine{}
	if err := settingEngine.SetEphemeralUDPPortRange(uint16(cfg.UDPPortMin), uint16(cfg.UDPPortMax)); err != nil {
		return nil, err
	}
	iceAddresses, err := cfg.ICEAddresses()
	if err != nil {
		return nil, err
	}
	if err := settingEngine.SetICEAddressRewriteRules(webrtc.ICEAddressRewriteRule{
		External:        iceAddresses,
		AsCandidateType: webrtc.ICECandidateTypeHost,
		Mode:            webrtc.ICEAddressRewriteReplace,
	}); err != nil {
		return nil, err
	}
	return webrtc.NewAPI(webrtc.WithSettingEngine(settingEngine)).NewPeerConnection(webrtc.Configuration{})
}

func drainRTCP(sender *webrtc.RTPSender) {
	buffer := make([]byte, 1500)
	for {
		if _, _, err := sender.Read(buffer); err != nil {
			return
		}
	}
}

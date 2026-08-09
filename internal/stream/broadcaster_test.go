package stream

import (
	"context"
	"io"
	"testing"
	"time"

	"browser-stream/internal/config"

	"github.com/pion/webrtc/v4"
)

func TestRetryDelayIsBoundedExponential(t *testing.T) {
	want := []time.Duration{100 * time.Millisecond, 200 * time.Millisecond, 400 * time.Millisecond, 800 * time.Millisecond, time.Second, time.Second}
	for attempt, expected := range want {
		if got := retryDelay(attempt); got != expected {
			t.Fatalf("retryDelay(%d) = %s, want %s", attempt, got, expected)
		}
	}
}

func TestBroadcasterKeepsOnePipelinePerProfileAndCodec(t *testing.T) {
	b := newTestBroadcaster(t, config.Config{Profile: config.VideoProfile720p60})

	full := b.Subscribe(&webrtc.TrackLocalStaticSample{}, config.StreamFull, config.CodecVP8)
	saver := b.Subscribe(&webrtc.TrackLocalStaticSample{}, config.StreamSaver360, config.CodecVP9)
	fallback := b.Subscribe(&webrtc.TrackLocalStaticSample{}, config.StreamSaver360, config.CodecVP8)
	shared := b.Subscribe(&webrtc.TrackLocalStaticSample{}, config.StreamFull, config.CodecVP8)
	defer func() {
		full()
		saver()
		fallback()
		shared()
	}()

	b.mu.Lock()
	pipelines := len(b.pipelines)
	b.mu.Unlock()
	if pipelines != 3 {
		t.Fatalf("pipelines = %d, want 3 (full/vp8, saver360/vp9, saver360/vp8)", pipelines)
	}
}

func TestBroadcasterDropsPipelineWhenLastViewerLeaves(t *testing.T) {
	b := newTestBroadcaster(t, config.Config{Profile: config.VideoProfile720p60})

	unsubscribe := b.Subscribe(&webrtc.TrackLocalStaticSample{}, config.StreamSaver360, config.CodecVP9)
	unsubscribe()

	eventually(t, func() bool {
		b.mu.Lock()
		defer b.mu.Unlock()
		return len(b.pipelines) == 0
	}, "the saver pipeline was still registered after its last viewer left")
}

func TestBroadcasterProfileChangeRestartsEveryPipeline(t *testing.T) {
	b := newTestBroadcaster(t, config.Config{Profile: config.VideoProfile1080p60})
	unsubscribe := b.Subscribe(&webrtc.TrackLocalStaticSample{}, config.StreamFull, config.CodecVP8)
	defer unsubscribe()

	b.mu.Lock()
	entry := b.pipelines[deliveryKey{profile: config.StreamFull, codec: config.CodecVP8}]
	b.mu.Unlock()
	if entry == nil {
		t.Fatal("expected a running pipeline for the full profile")
	}
	eventually(t, func() bool { return entry.captureStarts.Load() >= 1 }, "the pipeline never started")
	before := entry.captureStarts.Load()

	if !b.SetProfile(config.VideoProfile720p60) || b.Profile() != config.VideoProfile720p60 {
		t.Fatalf("profile change was not retained, got %q", b.Profile())
	}
	eventually(t, func() bool { return entry.captureStarts.Load() > before },
		"the encoder was not restarted after the shared capture profile changed")
}

func TestBroadcasterStatsAggregatesEveryPipeline(t *testing.T) {
	b := newTestBroadcaster(t, config.Config{Profile: config.VideoProfile1080p30})
	unsubscribe := b.Subscribe(&webrtc.TrackLocalStaticSample{}, config.StreamSaver360, config.CodecVP9)
	defer unsubscribe()

	b.mu.Lock()
	entry := b.pipelines[deliveryKey{profile: config.StreamSaver360, codec: config.CodecVP9}]
	b.mu.Unlock()
	entry.samples.Store(120)
	entry.writeFailures.Store(2)

	got := b.Stats()
	if got.Subscribers != 1 || got.Samples != 120 || got.WriteFailures != 2 {
		t.Fatalf("unexpected stats: %#v", got)
	}
	if len(got.Pipelines) != 1 || got.Pipelines[0].Profile != "saver360" {
		t.Fatalf("expected per-pipeline breakdown, got %#v", got.Pipelines)
	}
	if got.Pipelines[0].VideoKbps != 70 || got.Pipelines[0].Height != 360 || got.Pipelines[0].FPS != 15 {
		t.Fatalf("saver pipeline was not reported at its configured cost: %#v", got.Pipelines[0])
	}
}

func TestIsKeyframeReadsVP8AndVP9Headers(t *testing.T) {
	tests := []struct {
		name  string
		codec config.VideoCodec
		frame []byte
		want  bool
	}{
		{"vp8 keyframe", config.CodecVP8, []byte{0x00, 0x00, 0x00}, true},
		{"vp8 inter frame", config.CodecVP8, []byte{0x01, 0x00, 0x00}, false},
		{"vp9 keyframe", config.CodecVP9, []byte{0x82, 0x49, 0x83}, true},
		{"vp9 inter frame", config.CodecVP9, []byte{0x86, 0x00, 0x00}, false},
		{"vp9 show existing frame", config.CodecVP9, []byte{0x88, 0x00, 0x00}, false},
		{"empty frame fails open", config.CodecVP9, nil, true},
		{"unrecognised header fails open", config.CodecVP9, []byte{0x00}, true},
	}
	for _, test := range tests {
		if got := isKeyframe(test.codec, test.frame); got != test.want {
			t.Fatalf("%s: isKeyframe = %v, want %v", test.name, got, test.want)
		}
	}
}

func TestVideoDecoderDerivesDurationFromIVFTimestamps(t *testing.T) {
	// ffmpeg drops frames under load, so a fixed 1/FPS duration makes RTP
	// timestamps drift away from wall clock. Durations must follow the
	// container timestamps instead.
	spec := config.Config{Width: 640, Height: 360, FPS: 30}.StreamSpecFor(config.StreamFull)
	stream := ivfStream(t, 30, []ivfTestFrame{
		{timestamp: 0, payload: []byte{0x00, 0x9d, 0x01, 0x2a}},
		{timestamp: 1, payload: []byte{0x01, 0x00}},
		{timestamp: 4, payload: []byte{0x01, 0x00}}, // two frames were dropped
	})

	next, err := videoDecoder(spec)(stream)
	if err != nil {
		t.Fatal(err)
	}
	first, err := next()
	if err != nil {
		t.Fatal(err)
	}
	if !first.Keyframe {
		t.Fatal("expected the first frame to be reported as a keyframe")
	}
	second, err := next()
	if err != nil {
		t.Fatal(err)
	}
	if !closeEnough(second.Duration, time.Second/30) {
		t.Fatalf("second frame duration = %s, want one frame at 30 FPS", second.Duration)
	}
	third, err := next()
	if err != nil {
		t.Fatal(err)
	}
	if !closeEnough(third.Duration, time.Second/10) {
		t.Fatalf("third frame duration = %s, want three frames at 30 FPS after a drop", third.Duration)
	}
	if _, err := next(); err != io.EOF {
		t.Fatalf("expected EOF at the end of the stream, got %v", err)
	}
}

func newTestBroadcaster(t *testing.T, cfg config.Config) *Broadcaster {
	t.Helper()
	b := NewBroadcaster(cfg)
	// Replace the encoder with one that blocks until cancelled: these tests are
	// about pipeline bookkeeping, not about running ffmpeg.
	b.newSource = func(key deliveryKey) source {
		spec := cfg.StreamSpecWithCodec(key.profile, key.codec)
		return source{
			label: key.String(),
			start: func(ctx context.Context) (io.ReadCloser, func() error, error) {
				reader, writer := io.Pipe()
				go func() {
					<-ctx.Done()
					_ = writer.Close()
				}()
				return reader, func() error { return nil }, nil
			},
			decode: videoDecoder(spec),
		}
	}
	return b
}

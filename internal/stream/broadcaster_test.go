package stream

import (
	"context"
	"testing"
	"time"

	"browser-stream/internal/config"

	"github.com/pion/webrtc/v4"
)

func TestBroadcasterRemoveLastFailedTrackCancelsCapture(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	track := &webrtc.TrackLocalStaticSample{}
	b := NewBroadcaster(config.Config{})
	b.tracks[track] = struct{}{}
	b.running = true
	b.cancel = cancel

	b.remove(track)

	select {
	case <-ctx.Done():
	case <-time.After(100 * time.Millisecond):
		t.Fatal("capture was not cancelled after the final failed track was removed")
	}
}

func TestBroadcasterProfileChangeCancelsRunningCapture(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	b := NewBroadcaster(config.Config{Profile: config.VideoProfile1080p60})
	b.tracks[&webrtc.TrackLocalStaticSample{}] = struct{}{}
	b.running = true
	b.cancel = cancel

	changed := b.SetProfile(config.VideoProfile720p60)

	if !changed || b.Profile() != config.VideoProfile720p60 {
		t.Fatalf("profile change was not retained: changed=%v profile=%q", changed, b.Profile())
	}
	select {
	case <-ctx.Done():
	case <-time.After(100 * time.Millisecond):
		t.Fatal("capture was not cancelled after profile change")
	}
}

func TestRetryDelayIsBoundedExponential(t *testing.T) {
	want := []time.Duration{100 * time.Millisecond, 200 * time.Millisecond, 400 * time.Millisecond, 800 * time.Millisecond, time.Second, time.Second}
	for attempt, expected := range want {
		if got := retryDelay(attempt); got != expected {
			t.Fatalf("retryDelay(%d) = %s, want %s", attempt, got, expected)
		}
	}
}

func TestBroadcasterStatsSnapshot(t *testing.T) {
	b := NewBroadcaster(config.Config{Profile: config.VideoProfile1080p30})
	b.running = true
	b.tracks[&webrtc.TrackLocalStaticSample{}] = struct{}{}
	b.samples.Store(120)
	b.writeFailures.Store(2)
	b.captureStarts.Store(3)
	b.captureFailures.Store(1)

	got := b.Stats()
	if !got.Running || got.Subscribers != 1 || got.Samples != 120 || got.WriteFailures != 2 || got.CaptureStarts != 3 || got.CaptureFailures != 1 {
		t.Fatalf("unexpected stats: %#v", got)
	}
}

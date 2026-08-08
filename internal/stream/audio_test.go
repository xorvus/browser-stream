package stream

import (
	"context"
	"testing"
	"time"

	"browser-stream/internal/config"

	"github.com/pion/webrtc/v4"
)

func TestAudioBroadcasterRemoveLastFailedTrackCancelsCapture(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	track := &webrtc.TrackLocalStaticSample{}
	b := NewAudioBroadcaster(config.Config{})
	b.tracks[track] = struct{}{}
	b.running = true
	b.cancel = cancel

	b.remove(track)

	select {
	case <-ctx.Done():
	case <-time.After(100 * time.Millisecond):
		t.Fatal("audio capture was not cancelled after the final failed track was removed")
	}
}

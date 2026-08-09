package stream

import (
	"context"
	"io"
	"testing"

	"browser-stream/internal/config"

	"github.com/pion/webrtc/v4"
)

func TestAudioBroadcasterSeparatesSaverAndFullEncoders(t *testing.T) {
	b := newTestAudioBroadcaster()
	full := b.Subscribe(&webrtc.TrackLocalStaticSample{}, config.AudioSpec{Kbps: 32, Channels: 2})
	saver := b.Subscribe(&webrtc.TrackLocalStaticSample{}, config.AudioSpec{Kbps: 16, Channels: 1})
	shared := b.Subscribe(&webrtc.TrackLocalStaticSample{}, config.AudioSpec{Kbps: 32, Channels: 2})
	defer func() {
		full()
		saver()
		shared()
	}()

	b.mu.Lock()
	pipelines := len(b.pipelines)
	b.mu.Unlock()
	if pipelines != 2 {
		t.Fatalf("audio pipelines = %d, want 2 (32k stereo and 16k mono)", pipelines)
	}
}

func TestAudioBroadcasterDropsPipelineWhenLastViewerLeaves(t *testing.T) {
	b := newTestAudioBroadcaster()
	b.Subscribe(&webrtc.TrackLocalStaticSample{}, config.AudioSpec{Kbps: 16, Channels: 1})()

	eventually(t, func() bool {
		b.mu.Lock()
		defer b.mu.Unlock()
		return len(b.pipelines) == 0
	}, "the audio pipeline was still registered after its last viewer left")
}

func newTestAudioBroadcaster() *AudioBroadcaster {
	b := NewAudioBroadcaster()
	b.newSource = func(spec config.AudioSpec) source {
		return source{
			label: "test audio",
			start: func(ctx context.Context) (io.ReadCloser, func() error, error) {
				reader, writer := io.Pipe()
				go func() {
					<-ctx.Done()
					_ = writer.Close()
				}()
				return reader, func() error { return nil }, nil
			},
			decode: audioDecoder(spec),
		}
	}
	return b
}

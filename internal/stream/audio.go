package stream

import (
	"context"
	"io"
	"sync"

	"browser-stream/internal/config"

	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media/oggreader"
)

// AudioBroadcaster mirrors Broadcaster for audio: one Opus encoder per distinct
// audio spec. Data-saver viewers get a 16 kbps mono encoder, which is roughly a
// sixth of the default stereo stream and matters when the whole budget is
// 100 kbps.
type AudioBroadcaster struct {
	mu        sync.Mutex
	pipelines map[config.AudioSpec]*pipeline

	// newSource is a seam for tests, matching Broadcaster.
	newSource func(config.AudioSpec) source

	stats struct {
		samples         uint64
		writeFailures   uint64
		captureStarts   uint64
		captureFailures uint64
	}
}

func NewAudioBroadcaster() *AudioBroadcaster {
	b := &AudioBroadcaster{pipelines: make(map[config.AudioSpec]*pipeline)}
	b.newSource = audioSource
	return b
}

func (b *AudioBroadcaster) Subscribe(track *webrtc.TrackLocalStaticSample, spec config.AudioSpec) func() {
	b.mu.Lock()
	entry, ok := b.pipelines[spec]
	if !ok {
		entry = newPipeline(func() source { return b.newSource(spec) })
		entry.onIdle = func() { b.release(spec) }
		b.pipelines[spec] = entry
	}
	b.mu.Unlock()
	return entry.subscribe(track)
}

func (b *AudioBroadcaster) release(spec config.AudioSpec) {
	b.mu.Lock()
	defer b.mu.Unlock()
	entry, ok := b.pipelines[spec]
	if !ok {
		return
	}
	if running, subscribers := entry.stats(); subscribers > 0 || running {
		return
	}
	b.stats.samples += entry.samples.Load()
	b.stats.writeFailures += entry.writeFailures.Load()
	b.stats.captureStarts += entry.captureStarts.Load()
	b.stats.captureFailures += entry.captureFailures.Load()
	delete(b.pipelines, spec)
}

func audioSource(spec config.AudioSpec) source {
	return source{
		label: "browser audio",
		start: func(ctx context.Context) (io.ReadCloser, func() error, error) {
			return StartAudio(ctx, spec)
		},
		decode: audioDecoder(spec),
	}
}

// audioDecoder reads one Opus packet per Ogg page. The sample duration must
// match the encoder's frame size, not a hardcoded 20 ms: the saver profile uses
// 60 ms frames, and mismatched durations would walk the RTP clock three times
// too fast and break audio/video sync.
func audioDecoder(spec config.AudioSpec) func(io.Reader) (sampleReader, error) {
	frame := spec.FrameDuration()
	return func(stdout io.Reader) (sampleReader, error) {
		reader, _, err := oggreader.NewWith(stdout)
		if err != nil {
			return nil, err
		}
		return func() (sample, error) {
			for {
				payload, pageHeader, err := reader.ParseNextPage()
				if err != nil {
					return sample{}, err
				}
				if _, isHeader := pageHeader.HeaderType(payload); isHeader {
					continue
				}
				return sample{Data: payload, Duration: frame, Keyframe: true}, nil
			}
		}, nil
	}
}

func (b *AudioBroadcaster) Stats() BroadcasterStats {
	b.mu.Lock()
	defer b.mu.Unlock()

	stats := BroadcasterStats{
		Samples:         b.stats.samples,
		WriteFailures:   b.stats.writeFailures,
		CaptureStarts:   b.stats.captureStarts,
		CaptureFailures: b.stats.captureFailures,
	}
	for _, entry := range b.pipelines {
		running, subscribers := entry.stats()
		stats.Running = stats.Running || running
		stats.Subscribers += subscribers
		stats.Samples += entry.samples.Load()
		stats.WriteFailures += entry.writeFailures.Load()
		stats.CaptureStarts += entry.captureStarts.Load()
		stats.CaptureFailures += entry.captureFailures.Load()
	}
	return stats
}

package stream

import (
	"context"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"browser-stream/internal/config"

	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
	"github.com/pion/webrtc/v4/pkg/media/oggreader"
)

const opusFrameDuration = 20 * time.Millisecond

type AudioBroadcaster struct {
	cfg     config.Config
	mu      sync.Mutex
	tracks  map[*webrtc.TrackLocalStaticSample]struct{}
	cached  atomic.Value
	running bool
	cancel  context.CancelFunc
	retries atomic.Int32

	samples         atomic.Uint64
	writeFailures   atomic.Uint64
	captureStarts   atomic.Uint64
	captureFailures atomic.Uint64
}

func NewAudioBroadcaster(cfg config.Config) *AudioBroadcaster {
	return &AudioBroadcaster{cfg: cfg, tracks: make(map[*webrtc.TrackLocalStaticSample]struct{})}
}

func (b *AudioBroadcaster) Subscribe(track *webrtc.TrackLocalStaticSample) func() {
	b.mu.Lock()
	b.tracks[track] = struct{}{}
	b.updateCachedLocked()
	if !b.running {
		b.startLocked(0)
	}
	b.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			b.mu.Lock()
			delete(b.tracks, track)
			b.updateCachedLocked()
			if len(b.tracks) == 0 && b.cancel != nil {
				b.cancel()
			}
			b.mu.Unlock()
		})
	}
}

func (b *AudioBroadcaster) run(ctx context.Context) {
	b.captureStarts.Add(1)
	stdout, wait, err := StartAudio(ctx, b.cfg)
	if err != nil {
		log.Printf("start browser audio capture: %v", err)
		b.captureFailures.Add(1)
		b.finish(true)
		return
	}
	defer func() {
		_ = stdout.Close()
		failed := ctx.Err() == nil
		if err := wait(); err != nil && failed {
			log.Printf("browser audio capture stopped: %v", err)
		}
		if failed {
			b.captureFailures.Add(1)
		}
		b.finish(failed)
	}()

	reader, _, err := oggreader.NewWith(stdout)
	if err != nil {
		log.Printf("read Ogg/Opus header: %v", err)
		return
	}
	log.Printf("browser audio capture: Opus 48000 Hz stereo")

	for {
		payload, pageHeader, err := reader.ParseNextPage()
		if err != nil {
			return
		}
		if _, isHeader := pageHeader.HeaderType(payload); isHeader {
			continue
		}
		b.samples.Add(1)
		b.retries.Store(0)
		for _, track := range b.snapshot() {
			if err := track.WriteSample(media.Sample{Data: payload, Duration: opusFrameDuration}); err != nil {
				b.writeFailures.Add(1)
				b.remove(track)
			}
		}
	}
}

func (b *AudioBroadcaster) updateCachedLocked() {
	tracks := make([]*webrtc.TrackLocalStaticSample, 0, len(b.tracks))
	for track := range b.tracks {
		tracks = append(tracks, track)
	}
	b.cached.Store(tracks)
}

func (b *AudioBroadcaster) snapshot() []*webrtc.TrackLocalStaticSample {
	if v := b.cached.Load(); v != nil {
		return v.([]*webrtc.TrackLocalStaticSample)
	}
	return nil
}

func (b *AudioBroadcaster) remove(track *webrtc.TrackLocalStaticSample) {
	b.mu.Lock()
	delete(b.tracks, track)
	b.updateCachedLocked()
	if len(b.tracks) == 0 && b.cancel != nil {
		b.cancel()
	}
	b.mu.Unlock()
}

func (b *AudioBroadcaster) finish(failed bool) {
	b.mu.Lock()
	b.running, b.cancel = false, nil
	b.updateCachedLocked()
	if len(b.tracks) > 0 {
		delay := time.Duration(0)
		if failed {
			delay = retryDelay(int(b.retries.Load()))
			b.retries.Add(1)
		}
		b.startLocked(delay)
	}
	b.mu.Unlock()
}

func (b *AudioBroadcaster) startLocked(delay time.Duration) {
	ctx, cancel := context.WithCancel(context.Background())
	b.running, b.cancel = true, cancel
	go func() {
		if delay > 0 {
			timer := time.NewTimer(delay)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				b.finish(false)
				return
			case <-timer.C:
			}
		}
		b.run(ctx)
	}()
}

func (b *AudioBroadcaster) Stats() BroadcasterStats {
	b.mu.Lock()
	running, subscribers := b.running, len(b.tracks)
	b.mu.Unlock()
	return BroadcasterStats{
		Running: running, Subscribers: subscribers, Samples: b.samples.Load(), WriteFailures: b.writeFailures.Load(),
		CaptureStarts: b.captureStarts.Load(), CaptureFailures: b.captureFailures.Load(),
	}
}

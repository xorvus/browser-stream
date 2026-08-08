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
	"github.com/pion/webrtc/v4/pkg/media/ivfreader"
)

type Broadcaster struct {
	cfg     config.Config
	mu      sync.Mutex
	tracks  map[*webrtc.TrackLocalStaticSample]struct{}
	running bool
	cancel  context.CancelFunc
	retries int

	samples         atomic.Uint64
	writeFailures   atomic.Uint64
	captureStarts   atomic.Uint64
	captureFailures atomic.Uint64
}

type BroadcasterStats struct {
	Running         bool   `json:"running"`
	Subscribers     int    `json:"subscribers"`
	Samples         uint64 `json:"samples"`
	WriteFailures   uint64 `json:"writeFailures"`
	CaptureStarts   uint64 `json:"captureStarts"`
	CaptureFailures uint64 `json:"captureFailures"`
}

func NewBroadcaster(cfg config.Config) *Broadcaster {
	return &Broadcaster{cfg: cfg, tracks: make(map[*webrtc.TrackLocalStaticSample]struct{})}
}

func (b *Broadcaster) Subscribe(track *webrtc.TrackLocalStaticSample) func() {
	b.mu.Lock()
	b.tracks[track] = struct{}{}
	if !b.running {
		b.startLocked(0)
	}
	b.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			b.mu.Lock()
			delete(b.tracks, track)
			if len(b.tracks) == 0 && b.cancel != nil {
				b.cancel()
			}
			b.mu.Unlock()
		})
	}
}

func (b *Broadcaster) run(ctx context.Context) {
	b.mu.Lock()
	cfg := b.cfg
	b.mu.Unlock()
	b.captureStarts.Add(1)
	stdout, wait, err := Start(ctx, cfg)
	if err != nil {
		log.Printf("start browser capture: %v", err)
		b.captureFailures.Add(1)
		b.finish(true)
		return
	}
	defer func() {
		_ = stdout.Close()
		failed := ctx.Err() == nil
		if err := wait(); err != nil && failed {
			log.Printf("browser capture stopped: %v", err)
		}
		if failed {
			b.captureFailures.Add(1)
		}
		b.finish(failed)
	}()

	reader, header, err := ivfreader.NewWith(stdout)
	if err != nil {
		log.Printf("read IVF header: %v", err)
		return
	}
	_, _, outputFPS := cfg.VideoOutput()
	log.Printf("browser capture: %s %dx%d at %d FPS", header.FourCC, header.Width, header.Height, outputFPS)
	frameDuration := time.Second / time.Duration(outputFPS)
	for {
		frame, _, err := reader.ParseNextFrame()
		if err != nil {
			return
		}
		b.samples.Add(1)
		b.mu.Lock()
		b.retries = 0
		b.mu.Unlock()
		for _, track := range b.snapshot() {
			if err := track.WriteSample(media.Sample{Data: frame, Duration: frameDuration}); err != nil {
				b.writeFailures.Add(1)
				b.remove(track)
			}
		}
	}
}

func (b *Broadcaster) snapshot() []*webrtc.TrackLocalStaticSample {
	b.mu.Lock()
	defer b.mu.Unlock()
	tracks := make([]*webrtc.TrackLocalStaticSample, 0, len(b.tracks))
	for track := range b.tracks {
		tracks = append(tracks, track)
	}
	return tracks
}

func (b *Broadcaster) remove(track *webrtc.TrackLocalStaticSample) {
	b.mu.Lock()
	delete(b.tracks, track)
	if len(b.tracks) == 0 && b.cancel != nil {
		b.cancel()
	}
	b.mu.Unlock()
}

func (b *Broadcaster) finish(failed bool) {
	b.mu.Lock()
	b.running, b.cancel = false, nil
	if len(b.tracks) > 0 {
		delay := time.Duration(0)
		if failed {
			delay = retryDelay(b.retries)
			b.retries++
		}
		b.startLocked(delay)
	}
	b.mu.Unlock()
}

func (b *Broadcaster) startLocked(delay time.Duration) {
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

func retryDelay(attempt int) time.Duration {
	delay := 100 * time.Millisecond * time.Duration(1<<min(attempt, 4))
	return min(delay, time.Second)
}

func (b *Broadcaster) SetProfile(profile config.VideoProfile) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.cfg.Profile == profile {
		return false
	}
	b.cfg.Profile = profile
	if b.running && b.cancel != nil {
		b.cancel()
	}
	return true
}

func (b *Broadcaster) Profile() config.VideoProfile {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.cfg.Profile
}

func (b *Broadcaster) Stats() BroadcasterStats {
	b.mu.Lock()
	running, subscribers := b.running, len(b.tracks)
	b.mu.Unlock()
	return BroadcasterStats{
		Running: running, Subscribers: subscribers, Samples: b.samples.Load(), WriteFailures: b.writeFailures.Load(),
		CaptureStarts: b.captureStarts.Load(), CaptureFailures: b.captureFailures.Load(),
	}
}

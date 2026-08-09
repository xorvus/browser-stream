package stream

import (
	"context"
	"io"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pion/webrtc/v4/pkg/media"

	"github.com/pion/webrtc/v4"
)

// sample is one encoded unit ready for RTP packetisation. Keyframe is what lets
// a viewer that joins a running encoder wait for a decodable starting point
// instead of rendering corruption.
type sample struct {
	Data     []byte
	Duration time.Duration
	Keyframe bool
}

// sampleReader pulls the next sample from a running ffmpeg process. It returns
// an error exactly once, when the stream ends.
type sampleReader func() (sample, error)

// source is the pluggable half of a pipeline: how to start the encoder and how
// to turn its stdout into samples.
type source struct {
	label  string
	start  func(context.Context) (io.ReadCloser, func() error, error)
	decode func(io.Reader) (sampleReader, error)
}

type subscriber struct {
	track *webrtc.TrackLocalStaticSample
	// gated is set for viewers that joined mid-GOP. They receive nothing until
	// the next keyframe, which is cheaper and less ugly than sending them
	// undecodable inter frames.
	gated atomic.Bool
}

// pipeline owns one ffmpeg process and fans its output out to every subscriber
// that asked for the same encoder settings. It starts on the first subscriber
// and stops on the last, so an idle server runs no encoders at all.
type pipeline struct {
	source func() source

	mu      sync.Mutex
	tracks  map[*webrtc.TrackLocalStaticSample]*subscriber
	cached  atomic.Value // []*subscriber
	running bool
	cancel  context.CancelFunc
	onIdle  func()

	// retries is atomic because it is reset on every decoded sample; taking the
	// pipeline mutex per frame would put lock traffic on the hot path.
	retries atomic.Int32

	samples         atomic.Uint64
	writeFailures   atomic.Uint64
	captureStarts   atomic.Uint64
	captureFailures atomic.Uint64
}

func newPipeline(source func() source) *pipeline {
	return &pipeline{source: source, tracks: make(map[*webrtc.TrackLocalStaticSample]*subscriber)}
}

func (p *pipeline) subscribe(track *webrtc.TrackLocalStaticSample) func() {
	p.mu.Lock()
	entry := &subscriber{track: track}
	// Only gate when an encoder is already mid-stream. A pipeline that is about
	// to start always emits a keyframe first.
	entry.gated.Store(p.running)
	p.tracks[track] = entry
	p.updateCachedLocked()
	if !p.running {
		p.startLocked(0)
	}
	p.mu.Unlock()

	var once sync.Once
	return func() { once.Do(func() { p.remove(track) }) }
}

func (p *pipeline) remove(track *webrtc.TrackLocalStaticSample) {
	p.mu.Lock()
	delete(p.tracks, track)
	p.updateCachedLocked()
	idle := len(p.tracks) == 0
	if idle && p.cancel != nil {
		p.cancel()
	}
	onIdle := p.onIdle
	p.mu.Unlock()
	if idle && onIdle != nil {
		onIdle()
	}
}

// restart stops the current encoder. Subscribers stay attached, so finish()
// immediately brings a fresh process up with whatever settings source() now
// reports.
func (p *pipeline) restart() {
	p.mu.Lock()
	if p.running && p.cancel != nil {
		p.cancel()
	}
	p.mu.Unlock()
}

func (p *pipeline) updateCachedLocked() {
	subscribers := make([]*subscriber, 0, len(p.tracks))
	for _, entry := range p.tracks {
		subscribers = append(subscribers, entry)
	}
	p.cached.Store(subscribers)
}

func (p *pipeline) snapshot() []*subscriber {
	if value := p.cached.Load(); value != nil {
		return value.([]*subscriber)
	}
	return nil
}

func (p *pipeline) startLocked(delay time.Duration) {
	ctx, cancel := context.WithCancel(context.Background())
	p.running, p.cancel = true, cancel
	source := p.source()
	go func() {
		if delay > 0 {
			timer := time.NewTimer(delay)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				p.finish(false)
				return
			case <-timer.C:
			}
		}
		p.run(ctx, source)
	}()
}

func (p *pipeline) finish(failed bool) {
	p.mu.Lock()
	p.running, p.cancel = false, nil
	idle := len(p.tracks) == 0
	if !idle {
		delay := time.Duration(0)
		if failed {
			delay = retryDelay(int(p.retries.Load()))
			p.retries.Add(1)
		}
		p.startLocked(delay)
	}
	onIdle := p.onIdle
	p.mu.Unlock()
	// The encoder has now actually exited, so the owning registry can drop this
	// pipeline. Reporting idleness only from remove() left a stopped pipeline
	// in the map, because at that moment it was still shutting down.
	if idle && onIdle != nil {
		onIdle()
	}
}

func (p *pipeline) run(ctx context.Context, src source) {
	p.captureStarts.Add(1)
	stdout, wait, err := src.start(ctx)
	if err != nil {
		log.Printf("start %s capture: %v", src.label, err)
		p.captureFailures.Add(1)
		p.finish(true)
		return
	}
	defer func() {
		_ = stdout.Close()
		failed := ctx.Err() == nil
		if err := wait(); err != nil && failed {
			log.Printf("%s capture stopped: %v", src.label, err)
		}
		if failed {
			p.captureFailures.Add(1)
		}
		p.finish(failed)
	}()

	next, err := src.decode(stdout)
	if err != nil {
		log.Printf("read %s stream header: %v", src.label, err)
		return
	}

	for {
		current, err := next()
		if err != nil {
			return
		}
		p.samples.Add(1)
		p.retries.Store(0)
		for _, entry := range p.snapshot() {
			if entry.gated.Load() {
				if !current.Keyframe {
					continue
				}
				entry.gated.Store(false)
			}
			if err := entry.track.WriteSample(media.Sample{Data: current.Data, Duration: current.Duration}); err != nil {
				p.writeFailures.Add(1)
				p.remove(entry.track)
			}
		}
	}
}

func (p *pipeline) stats() (running bool, subscribers int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.running, len(p.tracks)
}

func retryDelay(attempt int) time.Duration {
	delay := 100 * time.Millisecond * time.Duration(1<<min(attempt, 4))
	return min(delay, time.Second)
}

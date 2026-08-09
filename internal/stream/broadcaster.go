package stream

import (
	"context"
	"io"
	"log"
	"sort"
	"sync"
	"time"

	"browser-stream/internal/config"

	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media/ivfreader"
)

// Broadcaster owns one encoder pipeline per distinct delivery profile.
//
// Before this, a single shared encoder meant one viewer picking a lower quality
// dragged every other viewer down with it. Now a data-saver viewer gets its own
// small VP9 encoder while everyone else keeps the full-rate VP8 stream, and the
// saver encoder only exists while somebody is actually using it.
// deliveryKey identifies one encoder. Codec is part of the identity because a
// viewer whose browser did not offer VP9 needs a VP8 encoder for the same
// profile, and the two cannot share a process.
type deliveryKey struct {
	profile config.StreamProfile
	codec   config.VideoCodec
}

func (k deliveryKey) String() string { return string(k.profile) + "/" + string(k.codec) }

type Broadcaster struct {
	mu        sync.Mutex
	cfg       config.Config
	pipelines map[deliveryKey]*pipeline

	// newSource is a seam for tests, which need pipeline bookkeeping without
	// spawning ffmpeg.
	newSource func(deliveryKey) source

	stats struct {
		samples         uint64
		writeFailures   uint64
		captureStarts   uint64
		captureFailures uint64
	}
}

type BroadcasterStats struct {
	Running         bool             `json:"running"`
	Subscribers     int              `json:"subscribers"`
	Samples         uint64           `json:"samples"`
	WriteFailures   uint64           `json:"writeFailures"`
	CaptureStarts   uint64           `json:"captureStarts"`
	CaptureFailures uint64           `json:"captureFailures"`
	Pipelines       []PipelineStats  `json:"pipelines,omitempty"`
	Estimate        *BitrateEstimate `json:"estimate,omitempty"`
}

// PipelineStats exposes each encoder separately so an operator can see which
// profile a stall belongs to instead of reading one aggregate number.
type PipelineStats struct {
	Profile     string `json:"profile"`
	Running     bool   `json:"running"`
	Subscribers int    `json:"subscribers"`
	VideoKbps   int    `json:"videoKbps"`
	AudioKbps   int    `json:"audioKbps"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	FPS         int    `json:"fps"`
	Codec       string `json:"codec"`
}

// BitrateEstimate is the advertised cost of each profile, so the viewer can
// show a real number instead of a vague "low quality" label.
type BitrateEstimate struct {
	Profile   string `json:"profile"`
	TotalKbps int    `json:"totalKbps"`
}

func NewBroadcaster(cfg config.Config) *Broadcaster {
	b := &Broadcaster{cfg: cfg, pipelines: make(map[deliveryKey]*pipeline)}
	b.newSource = b.videoSource
	return b
}

// Subscribe attaches a track to the encoder for the requested profile and
// codec, starting that encoder if it is not already running.
func (b *Broadcaster) Subscribe(track *webrtc.TrackLocalStaticSample, profile config.StreamProfile, codec config.VideoCodec) func() {
	key := deliveryKey{profile: profile, codec: codec}
	b.mu.Lock()
	entry, ok := b.pipelines[key]
	if !ok {
		entry = newPipeline(func() source { return b.newSource(key) })
		entry.onIdle = func() { b.release(key) }
		b.pipelines[key] = entry
	}
	b.mu.Unlock()
	return entry.subscribe(track)
}

// release drops an encoder once its last viewer leaves so a long-lived server
// does not accumulate one idle pipeline per profile ever requested.
func (b *Broadcaster) release(key deliveryKey) {
	b.mu.Lock()
	defer b.mu.Unlock()
	entry, ok := b.pipelines[key]
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
	delete(b.pipelines, key)
}

func (b *Broadcaster) videoSource(key deliveryKey) source {
	b.mu.Lock()
	spec := b.cfg.StreamSpecWithCodec(key.profile, key.codec)
	b.mu.Unlock()
	return source{
		label: "browser " + key.String(),
		start: func(ctx context.Context) (io.ReadCloser, func() error, error) {
			log.Printf("browser capture %s: %s %dp at %d FPS, %d kbps",
				key, spec.Codec, outputHeight(spec), spec.CaptureFPS, spec.VideoKbps)
			return Start(ctx, spec)
		},
		decode: videoDecoder(spec),
	}
}

func outputHeight(spec config.StreamSpec) int {
	if spec.Height > 0 && spec.Height < spec.CaptureHeight {
		return spec.Height
	}
	return spec.CaptureHeight
}

// videoDecoder turns the IVF byte stream into samples. Frame duration comes
// from the IVF timestamps rather than a nominal 1/FPS constant: ffmpeg drops
// frames under load (-drop-threshold), and assuming a fixed duration made RTP
// timestamps drift away from wall clock, which shows up as growing latency and
// audio/video desync.
func videoDecoder(spec config.StreamSpec) func(io.Reader) (sampleReader, error) {
	return func(stdout io.Reader) (sampleReader, error) {
		reader, header, err := ivfreader.NewWith(stdout)
		if err != nil {
			return nil, err
		}
		fallback := time.Second / time.Duration(max(spec.CaptureFPS, 1))

		// One IVF tick is TimebaseNumerator/TimebaseDenominator seconds, but
		// ivfreader.ParseNextFrame already pre-scales the raw timestamp by
		// TimebaseDenominator/TimebaseNumerator. Converting what it hands back
		// therefore squares the ratio. ffmpeg writes denominator=FPS and
		// numerator=1, so consecutive frames arrive FPS ticks apart and a
		// dropped frame shows up as a proportionally larger gap.
		ratio := 0.0
		if header.TimebaseDenominator > 0 && header.TimebaseNumerator > 0 {
			ratio = float64(header.TimebaseNumerator) / float64(header.TimebaseDenominator)
		}
		perTick := ratio * ratio * float64(time.Second)

		var previous uint64
		var seen bool
		return func() (sample, error) {
			frame, frameHeader, err := reader.ParseNextFrame()
			if err != nil {
				return sample{}, err
			}
			duration := fallback
			if seen && perTick > 0 && frameHeader.Timestamp > previous {
				duration = time.Duration(float64(frameHeader.Timestamp-previous) * perTick)
			}
			if duration <= 0 || duration > time.Second {
				duration = fallback
			}
			previous, seen = frameHeader.Timestamp, true
			return sample{Data: frame, Duration: duration, Keyframe: isKeyframe(spec.Codec, frame)}, nil
		}, nil
	}
}

// isKeyframe inspects the first byte of the frame. It fails open: an
// unrecognised header is treated as a keyframe so a viewer can never be gated
// forever by a parsing gap.
func isKeyframe(codec config.VideoCodec, frame []byte) bool {
	if len(frame) == 0 {
		return true
	}
	if codec == config.CodecVP9 {
		// Profile 0 uncompressed header: 10 marker, 00 profile, then
		// show_existing_frame and frame_type. Anything else is not classified.
		if frame[0]&0xF0 != 0x80 {
			return true
		}
		return frame[0]&0x08 == 0 && frame[0]&0x04 == 0
	}
	return frame[0]&0x01 == 0
}

// SetProfile changes the capture geometry for every pipeline. Saver pipelines
// scale from whatever is captured, so they follow along without the viewer
// having to renegotiate.
func (b *Broadcaster) SetProfile(profile config.VideoProfile) bool {
	b.mu.Lock()
	if b.cfg.Profile == profile {
		b.mu.Unlock()
		return false
	}
	b.cfg.Profile = profile
	running := make([]*pipeline, 0, len(b.pipelines))
	for _, entry := range b.pipelines {
		running = append(running, entry)
	}
	b.mu.Unlock()

	for _, entry := range running {
		entry.restart()
	}
	return true
}

func (b *Broadcaster) Profile() config.VideoProfile {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.cfg.Profile
}

// SpecFor exposes the advertised cost of a profile, for /stats and for the
// signalling handler that has to pick a codec before subscribing.
func (b *Broadcaster) SpecFor(profile config.StreamProfile) config.StreamSpec {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.cfg.StreamSpecFor(profile)
}

func (b *Broadcaster) Stats() BroadcasterStats {
	b.mu.Lock()
	defer b.mu.Unlock()

	stats := BroadcasterStats{
		Samples:         b.stats.samples,
		WriteFailures:   b.stats.writeFailures,
		CaptureStarts:   b.stats.captureStarts,
		CaptureFailures: b.stats.captureFailures,
	}
	for key, entry := range b.pipelines {
		running, subscribers := entry.stats()
		spec := b.cfg.StreamSpecWithCodec(key.profile, key.codec)
		stats.Running = stats.Running || running
		stats.Subscribers += subscribers
		stats.Samples += entry.samples.Load()
		stats.WriteFailures += entry.writeFailures.Load()
		stats.CaptureStarts += entry.captureStarts.Load()
		stats.CaptureFailures += entry.captureFailures.Load()
		stats.Pipelines = append(stats.Pipelines, PipelineStats{
			Profile:     string(key.profile),
			Running:     running,
			Subscribers: subscribers,
			VideoKbps:   spec.VideoKbps,
			AudioKbps:   spec.AudioKbps,
			Width:       spec.CaptureWidth,
			Height:      outputHeight(spec),
			FPS:         spec.CaptureFPS,
			Codec:       string(spec.Codec),
		})
	}
	sort.Slice(stats.Pipelines, func(i, j int) bool { return stats.Pipelines[i].Profile < stats.Pipelines[j].Profile })
	return stats
}

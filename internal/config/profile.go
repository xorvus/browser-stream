package config

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// VideoCodec is the encoder a delivery pipeline uses. VP9 costs more CPU per
// frame but needs roughly 30% fewer bits for the same quality, which only pays
// off at the small resolutions used by the data-saver profiles.
type VideoCodec string

const (
	CodecVP8 VideoCodec = "vp8"
	CodecVP9 VideoCodec = "vp9"
)

// StreamProfile selects how a single viewer is served. It is independent of
// VideoProfile: VideoProfile decides what is captured (browser window size),
// StreamProfile decides what that capture is re-encoded to for one viewer.
type StreamProfile string

const (
	// StreamFull re-uses the capture geometry untouched.
	StreamFull StreamProfile = "full"
	// StreamSaver480 targets roughly 375 kbps end to end.
	StreamSaver480 StreamProfile = "saver480"
	// StreamSaver360 targets roughly 100 kbps end to end.
	StreamSaver360 StreamProfile = "saver360"
)

func ParseStreamProfile(raw string) (StreamProfile, error) {
	profile := StreamProfile(strings.ToLower(strings.TrimSpace(raw)))
	switch profile {
	case "":
		return StreamFull, nil
	case StreamFull, StreamSaver480, StreamSaver360:
		return profile, nil
	default:
		return "", fmt.Errorf("stream profile must be one of full, saver480, or saver360")
	}
}

// StreamSpec is everything an encoder pipeline needs. It is a value type so it
// doubles as the pipeline identity: two viewers asking for the same spec share
// one ffmpeg process.
type StreamSpec struct {
	Profile StreamProfile

	// CaptureWidth/CaptureHeight/CaptureFPS describe the x11grab input.
	CaptureWidth  int
	CaptureHeight int
	CaptureFPS    int

	// Height is the encoded output height. Zero means "encode the capture as
	// captured"; width is always derived from the capture aspect ratio.
	Height int

	VideoKbps  int
	Codec      VideoCodec
	CPUUsed    int
	Threads    int
	GOPSeconds int

	AudioKbps     int
	AudioChannels int
	// AudioFrameMillis is the Opus frame size. At a 100 kbps budget the
	// per-packet RTP/UDP/IP header cost is not a rounding error: 20 ms frames
	// mean 50 packets per second, which is 16 kbps of headers on top of a
	// 16 kbps audio stream. 60 ms frames cut that to 5 kbps for 40 ms of extra
	// audio latency.
	AudioFrameMillis int
	// AudioApplication is the libopus tuning. Restricted low delay gives the
	// tightest latency; the generic audio mode compresses better, which is the
	// right trade once a profile has already accepted larger frames.
	AudioApplication string
}

// Saver reports whether this spec is a bandwidth-constrained delivery.
func (s StreamSpec) Saver() bool { return s.Profile != StreamFull }

// TotalKbps is what the viewer actually pays for: both payloads plus the
// RTP/UDP/IP headers each packet carries. Quoting only the encoder bitrates
// understates a low-rate audio stream badly, because its header cost can rival
// its payload.
func (s StreamSpec) TotalKbps() int {
	const headerBytes = 12 + 8 + 20 // RTP + UDP + IPv4
	const videoPayloadBytes = 1200  // typical WebRTC packetisation limit

	audioPackets := 0.0
	if s.AudioKbps > 0 && s.AudioFrameMillis > 0 {
		audioPackets = 1000 / float64(s.AudioFrameMillis)
	}
	videoPackets := float64(s.VideoKbps) * 1000 / 8 / videoPayloadBytes
	overhead := (audioPackets + videoPackets) * headerBytes * 8 / 1000
	return s.VideoKbps + s.AudioKbps + int(overhead+0.5)
}

// GOPFrames is the keyframe interval in frames. A new viewer joining a running
// pipeline waits at most this long before its first decodable frame.
func (s StreamSpec) GOPFrames() int {
	frames := s.GOPSeconds * s.CaptureFPS
	if frames < 1 {
		return 1
	}
	return frames
}

// videoLadder caps each capture profile at a bitrate that suits its pixel rate.
// Before this existed every profile encoded at VIDEO_BITRATE, so picking 720p30
// in the UI cut the pixel rate by 75% while still targeting 6000 kbps.
var videoLadder = map[VideoProfile]int{
	VideoProfile1080p60: 6000,
	VideoProfile1080p30: 4000,
	VideoProfile720p60:  3000,
	VideoProfile720p30:  1800,
}

func ladderKbps(profile VideoProfile) int {
	if kbps, ok := videoLadder[profile]; ok {
		return kbps
	}
	return videoLadder[VideoProfile1080p60]
}

// StreamSpecFor builds the encoder spec for one viewer, given the capture
// geometry currently in force.
func (c Config) StreamSpecFor(profile StreamProfile) StreamSpec {
	captureWidth, captureHeight, captureFPS := c.VideoOutput()

	spec := StreamSpec{
		Profile:          profile,
		CaptureWidth:     captureWidth,
		CaptureHeight:    captureHeight,
		CaptureFPS:       captureFPS,
		Codec:            CodecVP8,
		CPUUsed:          16,
		Threads:          4,
		GOPSeconds:       2,
		AudioChannels:    2,
		AudioFrameMillis: 20,
		AudioApplication: "lowdelay",
	}

	switch profile {
	case StreamSaver360:
		spec.Height = 360
		spec.CaptureFPS = min(captureFPS, 15)
		spec.VideoKbps = 70
		spec.AudioKbps = 16
		spec.AudioChannels = 1
		spec.AudioFrameMillis = 60
		spec.AudioApplication = "audio"
		spec.Codec = CodecVP9
		spec.CPUUsed = 8
		spec.Threads = 2
		spec.GOPSeconds = 4
	case StreamSaver480:
		spec.Height = 480
		spec.CaptureFPS = min(captureFPS, 20)
		spec.VideoKbps = 350
		spec.AudioKbps = 24
		spec.Codec = CodecVP9
		spec.CPUUsed = 8
		spec.Threads = 4
		spec.GOPSeconds = 4
	default:
		spec.VideoKbps = ladderKbps(c.Profile)
		spec.AudioKbps = kbpsFromRate(c.AudioBitrate, 32)
	}

	// VIDEO_BITRATE and AUDIO_BITRATE are ceilings, never floors: an operator
	// can throttle the whole server without being able to push a saver viewer
	// back over its budget.
	if cap := kbpsFromRate(c.Bitrate, 0); cap > 0 && cap < spec.VideoKbps {
		spec.VideoKbps = cap
	}
	if cap := kbpsFromRate(c.AudioBitrate, 0); cap > 0 && cap < spec.AudioKbps {
		spec.AudioKbps = cap
	}
	return spec
}

// StreamSpecWithCodec is StreamSpecFor with the codec forced, used when a
// viewer's browser did not offer VP9. The bitrate budget is deliberately not
// raised to compensate: a saver profile is a promise about bytes on the wire,
// so a VP8 fallback spends the same budget at lower quality.
func (c Config) StreamSpecWithCodec(profile StreamProfile, codec VideoCodec) StreamSpec {
	spec := c.StreamSpecFor(profile)
	if codec != "" {
		spec.Codec = codec
	}
	return spec
}

// AudioSpec is the audio half of a StreamSpec, extracted so viewers with the
// same audio settings but different video settings share one encoder.
type AudioSpec struct {
	Kbps        int
	Channels    int
	FrameMillis int
	Application string
}

func (s StreamSpec) AudioSpec() AudioSpec {
	return AudioSpec{
		Kbps:        s.AudioKbps,
		Channels:    s.AudioChannels,
		FrameMillis: s.AudioFrameMillis,
		Application: s.AudioApplication,
	}
}

// FrameDuration is the RTP sample duration for one Opus packet.
func (a AudioSpec) FrameDuration() time.Duration {
	if a.FrameMillis <= 0 {
		return 20 * time.Millisecond
	}
	return time.Duration(a.FrameMillis) * time.Millisecond
}

// kbpsFromRate parses ffmpeg-style rate strings ("6000k", "6M", "96000").
func kbpsFromRate(raw string, fallback int) int {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return fallback
	}
	multiplier := 1
	switch {
	case strings.HasSuffix(trimmed, "k"), strings.HasSuffix(trimmed, "K"):
		trimmed = trimmed[:len(trimmed)-1]
	case strings.HasSuffix(trimmed, "m"), strings.HasSuffix(trimmed, "M"):
		trimmed, multiplier = trimmed[:len(trimmed)-1], 1000
	default:
		multiplier = 0 // plain bits per second
	}
	value, err := strconv.Atoi(strings.TrimSpace(trimmed))
	if err != nil || value <= 0 {
		return fallback
	}
	if multiplier == 0 {
		return max(value/1000, 1)
	}
	return value * multiplier
}

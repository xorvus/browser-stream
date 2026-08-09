package config

import "testing"

func TestStreamSpecScalesBitrateWithTheCaptureProfile(t *testing.T) {
	// The ladder exists because bitrate used to be a single global value: every
	// profile encoded at VIDEO_BITRATE regardless of how many pixels it carried.
	cfg := Config{Width: 1920, Height: 1080, FPS: 60, Bitrate: "6000k", AudioBitrate: "32k"}
	want := map[VideoProfile]int{
		VideoProfile1080p60: 6000,
		VideoProfile1080p30: 4000,
		VideoProfile720p60:  3000,
		VideoProfile720p30:  1800,
	}
	for profile, kbps := range want {
		cfg.Profile = profile
		if got := cfg.StreamSpecFor(StreamFull).VideoKbps; got != kbps {
			t.Fatalf("%s video bitrate = %d kbps, want %d", profile, got, kbps)
		}
	}
}

func TestStreamSpecTreatsEnvironmentBitratesAsCeilings(t *testing.T) {
	cfg := Config{Width: 1920, Height: 1080, FPS: 60, Bitrate: "800k", AudioBitrate: "24k", Profile: VideoProfile1080p60}

	full := cfg.StreamSpecFor(StreamFull)
	if full.VideoKbps != 800 || full.AudioKbps != 24 {
		t.Fatalf("full spec = %d/%d kbps, want the configured ceiling 800/24", full.VideoKbps, full.AudioKbps)
	}

	// A ceiling must never raise a saver profile back above its budget.
	saver := cfg.StreamSpecFor(StreamSaver360)
	if saver.VideoKbps != 70 || saver.AudioKbps != 16 {
		t.Fatalf("saver spec = %d/%d kbps, want 70/16", saver.VideoKbps, saver.AudioKbps)
	}
}

func TestSaver360FitsInAHundredKilobits(t *testing.T) {
	spec := Config{Width: 1920, Height: 1080, FPS: 60, Profile: VideoProfile1080p60}.StreamSpecFor(StreamSaver360)

	if spec.TotalKbps() > 100 {
		t.Fatalf("saver360 total = %d kbps, want at most 100", spec.TotalKbps())
	}
	if spec.Height != 360 || spec.CaptureFPS != 15 {
		t.Fatalf("saver360 = %dp%d, want 360p15", spec.Height, spec.CaptureFPS)
	}
	if spec.Codec != CodecVP9 {
		t.Fatalf("saver360 codec = %q, want vp9", spec.Codec)
	}
	if spec.AudioChannels != 1 {
		t.Fatalf("saver360 audio channels = %d, want mono", spec.AudioChannels)
	}
	if spec.GOPFrames() != 60 {
		t.Fatalf("saver360 GOP = %d frames, want 60 (4 seconds at 15 FPS)", spec.GOPFrames())
	}
}

func TestSaverProfileNeverExceedsTheCaptureFrameRate(t *testing.T) {
	// A 720p30 capture must not make a saver stream ask for 20 FPS it cannot get.
	cfg := Config{Width: 1280, Height: 720, FPS: 12, Profile: VideoProfile720p30}
	if got := cfg.StreamSpecFor(StreamSaver480).CaptureFPS; got != 12 {
		t.Fatalf("saver480 capture FPS = %d, want the capture limit 12", got)
	}
}

func TestStreamSpecWithCodecKeepsTheBudget(t *testing.T) {
	cfg := Config{Width: 1920, Height: 1080, FPS: 60, Profile: VideoProfile1080p60}
	spec := cfg.StreamSpecWithCodec(StreamSaver360, CodecVP8)
	if spec.Codec != CodecVP8 {
		t.Fatalf("codec = %q, want vp8", spec.Codec)
	}
	if spec.VideoKbps != 70 {
		t.Fatalf("VP8 fallback bitrate = %d kbps, want the same 70 kbps budget", spec.VideoKbps)
	}
}

func TestParseStreamProfile(t *testing.T) {
	for raw, want := range map[string]StreamProfile{
		"":            StreamFull,
		"full":        StreamFull,
		" SAVER360  ": StreamSaver360,
		"saver480":    StreamSaver480,
	} {
		got, err := ParseStreamProfile(raw)
		if err != nil {
			t.Fatalf("ParseStreamProfile(%q): %v", raw, err)
		}
		if got != want {
			t.Fatalf("ParseStreamProfile(%q) = %q, want %q", raw, got, want)
		}
	}
	if _, err := ParseStreamProfile("potato"); err == nil {
		t.Fatal("expected an unknown stream profile to be rejected")
	}
}

func TestKbpsFromRateAcceptsFFmpegRateStrings(t *testing.T) {
	for raw, want := range map[string]int{
		"6000k":  6000,
		"6M":     6000,
		"96000":  96,
		"  32k ": 32,
		"":       -1,
		"abc":    -1,
	} {
		if got := kbpsFromRate(raw, -1); got != want {
			t.Fatalf("kbpsFromRate(%q) = %d, want %d", raw, got, want)
		}
	}
}

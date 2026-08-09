package stream

import (
	"strings"
	"testing"
	"time"

	"browser-stream/internal/config"
)

func fullSpec(cfg config.Config) config.StreamSpec {
	return cfg.StreamSpecFor(config.StreamFull)
}

func TestFFmpegArgsCaptureTheConfiguredGeometryDirectly(t *testing.T) {
	args := FFmpegArgs(fullSpec(config.Config{
		Width: 1280, Height: 720, FPS: 30, Bitrate: "6000k", Profile: config.VideoProfile720p30,
	}))
	joined := strings.Join(args, " ")

	for _, value := range []string{
		"-f x11grab",
		"-framerate 30",
		"-video_size 1280x720",
		"-i :99.0",
		"-c:v libvpx",
		"-deadline realtime",
		"-cpu-used 16",
		"-lag-in-frames 0",
		"-error-resilient 1",
		"-drop-threshold 30",
		"-screen-content-mode 1",
		"-f ivf pipe:1",
	} {
		if !strings.Contains(joined, value) {
			t.Fatalf("missing %q in %q", value, joined)
		}
	}
	if strings.Contains(joined, "-vf") {
		t.Fatalf("expected direct capture without a scale filter, got %q", joined)
	}
}

func TestFFmpegArgsUseTheLadderBitrateNotTheGlobalCeiling(t *testing.T) {
	// Regression: every profile used to encode at VIDEO_BITRATE, so selecting
	// 720p30 cut the pixel rate by 75% while still targeting 6000 kbps.
	joined := strings.Join(FFmpegArgs(fullSpec(config.Config{
		Width: 1920, Height: 1080, FPS: 60, Bitrate: "6000k", Profile: config.VideoProfile720p30,
	})), " ")
	if !strings.Contains(joined, "-b:v 1800k") {
		t.Fatalf("expected the 720p30 ladder bitrate, got %q", joined)
	}
	if strings.Contains(joined, "6000k") {
		t.Fatalf("the global ceiling leaked into a lower profile: %q", joined)
	}
}

func TestFFmpegArgsKeepKeyframesFarApart(t *testing.T) {
	// A one-second GOP spends a large share of a small budget on intra frames.
	joined := strings.Join(FFmpegArgs(fullSpec(config.Config{
		Width: 1280, Height: 720, FPS: 30, Profile: config.VideoProfile720p30,
	})), " ")
	if !strings.Contains(joined, "-g 60") {
		t.Fatalf("expected a two-second GOP at 30 FPS, got %q", joined)
	}
}

func TestSaverSpecDownscalesInsteadOfResizingTheBrowser(t *testing.T) {
	cfg := config.Config{Width: 1920, Height: 1080, FPS: 60, Profile: config.VideoProfile720p60}
	joined := strings.Join(FFmpegArgs(cfg.StreamSpecFor(config.StreamSaver360)), " ")

	for _, value := range []string{
		"-framerate 15",
		"-video_size 1280x720",
		"-vf scale=-2:360:flags=bilinear",
		"-c:v libvpx-vp9",
		"-tune-content screen",
		"-row-mt 1",
		"-b:v 70k",
		"-maxrate 70k",
		"-g 60",
	} {
		if !strings.Contains(joined, value) {
			t.Fatalf("missing %q in %q", value, joined)
		}
	}
}

func TestSaverSpecFitsInsideAHundredKilobitBudget(t *testing.T) {
	spec := config.Config{Width: 1920, Height: 1080, FPS: 60, Profile: config.VideoProfile1080p60}.
		StreamSpecFor(config.StreamSaver360)
	if total := spec.TotalKbps(); total > 100 {
		t.Fatalf("saver360 budget = %d kbps, want at most 100", total)
	}
	if !spec.Saver() {
		t.Fatal("saver360 should be reported as a saver profile")
	}
}

func TestAudioFFmpegArgsUseLongFramesForSaverViewers(t *testing.T) {
	// At 16 kbps, 20 ms frames cost about as much in packet headers as the
	// audio itself. 60 ms frames cut that to a third.
	spec := config.Config{Width: 1280, Height: 720, FPS: 60, Profile: config.VideoProfile720p60}.
		StreamSpecFor(config.StreamSaver360).AudioSpec()
	joined := strings.Join(AudioFFmpegArgs(spec), " ")

	for _, value := range []string{"-ac 1", "-ar 48000", "-c:a libopus", "-application audio", "-frame_duration 60", "-b:a 16k", "-f ogg pipe:1"} {
		if !strings.Contains(joined, value) {
			t.Fatalf("missing %q in %q", value, joined)
		}
	}
	if spec.FrameDuration() != 60*time.Millisecond {
		t.Fatalf("frame duration = %s, want 60ms", spec.FrameDuration())
	}
}

func TestAudioFFmpegArgsDefaultToLowLatencyStereo(t *testing.T) {
	joined := strings.Join(AudioFFmpegArgs(config.AudioSpec{Kbps: 32}), " ")
	for _, value := range []string{"-ac 2", "-b:a 32k", "-application lowdelay", "-frame_duration 20"} {
		if !strings.Contains(joined, value) {
			t.Fatalf("missing %q in %q", value, joined)
		}
	}
}

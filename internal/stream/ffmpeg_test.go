package stream

import (
	"strings"
	"testing"

	"browser-stream/internal/config"
)

func TestFFmpegArgsUseConfiguredCaptureSettings(t *testing.T) {
	args := FFmpegArgs(config.Config{
		Width:   1280,
		Height:  720,
		FPS:     30,
		Bitrate: "2500k",
		Profile: config.VideoProfile720p30,
	})
	joined := strings.Join(args, " ")

	for _, value := range []string{
		"-f x11grab",
		"-framerate 30",
		"-video_size 1280x720",
		"-i :99.0",
		"-c:v libvpx",
		"-deadline realtime",
		"-cpu-used 16",
		"-threads 4",
		"-lag-in-frames 0",
		"-drop-threshold 30",
		"-screen-content-mode 1",
		"-b:v 2500k",
		"-maxrate 2500k",
		"-bufsize 2500k",
		"-g 30",
		"-f ivf pipe:1",
	} {
		if !strings.Contains(joined, value) {
			t.Fatalf("missing %q in %q", value, joined)
		}
	}
}

func TestFFmpegArgsScaleAndReduceFPSForSharedProfile(t *testing.T) {
	args := FFmpegArgs(config.Config{
		Width: 1920, Height: 1080, FPS: 60, Bitrate: "4000k", Profile: config.VideoProfile720p30,
	})
	joined := strings.Join(args, " ")
	if strings.Contains(joined, "-vf") {
		t.Fatalf("expected direct-size capture without a scale filter, got %q", joined)
	}
	if !strings.Contains(joined, "-framerate 30 -video_size 1280x720") {
		t.Fatalf("expected direct 720p30 capture input, got %q", joined)
	}
}

func TestFFmpegArgsClampProfileToConfiguredCapture(t *testing.T) {
	args := FFmpegArgs(config.Config{
		Width: 1280, Height: 720, FPS: 30, Bitrate: "2500k", Profile: config.VideoProfile1080p60,
	})
	joined := strings.Join(args, " ")
	if strings.Contains(joined, "-vf") {
		t.Fatalf("did not expect upscaling or frame duplication, got %q", joined)
	}
	if !strings.Contains(joined, "-g 30") {
		t.Fatalf("expected clamped GOP, got %q", joined)
	}
}

func TestAudioFFmpegArgsCapturePulseMonitorAsOpus(t *testing.T) {
	args := AudioFFmpegArgs(config.Config{AudioBitrate: "96k"})
	joined := strings.Join(args, " ")

	for _, value := range []string{
		"-f pulse",
		"-i browser_stream.monitor",
		"-ac 2",
		"-ar 48000",
		"-c:a libopus",
		"-b:a 96k",
		"-page_duration 1",
		"-f ogg pipe:1",
	} {
		if !strings.Contains(joined, value) {
			t.Fatalf("missing %q in %q", value, joined)
		}
	}
}

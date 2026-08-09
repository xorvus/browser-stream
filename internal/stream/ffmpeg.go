package stream

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"

	"browser-stream/internal/config"
)

// FFmpegArgs builds the x11grab -> VPx -> IVF pipeline for one delivery spec.
//
// Two things here matter far more than anything else at low bitrates:
//
//   - Resolution and frame rate, not the bitrate flag, are what actually make a
//     stream fit in a small budget. libvpx cannot hit 100 kbps at 720p30 in
//     realtime mode; it overshoots to roughly 200 kbps. The saver specs drop to
//     360p15 first and let the rate control do the rest.
//   - Keyframe spacing. A one-second GOP spends a large slice of a 100 kbps
//     budget on intra frames. The specs use 2-4 seconds instead, which trades a
//     slower join for materially better steady-state quality.
func FFmpegArgs(spec config.StreamSpec) []string {
	bitrate := strconv.Itoa(spec.VideoKbps) + "k"

	args := []string{
		"-hide_banner",
		"-loglevel", "error",
		"-f", "x11grab",
		"-framerate", strconv.Itoa(spec.CaptureFPS),
		"-video_size", fmt.Sprintf("%dx%d", spec.CaptureWidth, spec.CaptureHeight),
		"-i", ":99.0",
		"-an",
	}

	// Saver specs downscale the same capture region rather than resizing the
	// browser window, so one viewer switching to data-saver cannot change what
	// every other viewer sees.
	if scale := scaleFilter(spec); scale != "" {
		args = append(args, "-vf", scale)
	}

	args = append(args,
		"-pix_fmt", "yuv420p",
		"-c:v", encoderName(spec.Codec),
		"-deadline", "realtime",
		"-cpu-used", strconv.Itoa(spec.CPUUsed),
		"-threads", strconv.Itoa(spec.Threads),
		"-lag-in-frames", "0",
		"-error-resilient", "1",
		"-drop-threshold", "30",
	)
	args = append(args, contentTuning(spec.Codec)...)
	return append(args,
		"-b:v", bitrate,
		"-maxrate", bitrate,
		"-bufsize", bitrate,
		"-g", strconv.Itoa(spec.GOPFrames()),
		"-f", "ivf",
		"pipe:1",
	)
}

func scaleFilter(spec config.StreamSpec) string {
	if spec.Height <= 0 || spec.Height >= spec.CaptureHeight {
		return ""
	}
	// -2 keeps the capture aspect ratio and rounds the width to an even number,
	// so viewer-side pointer mapping against the capture geometry stays valid.
	return fmt.Sprintf("scale=-2:%d:flags=bilinear", spec.Height)
}

func encoderName(codec config.VideoCodec) string {
	if codec == config.CodecVP9 {
		return "libvpx-vp9"
	}
	return "libvpx"
}

func contentTuning(codec config.VideoCodec) []string {
	if codec == config.CodecVP9 {
		return []string{"-tune-content", "screen", "-row-mt", "1", "-tile-columns", "1", "-aq-mode", "0"}
	}
	return []string{"-screen-content-mode", "1"}
}

func Start(ctx context.Context, spec config.StreamSpec) (io.ReadCloser, func() error, error) {
	return startFFmpeg(ctx, FFmpegArgs(spec), "video")
}

// AudioFFmpegArgs captures the PulseAudio monitor as Ogg/Opus. Saver specs use
// a mono, low bitrate stream; an opus/48000/2 RTP track carries mono packets
// without any special handling on the viewer side.
func AudioFFmpegArgs(spec config.AudioSpec) []string {
	channels := spec.Channels
	if channels != 1 {
		channels = 2
	}
	application := spec.Application
	if application == "" {
		application = "lowdelay"
	}
	frameMillis := spec.FrameMillis
	if frameMillis <= 0 {
		frameMillis = 20
	}
	return []string{
		"-hide_banner",
		"-loglevel", "error",
		"-f", "pulse",
		"-i", "browser_stream.monitor",
		"-ac", strconv.Itoa(channels),
		"-ar", "48000",
		"-c:a", "libopus",
		"-application", application,
		"-frame_duration", strconv.Itoa(frameMillis),
		"-b:a", strconv.Itoa(spec.Kbps) + "k",
		// One Opus packet per Ogg page keeps the reader's one-page-one-sample
		// assumption true.
		"-page_duration", "1",
		"-f", "ogg",
		"pipe:1",
	}
}

func StartAudio(ctx context.Context, spec config.AudioSpec) (io.ReadCloser, func() error, error) {
	return startFFmpeg(ctx, AudioFFmpegArgs(spec), "audio")
}

func startFFmpeg(ctx context.Context, args []string, kind string) (io.ReadCloser, func() error, error) {
	command := exec.CommandContext(ctx, "ffmpeg", args...)
	command.Stderr = os.Stderr

	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, nil, fmt.Errorf("open %s ffmpeg stdout: %w", kind, err)
	}
	if err := command.Start(); err != nil {
		return nil, nil, fmt.Errorf("start %s ffmpeg: %w", kind, err)
	}
	return stdout, command.Wait, nil
}

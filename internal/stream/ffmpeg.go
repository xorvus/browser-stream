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

func FFmpegArgs(cfg config.Config) []string {
	outputWidth, outputHeight, outputFPS := cfg.VideoOutput()
	args := []string{
		"-hide_banner",
		"-loglevel", "error",
		"-f", "x11grab",
		"-framerate", strconv.Itoa(outputFPS),
		"-video_size", fmt.Sprintf("%dx%d", outputWidth, outputHeight),
		"-i", ":99.0",
		"-an",
	}
	return append(args,
		"-pix_fmt", "yuv420p",
		"-c:v", "libvpx",
		"-deadline", "realtime",
		"-cpu-used", "16",
		"-threads", "4",
		"-lag-in-frames", "0",
		"-drop-threshold", "30",
		"-screen-content-mode", "1",
		"-b:v", cfg.Bitrate,
		"-maxrate", cfg.Bitrate,
		"-bufsize", cfg.Bitrate,
		"-g", strconv.Itoa(outputFPS),
		"-f", "ivf",
		"pipe:1",
	)
}

func Start(ctx context.Context, cfg config.Config) (io.ReadCloser, func() error, error) {
	command := exec.CommandContext(ctx, "ffmpeg", FFmpegArgs(cfg)...)
	command.Stderr = os.Stderr

	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, nil, fmt.Errorf("open ffmpeg stdout: %w", err)
	}
	if err := command.Start(); err != nil {
		return nil, nil, fmt.Errorf("start ffmpeg: %w", err)
	}
	return stdout, command.Wait, nil
}

func AudioFFmpegArgs(cfg config.Config) []string {
	return []string{
		"-hide_banner",
		"-loglevel", "error",
		"-f", "pulse",
		"-i", "browser_stream.monitor",
		"-ac", "2",
		"-ar", "48000",
		"-c:a", "libopus",
		"-application", "lowdelay",
		"-b:a", cfg.AudioBitrate,
		"-page_duration", "1",
		"-f", "ogg",
		"pipe:1",
	}
}

func StartAudio(ctx context.Context, cfg config.Config) (io.ReadCloser, func() error, error) {
	command := exec.CommandContext(ctx, "ffmpeg", AudioFFmpegArgs(cfg)...)
	command.Stderr = os.Stderr

	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, nil, fmt.Errorf("open audio ffmpeg stdout: %w", err)
	}
	if err := command.Start(); err != nil {
		return nil, nil, fmt.Errorf("start audio ffmpeg: %w", err)
	}
	return stdout, command.Wait, nil
}

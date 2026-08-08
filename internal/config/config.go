package config

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

const (
	defaultBrowserURL   = "https://google.com"
	defaultWidth        = 1920
	defaultHeight       = 1080
	defaultFPS          = 60
	defaultBitrate      = "6000k"
	defaultAudioBitrate = "32k"
	defaultVideoProfile = VideoProfile720p60
	defaultICEHost      = "127.0.0.1"
	defaultUDPPortMin   = 50000
	defaultUDPPortMax   = 50010
)

type VideoProfile string

const (
	VideoProfile1080p60 VideoProfile = "1080p60"
	VideoProfile1080p30 VideoProfile = "1080p30"
	VideoProfile720p60  VideoProfile = "720p60"
	VideoProfile720p30  VideoProfile = "720p30"
)

func ParseVideoProfile(raw string) (VideoProfile, error) {
	profile := VideoProfile(strings.ToLower(strings.TrimSpace(raw)))
	switch profile {
	case VideoProfile1080p60, VideoProfile1080p30, VideoProfile720p60, VideoProfile720p30:
		return profile, nil
	default:
		return "", fmt.Errorf("VIDEO_PROFILE must be one of 1080p60, 1080p30, 720p60, or 720p30")
	}
}

func (p VideoProfile) Dimensions() (width, height, fps int) {
	switch p {
	case VideoProfile1080p30:
		return 1920, 1080, 30
	case VideoProfile720p60:
		return 1280, 720, 60
	case VideoProfile720p30:
		return 1280, 720, 30
	default:
		return 1920, 1080, 60
	}
}

type Config struct {
	BrowserURL   string
	Width        int
	Height       int
	FPS          int
	Bitrate      string
	AudioBitrate string
	Profile      VideoProfile
	ICEHost      string
	UDPPortMin   int
	UDPPortMax   int
}

func FromEnv(lookup func(string) string) (Config, error) {
	browserURL := valueOrDefault(lookup("BROWSER_URL"), defaultBrowserURL)
	parsedURL, err := url.Parse(browserURL)
	if err != nil || parsedURL.Host == "" || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") {
		return Config{}, fmt.Errorf("BROWSER_URL must be an absolute HTTP(S) URL")
	}

	width, err := positiveInt("VIDEO_WIDTH", valueOrDefault(lookup("VIDEO_WIDTH"), strconv.Itoa(defaultWidth)))
	if err != nil {
		return Config{}, err
	}
	height, err := positiveInt("VIDEO_HEIGHT", valueOrDefault(lookup("VIDEO_HEIGHT"), strconv.Itoa(defaultHeight)))
	if err != nil {
		return Config{}, err
	}
	fps, err := positiveInt("VIDEO_FPS", valueOrDefault(lookup("VIDEO_FPS"), strconv.Itoa(defaultFPS)))
	if err != nil {
		return Config{}, err
	}

	bitrate := strings.TrimSpace(valueOrDefault(lookup("VIDEO_BITRATE"), defaultBitrate))
	if bitrate == "" {
		return Config{}, fmt.Errorf("VIDEO_BITRATE must not be empty")
	}
	audioBitrate := strings.TrimSpace(valueOrDefault(lookup("AUDIO_BITRATE"), defaultAudioBitrate))
	if audioBitrate == "" {
		return Config{}, fmt.Errorf("AUDIO_BITRATE must not be empty")
	}
	profile, err := ParseVideoProfile(valueOrDefault(lookup("VIDEO_PROFILE"), string(defaultVideoProfile)))
	if err != nil {
		return Config{}, err
	}
	iceHost := valueOrDefault(lookup("WEBRTC_ICE_HOST"), defaultICEHost)
	if net.ParseIP(iceHost) == nil {
		return Config{}, fmt.Errorf("WEBRTC_ICE_HOST must be an IP address")
	}
	udpPortMin, err := udpPort("WEBRTC_UDP_PORT_MIN", valueOrDefault(lookup("WEBRTC_UDP_PORT_MIN"), strconv.Itoa(defaultUDPPortMin)))
	if err != nil {
		return Config{}, err
	}
	udpPortMax, err := udpPort("WEBRTC_UDP_PORT_MAX", valueOrDefault(lookup("WEBRTC_UDP_PORT_MAX"), strconv.Itoa(defaultUDPPortMax)))
	if err != nil {
		return Config{}, err
	}
	if udpPortMin > udpPortMax {
		return Config{}, fmt.Errorf("WEBRTC_UDP_PORT_MIN must not exceed WEBRTC_UDP_PORT_MAX")
	}

	return Config{
		BrowserURL: browserURL, Width: width, Height: height, FPS: fps, Bitrate: bitrate, AudioBitrate: audioBitrate,
		Profile: profile, ICEHost: iceHost, UDPPortMin: udpPortMin, UDPPortMax: udpPortMax,
	}, nil
}

func (c Config) VideoOutput() (width, height, fps int) {
	if c.Profile == "" {
		return c.Width, c.Height, c.FPS
	}
	width, height, fps = c.Profile.Dimensions()
	if c.Width > 0 && width > c.Width {
		width = c.Width
	}
	if c.Height > 0 && height > c.Height {
		height = c.Height
	}
	if c.FPS > 0 && fps > c.FPS {
		fps = c.FPS
	}
	return width, height, fps
}

func udpPort(name, raw string) (int, error) {
	value, err := positiveInt(name, raw)
	if err != nil || value > 65535 {
		return 0, fmt.Errorf("%s must be between 1 and 65535", name)
	}
	return value, nil
}

func positiveInt(name, raw string) (int, error) {
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", name)
	}
	return value, nil
}

func valueOrDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

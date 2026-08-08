package config

import "testing"

func TestFromEnvUsesDefaults(t *testing.T) {
	got, err := FromEnv(func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}

	want := Config{
		BrowserURL:   "https://google.com",
		Width:        1920,
		Height:       1080,
		FPS:          60,
		Bitrate:      "6000k",
		AudioBitrate: "32k",
		Profile:      VideoProfile720p60,
		ICEHost:      "127.0.0.1",
		UDPPortMin:   50000,
		UDPPortMax:   50010,
	}
	if got != want {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestFromEnvRejectsNonPositiveVideoDimensions(t *testing.T) {
	_, err := FromEnv(func(key string) string {
		if key == "VIDEO_WIDTH" {
			return "0"
		}
		return ""
	})
	if err == nil {
		t.Fatal("expected VIDEO_WIDTH=0 to be rejected")
	}
}

func TestFromEnvRejectsNonPositiveFrameRate(t *testing.T) {
	_, err := FromEnv(func(key string) string {
		if key == "VIDEO_FPS" {
			return "0"
		}
		return ""
	})
	if err == nil {
		t.Fatal("expected VIDEO_FPS=0 to be rejected")
	}
}

func TestFromEnvRejectsUnsupportedBrowserURL(t *testing.T) {
	_, err := FromEnv(func(key string) string {
		if key == "BROWSER_URL" {
			return "file:///etc/passwd"
		}
		return ""
	})
	if err == nil {
		t.Fatal("expected non-HTTP URL to be rejected")
	}
}

func TestFromEnvReadsConfiguredValues(t *testing.T) {
	env := map[string]string{
		"BROWSER_URL":         "https://example.test/demo",
		"VIDEO_WIDTH":         "1280",
		"VIDEO_HEIGHT":        "720",
		"VIDEO_FPS":           "30",
		"VIDEO_BITRATE":       "2500k",
		"AUDIO_BITRATE":       "96k",
		"VIDEO_PROFILE":       "720p30",
		"WEBRTC_ICE_HOST":     "192.0.2.20",
		"WEBRTC_UDP_PORT_MIN": "40000",
		"WEBRTC_UDP_PORT_MAX": "40020",
	}
	got, err := FromEnv(func(key string) string { return env[key] })
	if err != nil {
		t.Fatal(err)
	}
	want := Config{
		BrowserURL: "https://example.test/demo", Width: 1280, Height: 720, FPS: 30, Bitrate: "2500k", AudioBitrate: "96k",
		Profile: VideoProfile720p30, ICEHost: "192.0.2.20", UDPPortMin: 40000, UDPPortMax: 40020,
	}
	if got != want {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestParseVideoProfileSupportsSharedQualityProfiles(t *testing.T) {
	tests := map[string]VideoProfile{
		"1080p60": VideoProfile1080p60,
		"1080p30": VideoProfile1080p30,
		"720p60":  VideoProfile720p60,
		"720p30":  VideoProfile720p30,
	}
	for raw, want := range tests {
		got, err := ParseVideoProfile(raw)
		if err != nil {
			t.Fatalf("ParseVideoProfile(%q): %v", raw, err)
		}
		if got != want {
			t.Fatalf("ParseVideoProfile(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestFromEnvRejectsUnknownVideoProfile(t *testing.T) {
	_, err := FromEnv(func(key string) string {
		if key == "VIDEO_PROFILE" {
			return "4k120"
		}
		return ""
	})
	if err == nil {
		t.Fatal("expected unsupported VIDEO_PROFILE to be rejected")
	}
}

func TestFromEnvRejectsInvertedWebRTCPortRange(t *testing.T) {
	_, err := FromEnv(func(key string) string {
		switch key {
		case "WEBRTC_UDP_PORT_MIN":
			return "50010"
		case "WEBRTC_UDP_PORT_MAX":
			return "50000"
		default:
			return ""
		}
	})
	if err == nil {
		t.Fatal("expected an inverted UDP port range to be rejected")
	}
}

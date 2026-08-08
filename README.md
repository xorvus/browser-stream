# Browser Stream

Streams one Brave tab with audio to multiple WebRTC viewers.

## Requirements

- Docker
- Make

## Start

```bash
make build
make run
make smoke
```

Open `http://localhost:8080` and select **Start stream**.

```bash
make logs
make stop
make clean
```

`make clean` removes the container and image. The Brave profile volume is kept.

## Configuration

Pass overrides to `make run`, for example:

```bash
make run VIDEO_PROFILE=1080p30 WEBRTC_ICE_HOST=100.124.160.53
```

| Variable | Default |
| --- | --- |
| `BROWSER_URL` | `https://google.com` |
| `VIDEO_WIDTH` | `1920` |
| `VIDEO_HEIGHT` | `1080` |
| `VIDEO_FPS` | `60` |
| `VIDEO_BITRATE` | `6000k` |
| `VIDEO_PROFILE` | `720p60` |
| `AUDIO_BITRATE` | `32k` |
| `BRAVE_PROFILE_VOLUME` | `browser-stream-brave-profile` |
| `WEBRTC_ICE_HOST` | Tailscale IPv4 when available, otherwise `127.0.0.1` |
| `WEBRTC_UDP_PORT_MIN` | `50000` |
| `WEBRTC_UDP_PORT_MAX` | `50010` |

Supported profiles are `1080p60`, `1080p30`, `720p60`, and `720p30`. A quality change applies to every viewer.

## Remote access

Set `WEBRTC_ICE_HOST` to an address reachable by the viewer. The Makefile publishes the configured UDP range.

An HTTP tunnel carries the page and signaling but not the WebRTC UDP media path. Public internet access requires a reachable UDP address or TURN.

## Widevine

Widevine is not included. Place a legally obtained bundle in `./widevine`:

```text
widevine/
├── manifest.json
└── _platform_specific/
    └── linux_arm64/
        └── libwidevinecdm.so
```

Use `linux_x64` instead of `linux_arm64` for x86-64.

```bash
make widevine-status
make run
```

Set `WIDEVINE_DIR=/absolute/path` when the bundle is stored elsewhere. A valid bundle is mounted read-only and registered in the persistent Brave profile. ARM64 uses a ChromeOS ARM64 user agent for Netflix compatibility. Set `BROWSER_USER_AGENT` to override it, or set it to an empty value to use Brave's native user agent.

## Checks

```bash
make test
make smoke
curl http://localhost:8080/stats
```

If remote video does not connect, check `WEBRTC_ICE_HOST` and the UDP range. If playback stutters, use `720p30` or allocate more CPU to Docker. If protected content fails, run `make widevine-status` and check the container logs.

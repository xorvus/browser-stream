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
| `VIDEO_BITRATE` | `6000k` (ceiling, not a target) |
| `VIDEO_PROFILE` | `720p60` |
| `AUDIO_BITRATE` | `32k` (ceiling, not a target) |
| `BRAVE_PROFILE_VOLUME` | `browser-stream-brave-profile` |
| `WEBRTC_ICE_HOST` | Tailscale IPv4 when available, otherwise `127.0.0.1` |
| `WEBRTC_UDP_PORT_MIN` | `50000` |
| `WEBRTC_UDP_PORT_MAX` | `50010` |

Supported profiles are `1080p60`, `1080p30`, `720p60`, and `720p30`. A quality change applies to every viewer, because it resizes the browser window that is being captured.

Each profile now carries its own bitrate rather than encoding everything at `VIDEO_BITRATE`:

| Capture profile | Video bitrate |
| --- | --- |
| `1080p60` | 6000 kbps |
| `1080p30` | 4000 kbps |
| `720p60` | 3000 kbps |
| `720p30` | 1800 kbps |

`VIDEO_BITRATE` and `AUDIO_BITRATE` act as ceilings. Setting `VIDEO_BITRATE=1200k` caps every profile at 1200 kbps; it never raises a profile above its own budget.

## Data saver

Each viewer picks its own delivery profile from the **Data saver** control. The choice is per viewer: a phone on a metered connection can run at 360p while other viewers keep the full stream. A saver viewer gets its own encoder, downscaled from the same capture, and that encoder only runs while somebody is using it.

| Data saver | Output | Codec | Video | Audio | Total on the wire |
| --- | --- | --- | --- | --- | --- |
| Off | capture geometry | VP8 | profile ladder | 32 kbps stereo | — |
| On | 480p 20 FPS | VP9 | 350 kbps | 24 kbps stereo | ≈ 400 kbps |
| Max | 360p 15 FPS | VP9 | 70 kbps | 16 kbps mono | ≈ 94 kbps |

The totals include RTP, UDP and IP headers. At these rates the header cost is not negligible, which is why the 360p profile uses 60 ms Opus frames: 20 ms frames would add roughly 16 kbps of headers on top of 16 kbps of audio.

Viewers whose browser does not offer VP9 are served VP8 at the same bitrate, so the budget holds and only the picture quality drops. Browsers that report `navigator.connection.saveData`, or a 2G/3G connection, start in the 360p profile automatically.

Saver profiles use a four second keyframe interval. A viewer joining an encoder that is already running waits for the next keyframe before the picture appears, which avoids showing corruption and keeps intra frames from eating the budget.

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

`/stats` reports one entry per running encoder under `video.pipelines`, plus the advertised cost of every delivery profile under `streamProfiles`.

If remote video does not connect, check `WEBRTC_ICE_HOST` and the UDP range. If playback stutters, use `720p30`, switch that viewer to a data-saver profile, or allocate more CPU to Docker. If protected content fails, run `make widevine-status` and check the container logs.

#!/bin/bash

set -e

export DISPLAY=:99
: "${BROWSER_URL:=https://google.com}"
: "${BRAVE_USER_DATA_DIR:=/var/lib/browser-stream/brave-profile}"
: "${VIDEO_WIDTH:=1920}"
: "${VIDEO_HEIGHT:=1080}"
: "${VIDEO_FPS:=60}"
: "${VIDEO_BITRATE:=6000k}"
: "${VIDEO_PROFILE:=720p60}"
: "${AUDIO_BITRATE:=32k}"
: "${WEBRTC_ICE_HOST:=127.0.0.1}"
: "${WEBRTC_UDP_PORT_MIN:=50000}"
: "${WEBRTC_UDP_PORT_MAX:=50010}"
if test "${BROWSER_USER_AGENT+x}" = x; then
  BROWSER_USER_AGENT_CONFIGURED=1
else
  BROWSER_USER_AGENT_CONFIGURED=0
  BROWSER_USER_AGENT=
fi
export BROWSER_URL BRAVE_USER_DATA_DIR BROWSER_USER_AGENT VIDEO_WIDTH VIDEO_HEIGHT VIDEO_FPS VIDEO_BITRATE VIDEO_PROFILE AUDIO_BITRATE WEBRTC_ICE_HOST WEBRTC_UDP_PORT_MIN WEBRTC_UDP_PORT_MAX

. /usr/local/lib/browser-stream/brave.sh
. /usr/local/lib/browser-stream/widevine.sh

case "$VIDEO_PROFILE" in
  720p60|720p30)
    BROWSER_VIEWPORT_WIDTH=1280
    BROWSER_VIEWPORT_HEIGHT=720
    ;;
  *)
    BROWSER_VIEWPORT_WIDTH="$VIDEO_WIDTH"
    BROWSER_VIEWPORT_HEIGHT="$VIDEO_HEIGHT"
    ;;
esac

export PULSE_SINK=browser_stream
pulseaudio --daemonize=yes --exit-idle-time=-1
pactl load-module module-null-sink sink_name="$PULSE_SINK" rate=48000 channels=2 channel_map=front-left,front-right >/dev/null

Xvfb :99 \
  -screen 0 "${VIDEO_WIDTH}x${VIDEO_HEIGHT}x24" \
  -ac \
  -noreset &

for _ in $(seq 1 20); do
  if xdpyinfo -display :99 >/dev/null 2>&1; then
    break
  fi
  sleep 0.1
done

mkdir -p "$BRAVE_USER_DATA_DIR"
rm -f \
  "$BRAVE_USER_DATA_DIR/SingletonLock" \
  "$BRAVE_USER_DATA_DIR/SingletonCookie" \
  "$BRAVE_USER_DATA_DIR/SingletonSocket"

WIDEVINE_ROOT=/opt/brave.com/brave/WidevineCdm
RUNTIME_ARCH=$(uname -m)
if WIDEVINE_BUNDLE=$(widevine_bundle_path "$WIDEVINE_ROOT" "$RUNTIME_ARCH"); then
  widevine_enable_local_state "$BRAVE_USER_DATA_DIR/Local State"
  widevine_register_component "$BRAVE_USER_DATA_DIR" "$WIDEVINE_BUNDLE"
  echo "widevine: ready for $RUNTIME_ARCH at $WIDEVINE_BUNDLE"
  echo "widevine: protected content enabled and registered in the active Brave profile"
  case "$RUNTIME_ARCH" in
    aarch64|arm64)
      if test "$BROWSER_USER_AGENT_CONFIGURED" -eq 0; then
        BROWSER_USER_AGENT=$(brave_chromeos_arm64_user_agent \
          "$(brave-browser --version 2>/dev/null || true)")
        export BROWSER_USER_AGENT
        echo "browser identity: ChromeOS ARM64 compatibility enabled"
      fi
      ;;
  esac
else
  echo "widevine: unavailable for $RUNTIME_ARCH; normal sites still work, DRM requires WIDEVINE_DIR with manifest.json and the matching libwidevinecdm.so" >&2
fi

mapfile -t BRAVE_ARGS < <(brave_launch_args \
  "$BRAVE_USER_DATA_DIR" \
  "$BROWSER_VIEWPORT_WIDTH" \
  "$BROWSER_VIEWPORT_HEIGHT" \
  "$BROWSER_USER_AGENT")

brave-browser "${BRAVE_ARGS[@]}" "$BROWSER_URL" &

for _ in $(seq 1 30); do
  if xdotool search --onlyvisible --class browser-stream windowsize %@ "$BROWSER_VIEWPORT_WIDTH" "$BROWSER_VIEWPORT_HEIGHT" >/dev/null 2>&1; then
    break
  fi
  sleep 0.1
done

exec /app/browser-stream

#!/bin/bash

brave_chromeos_arm64_user_agent() {
  local version_output=$1
  local chromium_major

  chromium_major=$(printf '%s\n' "$version_output" | sed -nE \
    's/^[^0-9]*([0-9]+)\..*$/\1/p')
  case "$chromium_major" in
    ''|*[!0-9]*)
      chromium_major=151
      ;;
  esac

  printf 'Mozilla/5.0 (X11; CrOS aarch64 16093.68.0) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/%s.0.0.0 Safari/537.36\n' \
    "$chromium_major"
}

brave_launch_args() {
  local profile_dir=$1
  local viewport_width=$2
  local viewport_height=$3
  local user_agent=${4-}

  # Xvfb has no window manager, so Chromium's occlusion detection can decide
  # the window is hidden and stop producing frames. The capture then shows a
  # blank page until something forces a repaint. The four disable flags below
  # keep the renderer painting and its timers running regardless of what
  # Chromium believes about visibility.
  printf '%s\n' \
    --remote-debugging-address=127.0.0.1 \
    --remote-debugging-port=9222 \
    --class=browser-stream \
    --no-sandbox \
    --no-first-run \
    --no-default-browser-check \
    --disable-dev-shm-usage \
    --use-gl=swiftshader \
    --ignore-gpu-blocklist \
    --autoplay-policy=no-user-gesture-required \
    --disable-backgrounding-occluded-windows \
    --disable-renderer-backgrounding \
    --disable-background-timer-throttling \
    --disable-features=CalculateNativeWinOcclusion \
    "--user-data-dir=$profile_dir" \
    "--window-size=$viewport_width,$viewport_height" \
    --window-position=0,0

  if test -n "$user_agent"; then
    printf '%s\n' "--user-agent=$user_agent"
  fi
}

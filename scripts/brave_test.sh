#!/bin/bash

set -eu

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
. "$SCRIPT_DIR/brave.sh"

mapfile -t args < <(brave_launch_args /var/lib/browser-stream/brave-profile 1280 720)

has_arg() {
  local expected=$1
  local arg
  for arg in "${args[@]}"; do
    if test "$arg" = "$expected"; then
      return 0
    fi
  done
  echo "missing Brave argument: $expected" >&2
  exit 1
}

has_arg --autoplay-policy=no-user-gesture-required
has_arg --user-data-dir=/var/lib/browser-stream/brave-profile
has_arg --window-size=1280,720
has_arg --remote-debugging-address=127.0.0.1
has_arg --remote-debugging-port=9222
has_arg --class=browser-stream

for arg in "${args[@]}"; do
  case "$arg" in
    --user-agent=*)
      echo "native Brave launch unexpectedly overrides the User-Agent" >&2
      exit 1
      ;;
  esac
done

chromeos_user_agent='Mozilla/5.0 (X11; CrOS aarch64 16093.68.0) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/151.0.0.0 Safari/537.36'
mapfile -t chromeos_args < <(brave_launch_args \
  /var/lib/browser-stream/brave-profile 1280 720 "$chromeos_user_agent")

user_agent_matches=0
for arg in "${chromeos_args[@]}"; do
  if test "$arg" = "--user-agent=$chromeos_user_agent"; then
    user_agent_matches=$((user_agent_matches + 1))
  fi
done

if test "$user_agent_matches" -ne 1; then
  echo "configured Brave launch must contain exactly one User-Agent override" >&2
  exit 1
fi

if ! declare -F brave_chromeos_arm64_user_agent >/dev/null; then
  echo "missing ChromeOS ARM64 User-Agent derivation" >&2
  exit 1
fi

derived_user_agent=$(brave_chromeos_arm64_user_agent 'Brave Browser 151.1.93.134')
case "$derived_user_agent" in
  *'CrOS aarch64 '*'Chrome/151.0.0.0'*)
    ;;
  *)
    echo "ChromeOS User-Agent did not use the running Brave major version" >&2
    exit 1
    ;;
esac

fallback_user_agent=$(brave_chromeos_arm64_user_agent 'unparseable version')
case "$fallback_user_agent" in
  *'Chrome/151.0.0.0'*)
    ;;
  *)
    echo "ChromeOS User-Agent did not use the defensive major-version fallback" >&2
    exit 1
    ;;
esac

echo "brave launch argument tests passed"

#!/bin/bash

set -eu

PROJECT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
TEST_TMP=$(mktemp -d)
trap 'rm -rf "$TEST_TMP"' EXIT
mkdir -p "$TEST_TMP/widevine"

without_drm=$(make --no-print-directory -n -C "$PROJECT_DIR" run \
  BRAVE_PROFILE_VOLUME=test-brave-profile \
  WIDEVINE_ARCH=aarch64 WIDEVINE_SEARCH_DIRS="$TEST_TMP/missing")

case "$without_drm" in
  *"--hostname browser-stream"*) ;;
  *) echo "run command is missing the stable hostname" >&2; exit 1 ;;
esac
case "$without_drm" in
  *"test-brave-profile:/var/lib/browser-stream/brave-profile"*) ;;
  *) echo "run command is missing the persistent Brave profile" >&2; exit 1 ;;
esac
case "$without_drm" in
  *"WidevineCdm"*) echo "empty WIDEVINE_DIR unexpectedly creates a DRM mount" >&2; exit 1 ;;
esac
case "$without_drm" in
  *"BROWSER_USER_AGENT"*) echo "unset User-Agent unexpectedly creates a container override" >&2; exit 1 ;;
esac

with_user_agent=$(make --no-print-directory -n -C "$PROJECT_DIR" run \
  BRAVE_PROFILE_VOLUME=test-brave-profile \
  WIDEVINE_ARCH=aarch64 WIDEVINE_SEARCH_DIRS="$TEST_TMP/missing" \
  BROWSER_USER_AGENT=test-agent)
case "$with_user_agent" in
  *'--env BROWSER_USER_AGENT="test-agent"'*) ;;
  *) echo "explicit User-Agent is missing from the container environment" >&2; exit 1 ;;
esac

with_drm=$(make --no-print-directory -n -C "$PROJECT_DIR" run \
  BRAVE_PROFILE_VOLUME=test-brave-profile WIDEVINE_DIR="$TEST_TMP/widevine")

case "$with_drm" in
  *"$TEST_TMP/widevine:/opt/brave.com/brave/WidevineCdm:ro"*) ;;
  *) echo "run command is missing the read-only Widevine mount" >&2; exit 1 ;;
esac

auto_bundle="$TEST_TMP/auto-widevine"
mkdir -p "$auto_bundle/_platform_specific/linux_arm64"
printf '{}\n' >"$auto_bundle/manifest.json"
printf 'test library\n' >"$auto_bundle/_platform_specific/linux_arm64/libwidevinecdm.so"

auto_detected=$(make --no-print-directory -n -C "$PROJECT_DIR" run \
  BRAVE_PROFILE_VOLUME=test-brave-profile \
  WIDEVINE_ARCH=aarch64 WIDEVINE_SEARCH_DIRS="$auto_bundle")
case "$auto_detected" in
  *"$auto_bundle:/opt/brave.com/brave/WidevineCdm:ro"*) ;;
  *) echo "complete ARM64 bundle was not auto-detected" >&2; exit 1 ;;
esac

wrong_arch=$(make --no-print-directory -n -C "$PROJECT_DIR" run \
  BRAVE_PROFILE_VOLUME=test-brave-profile \
  WIDEVINE_ARCH=x86_64 WIDEVINE_SEARCH_DIRS="$auto_bundle")
case "$wrong_arch" in
  *"WidevineCdm"*) echo "ARM64 bundle was selected for an x86-64 runtime" >&2; exit 1 ;;
esac

status=$(make --no-print-directory -C "$PROJECT_DIR" widevine-status \
  WIDEVINE_ARCH=aarch64 WIDEVINE_SEARCH_DIRS="$auto_bundle")
case "$status" in
  *"linux_arm64"*"$auto_bundle"*) ;;
  *) echo "widevine-status did not report the detected ARM64 bundle" >&2; exit 1 ;;
esac

echo "make run configuration tests passed"

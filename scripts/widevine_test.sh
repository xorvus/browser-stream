#!/bin/bash

set -eu

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
. "$SCRIPT_DIR/widevine.sh"

TEST_TMP=$(mktemp -d)
trap 'rm -rf "$TEST_TMP"' EXIT

create_bundle() {
  local root=$1
  local platform=$2
  mkdir -p "$root/_platform_specific/$platform"
  printf '{"version":"test"}\n' >"$root/manifest.json"
  printf 'test library\n' >"$root/_platform_specific/$platform/libwidevinecdm.so"
}

expect_valid() {
  local architecture=$1
  local platform=$2
  local root="$TEST_TMP/$architecture-valid"
  create_bundle "$root" "$platform"
  local actual
  actual=$(widevine_bundle_path "$root" "$architecture")
  test "$actual" = "$root"
}

expect_invalid() {
  local name=$1
  local architecture=$2
  local root="$TEST_TMP/$name"
  mkdir -p "$root"
  if widevine_bundle_path "$root" "$architecture" >/dev/null 2>&1; then
    echo "expected $name bundle to be rejected" >&2
    exit 1
  fi
}

expect_valid aarch64 linux_arm64
expect_valid arm64 linux_arm64
expect_valid x86_64 linux_x64
expect_valid amd64 linux_x64

missing_library="$TEST_TMP/missing-library"
mkdir -p "$missing_library"
printf '{}\n' >"$missing_library/manifest.json"
expect_invalid missing-library aarch64

missing_manifest="$TEST_TMP/missing-manifest"
mkdir -p "$missing_manifest/_platform_specific/linux_arm64"
printf 'test library\n' >"$missing_manifest/_platform_specific/linux_arm64/libwidevinecdm.so"
expect_invalid missing-manifest aarch64

expect_invalid unsupported riscv64

existing_state="$TEST_TMP/existing-profile/Local State"
mkdir -p "$(dirname "$existing_state")"
printf '{"unrelated":"keep","brave":{"other":7}}\n' >"$existing_state"
widevine_enable_local_state "$existing_state"
jq -e '
  .unrelated == "keep" and
  .brave.other == 7 and
  .brave.widevine_opted_in == true
' "$existing_state" >/dev/null

new_state="$TEST_TMP/new-profile/Local State"
widevine_enable_local_state "$new_state"
widevine_enable_local_state "$new_state"
jq -e '.brave.widevine_opted_in == true' "$new_state" >/dev/null

component_profile="$TEST_TMP/component-profile"
component_bundle=/opt/brave.com/brave/WidevineCdm
widevine_register_component "$component_profile" "$component_bundle"
jq -e --arg path "$component_bundle" '.Path == $path' \
  "$component_profile/WidevineCdm/latest-component-updated-widevine-cdm" >/dev/null

echo "widevine validation tests passed"

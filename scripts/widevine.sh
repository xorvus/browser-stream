#!/bin/bash

widevine_platform() {
  case "$1" in
    aarch64|arm64)
      printf '%s\n' linux_arm64
      ;;
    x86_64|amd64)
      printf '%s\n' linux_x64
      ;;
    *)
      return 1
      ;;
  esac
}

widevine_bundle_path() {
  local root=$1
  local architecture=$2
  local platform

  platform=$(widevine_platform "$architecture") || return 1
  test -f "$root/manifest.json" || return 1
  test -f "$root/_platform_specific/$platform/libwidevinecdm.so" || return 1
  printf '%s\n' "$root"
}

widevine_enable_local_state() {
  local state_file=$1
  local state_dir
  local temp_file

  state_dir=$(dirname "$state_file")
  temp_file="${state_file}.browser-stream.tmp"
  mkdir -p "$state_dir"

  if test -s "$state_file"; then
    jq '.brave.widevine_opted_in = true' "$state_file" >"$temp_file"
  else
    jq -n '{brave: {widevine_opted_in: true}}' >"$temp_file"
  fi

  mv "$temp_file" "$state_file"
}

widevine_register_component() {
  local profile_dir=$1
  local bundle_path=$2
  local component_dir="$profile_dir/WidevineCdm"
  local registration_file="$component_dir/latest-component-updated-widevine-cdm"
  local temp_file="${registration_file}.browser-stream.tmp"

  mkdir -p "$component_dir"
  jq -n --arg path "$bundle_path" '{Path: $path}' >"$temp_file"
  mv "$temp_file" "$registration_file"
}

#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 1 ]]; then
  echo "usage: fetch-appimage-tools.sh OUTPUT_DIR" >&2
  exit 2
fi

output_dir=$1
mkdir -p "$output_dir"
output_dir=$(realpath "$output_dir")

appimagetool_url='https://github.com/AppImage/appimagetool/releases/download/1.9.1/appimagetool-x86_64.AppImage'
appimagetool_sha256='ed4ce84f0d9caff66f50bcca6ff6f35aae54ce8135408b3fa33abfc3cb384eb0'
runtime_url='https://github.com/AppImage/type2-runtime/releases/download/continuous/runtime-x86_64'
runtime_sha256='1cc49bcf1e2ccd593c379adb17c9f85a36d619088296504de95b1d06215aebbf'

download_verified() {
  local url=$1
  local expected=$2
  local destination=$3
  local temporary
  temporary=$(mktemp "$output_dir/.download.XXXXXX")
  trap 'rm -f -- "$temporary"' RETURN
  curl --fail --location --proto '=https' --tlsv1.2 \
    --retry 3 --retry-delay 1 --retry-connrefused --silent --show-error \
    --output "$temporary" "$url"
  printf '%s  %s\n' "$expected" "$temporary" | sha256sum --check --status
  chmod 0755 "$temporary"
  mv -f -- "$temporary" "$destination"
  trap - RETURN
}

appimagetool="$output_dir/appimagetool-x86_64.AppImage"
runtime="$output_dir/runtime-x86_64"

if [[ ! -f $appimagetool ]] || ! printf '%s  %s\n' "$appimagetool_sha256" "$appimagetool" | sha256sum --check --status; then
  download_verified "$appimagetool_url" "$appimagetool_sha256" "$appimagetool"
fi
if [[ ! -f $runtime ]] || ! printf '%s  %s\n' "$runtime_sha256" "$runtime" | sha256sum --check --status; then
  download_verified "$runtime_url" "$runtime_sha256" "$runtime"
fi

printf '%s  %s\n' "$appimagetool_sha256" "$appimagetool" | sha256sum --check
printf '%s  %s\n' "$runtime_sha256" "$runtime" | sha256sum --check

#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 1 ]]; then
  echo "usage: fetch-tools.sh OUTPUT_DIR" >&2
  exit 2
fi

output_dir=$1
mkdir -p "$output_dir"
output_dir=$(realpath "$output_dir")
download_root=$(mktemp -d)
trap 'rm -rf -- "$download_root"' EXIT

download_verified() {
  local url=$1
  local expected=$2
  local destination=$3
  curl --fail --location --proto '=https' --tlsv1.2 \
    --retry 3 --retry-delay 1 --retry-connrefused --silent --show-error \
    --output "$destination" "$url"
  printf '%s  %s\n' "$expected" "$destination" | sha256sum --check --status
}

syft_archive="$download_root/syft.tar.gz"
download_verified \
  'https://github.com/anchore/syft/releases/download/v1.51.0/syft_1.51.0_linux_amd64.tar.gz' \
  '2a2e837a2c8d59ec9af5472ee22d3b04ee463c4e44476ecf993fd1e5ab6ebc7f' \
  "$syft_archive"
tar --extract --gzip --file "$syft_archive" --directory "$output_dir" syft

gitleaks_archive="$download_root/gitleaks.tar.gz"
download_verified \
  'https://github.com/gitleaks/gitleaks/releases/download/v8.30.1/gitleaks_8.30.1_linux_x64.tar.gz' \
  '551f6fc83ea457d62a0d98237cbad105af8d557003051f41f3e7ca7b3f2470eb' \
  "$gitleaks_archive"
tar --extract --gzip --file "$gitleaks_archive" --directory "$output_dir" gitleaks

download_verified \
  'https://github.com/google/osv-scanner/releases/download/v2.5.0/osv-scanner_linux_amd64' \
  'edcfc41d257db36148f065055655fe3fcfc434b0b423ea67468a84c207524e0c' \
  "$output_dir/osv-scanner"

chmod 0755 "$output_dir/syft" "$output_dir/gitleaks" "$output_dir/osv-scanner"
"$output_dir/syft" version
"$output_dir/gitleaks" version
"$output_dir/osv-scanner" --version

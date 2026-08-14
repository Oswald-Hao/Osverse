#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 2 ]]; then
  echo "usage: smoke-packages.sh RELEASE_DIR TARGET" >&2
  exit 2
fi

release_dir=$(realpath "$1")
target=$2
if [[ ! $target =~ ^ubuntu(20\.04|22\.04)$ ]] || [[ ! -d $release_dir ]]; then
  echo "invalid smoke-test input" >&2
  exit 2
fi

mapfile -t debs < <(find "$release_dir" -maxdepth 1 -type f -name "osverse_*_amd64_${target}.deb" -print)
mapfile -t appimages < <(find "$release_dir" -maxdepth 1 -type f -name "osverse-*-linux-amd64-${target}.AppImage" -print)
mapfile -t archives < <(find "$release_dir" -maxdepth 1 -type f -name "osverse-*-linux-amd64-${target}.tar.gz" -print)
if [[ ${#debs[@]} -ne 1 || ${#appimages[@]} -ne 1 || ${#archives[@]} -ne 1 ]]; then
  echo "expected exactly one deb, AppImage, and tar archive" >&2
  exit 1
fi

(
  cd "$release_dir"
  sha256sum --check "SHA256SUMS-${target}"
)

deb=${debs[0]}
appimage=${appimages[0]}
archive=${archives[0]}
root_command=()
if [[ $EUID -ne 0 ]]; then
  root_command=(sudo --non-interactive)
fi
temporary_root=$(mktemp -d)
cleanup() {
  "${root_command[@]}" dpkg --purge osverse >/dev/null 2>&1 || true
  rm -rf -- "$temporary_root"
}
trap cleanup EXIT

dpkg-deb --ctrl-tarfile "$deb" > "$temporary_root/control.tar"
tar --list --file "$temporary_root/control.tar" > "$temporary_root/control.list"
dpkg-deb --contents "$deb" > "$temporary_root/deb.list"
tar --list --gzip --file "$archive" > "$temporary_root/archive.list"
if grep -Eq '^\./(preinst|postinst|prerm|postrm)$' "$temporary_root/control.list"; then
  echo "Debian package unexpectedly contains a maintainer script" >&2
  exit 1
fi
grep -Fq './usr/bin/osverse' "$temporary_root/deb.list"
grep -Fq './usr/share/applications/io.github.osverse.Osverse.desktop' "$temporary_root/deb.list"
grep -Fq './usr/share/icons/hicolor/512x512/apps/osverse.png' "$temporary_root/deb.list"
grep -Eq '/osverse$' "$temporary_root/archive.list"
grep -Eq '/LICENSE$' "$temporary_root/archive.list"

"${root_command[@]}" dpkg --install "$deb" >/dev/null
status=$(dpkg-query --show --showformat='${db:Status-Abbrev}' osverse)
[[ $status == 'ii ' ]]
[[ -x /usr/bin/osverse ]]
[[ -f /usr/share/applications/io.github.osverse.Osverse.desktop ]]
"${root_command[@]}" dpkg --remove osverse >/dev/null
[[ ! -e /usr/bin/osverse ]]
[[ ! -e /usr/share/applications/io.github.osverse.Osverse.desktop ]]

log_file="$temporary_root/appimage.log"
xvfb-run --auto-servernum bash -s -- "$appimage" "$log_file" <<'BASH'
set -euo pipefail
appimage=$1
log_file=$2
APPIMAGE_EXTRACT_AND_RUN=1 "$appimage" >"$log_file" 2>&1 &
application_pid=$!
cleanup_application() {
  kill "$application_pid" >/dev/null 2>&1 || true
  wait "$application_pid" >/dev/null 2>&1 || true
}
trap cleanup_application EXIT
for _ in $(seq 1 150); do
  if ! kill -0 "$application_pid" >/dev/null 2>&1; then
    cat "$log_file" >&2
    exit 1
  fi
  if xdotool search --onlyvisible --name '^Osverse$' >/dev/null 2>&1; then
    exit 0
  fi
  sleep 0.1
done
cat "$log_file" >&2
echo "AppImage did not display an Osverse window" >&2
exit 1
BASH

echo "$target package smoke test passed"

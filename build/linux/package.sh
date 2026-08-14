#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 5 ]]; then
  echo "usage: package.sh VERSION BINARY TARGET OUTPUT_DIR APPIMAGE_TOOL_DIR" >&2
  exit 2
fi

package_version=$1
binary_path=$2
target_name=$3
output_dir=$4
appimage_tool_dir=$5

if [[ ! $package_version =~ ^[0-9][0-9A-Za-z.+:~-]*$ ]] ||
   [[ ! $target_name =~ ^ubuntu(20\.04|22\.04)$ ]] ||
   [[ ! -f $binary_path || ! -x $binary_path ]]; then
  echo "invalid package input" >&2
  exit 2
fi

binary_path=$(realpath "$binary_path")
appimage_tool_dir=$(realpath "$appimage_tool_dir")
appimagetool="$appimage_tool_dir/appimagetool-x86_64.AppImage"
appimage_runtime="$appimage_tool_dir/runtime-x86_64"
if [[ ! -f $appimagetool || ! -x $appimagetool || ! -f $appimage_runtime ]]; then
  echo "verified AppImage tools are unavailable" >&2
  exit 2
fi
mkdir -p "$output_dir"
output_dir=$(realpath "$output_dir")
staging_root=$(mktemp -d)
trap 'rm -rf -- "$staging_root"' EXIT

archive_name="osverse-${package_version}-linux-amd64-${target_name}"
archive_root="$staging_root/$archive_name"
install -d -m 0755 "$archive_root"
install -m 0755 "$binary_path" "$archive_root/osverse"
install -m 0644 README.md "$archive_root/README.md"
install -m 0644 LICENSE "$archive_root/LICENSE"
printf '%s\n' "$package_version" > "$archive_root/VERSION"

source_epoch=${SOURCE_DATE_EPOCH:-0}
find "$archive_root" -print0 | xargs -0 touch --date="@$source_epoch"
tar --sort=name --mtime="@$source_epoch" --owner=0 --group=0 --numeric-owner \
  -C "$staging_root" -czf "$output_dir/$archive_name.tar.gz" "$archive_name"

deb_root="$staging_root/deb"
install -d -m 0755 \
  "$deb_root/DEBIAN" \
  "$deb_root/usr/bin" \
  "$deb_root/usr/share/applications" \
  "$deb_root/usr/share/icons/hicolor/512x512/apps" \
  "$deb_root/usr/share/doc/osverse"
install -m 0755 "$binary_path" "$deb_root/usr/bin/osverse"
install -m 0644 build/appicon.png "$deb_root/usr/share/icons/hicolor/512x512/apps/osverse.png"
install -m 0644 README.md "$deb_root/usr/share/doc/osverse/README.md"
install -m 0644 LICENSE "$deb_root/usr/share/doc/osverse/copyright"

cat > "$deb_root/DEBIAN/control" <<EOF
Package: osverse
Version: $package_version
Section: devel
Priority: optional
Architecture: amd64
Maintainer: Osverse Project <noreply@github.com>
Depends: ca-certificates, gnupg, libgtk-3-0, libwebkit2gtk-4.0-37, policykit-1
Homepage: https://github.com/Oswald-Hao/Osverse
Description: Local-first AI development environment manager
 Osverse detects, installs, updates, and configures supported AI development
 tools on Ubuntu with explicit plans and rollback-safe user-level transactions.
EOF

cat > "$deb_root/usr/share/applications/io.github.osverse.Osverse.desktop" <<'EOF'
[Desktop Entry]
Type=Application
Name=Osverse
Comment=Manage local AI development tools
Exec=/usr/bin/osverse
Icon=osverse
Terminal=false
Categories=Development;Utility;
StartupNotify=true
EOF

chmod 0644 "$deb_root/DEBIAN/control" "$deb_root/usr/share/applications/io.github.osverse.Osverse.desktop"

find "$deb_root" -print0 | xargs -0 touch --date="@$source_epoch"
deb_name="osverse_${package_version}_amd64_${target_name}.deb"
dpkg-deb --root-owner-group --build "$deb_root" "$output_dir/$deb_name" >/dev/null

appdir="$staging_root/Osverse.AppDir"
install -d -m 0755 \
  "$appdir/usr/bin" \
  "$appdir/usr/share/applications" \
  "$appdir/usr/share/icons/hicolor/512x512/apps" \
  "$appdir/usr/share/licenses/osverse" \
  "$appdir/usr/share/metainfo"
install -m 0755 "$binary_path" "$appdir/usr/bin/osverse"
install -m 0644 build/appicon.png "$appdir/osverse.png"
install -m 0644 build/appicon.png "$appdir/usr/share/icons/hicolor/512x512/apps/osverse.png"
install -m 0644 LICENSE "$appdir/usr/share/licenses/osverse/LICENSE"
cat > "$appdir/AppRun" <<'EOF'
#!/bin/sh
set -eu
exec "${APPDIR}/usr/bin/osverse" "$@"
EOF
cat > "$appdir/io.github.osverse.Osverse.desktop" <<EOF
[Desktop Entry]
Type=Application
Name=Osverse
Comment=Manage local AI development tools
Exec=osverse
Icon=osverse
Terminal=false
Categories=Development;Utility;
StartupNotify=true
X-AppImage-Version=$package_version
EOF
chmod 0755 "$appdir/AppRun"
chmod 0644 "$appdir/io.github.osverse.Osverse.desktop"
cp "$appdir/io.github.osverse.Osverse.desktop" "$appdir/usr/share/applications/io.github.osverse.Osverse.desktop"
install -m 0644 build/linux/osverse.appdata.xml "$appdir/usr/share/metainfo/io.github.osverse.Osverse.appdata.xml"
find "$appdir" -print0 | xargs -0 touch --date="@$source_epoch"

appimage_name="osverse-${package_version}-linux-amd64-${target_name}.AppImage"
ARCH=x86_64 VERSION="$package_version" APPIMAGE_EXTRACT_AND_RUN=1 \
  "$appimagetool" --runtime-file "$appimage_runtime" \
  "$appdir" "$output_dir/$appimage_name" >/dev/null
chmod 0755 "$output_dir/$appimage_name"

(
  cd "$output_dir"
  sha256sum "$archive_name.tar.gz" "$deb_name" "$appimage_name" > "SHA256SUMS-${target_name}"
)

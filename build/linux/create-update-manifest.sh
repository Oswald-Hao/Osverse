#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 4 ]]; then
  echo "usage: create-update-manifest.sh VERSION REPOSITORY RELEASE_DIR OUTPUT" >&2
  exit 2
fi

version=$1
repository=$2
release_dir=$3
output=$4

if [[ ! $version =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]] ||
   [[ ! $repository =~ ^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$ ]] ||
   [[ ! -d $release_dir ]]; then
  echo "invalid update manifest input" >&2
  exit 2
fi

release_dir=$(realpath "$release_dir")
output_parent=$(dirname "$output")
mkdir -p "$output_parent"
output=$(realpath -m "$output")
case "$output" in
  "$release_dir"/*) ;;
  *) echo "update manifest output must be inside the release directory" >&2; exit 2 ;;
esac

python3 - "$version" "$repository" "$release_dir" "$output" <<'PY'
import hashlib
import json
import pathlib
import re
import sys

version, repository, release_dir_raw, output_raw = sys.argv[1:]
release_dir = pathlib.Path(release_dir_raw)
output = pathlib.Path(output_raw)
pattern = re.compile(
    r"^osverse(?:-|_)(?P<version>[^_]+?)(?:-linux-amd64-|_amd64_)"
    r"(?P<target>ubuntu(?:20\.04|22\.04))(?P<suffix>\.AppImage|\.tar\.gz|\.deb)$"
)
formats = {".AppImage": "appimage", ".tar.gz": "tar.gz", ".deb": "deb"}
artifacts = []
for candidate in sorted(release_dir.iterdir(), key=lambda item: item.name):
    if not candidate.is_file() or candidate.is_symlink():
        continue
    match = pattern.match(candidate.name)
    if not match or match.group("version") != version:
        continue
    suffix = match.group("suffix")
    digest = hashlib.sha256(candidate.read_bytes()).hexdigest()
    artifacts.append({
        "architecture": "amd64",
        "filename": candidate.name,
        "format": formats[suffix],
        "platform": "linux",
        "sha256": digest,
        "size": candidate.stat().st_size,
        "target": match.group("target"),
        "url": f"https://github.com/{repository}/releases/download/v{version}/{candidate.name}",
    })
if len(artifacts) != 6:
    raise SystemExit(f"expected six Linux release artifacts, found {len(artifacts)}")
document = {
    "artifacts": artifacts,
    "channel": "prerelease" if "-" in version else "stable",
    "repository": repository,
    "schemaVersion": 1,
    "tag": f"v{version}",
    "version": version,
}
temporary = output.with_name(f".{output.name}.tmp")
temporary.write_text(json.dumps(document, ensure_ascii=False, indent=2, sort_keys=True) + "\n", encoding="utf-8")
temporary.chmod(0o644)
temporary.replace(output)
PY

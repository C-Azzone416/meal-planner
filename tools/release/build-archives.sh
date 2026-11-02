#!/usr/bin/env bash
# Build the CLI and MCP binaries and package one archive per target.
#
# Usage: tools/release/build-archives.sh <version> <outdir> [os/arch ...]
#   <version>  semver without the "cli-" prefix, e.g. 0.1.0
#   <outdir>   directory that receives the archives (created if missing)
#   os/arch    optional targets; defaults to every supported target
#
# Run from anywhere; it operates on the repository containing this script.
# Output: <outdir>/mealplanner_<version>_<os>_<arch>.{tar.gz,zip}
set -euo pipefail

DEFAULT_TARGETS="darwin/arm64 darwin/amd64 linux/amd64 linux/arm64 windows/amd64"
VERSION_PKG="github.com/C-Azzone416/meal-planner/internal/core/version.Version"

if [ "$#" -lt 2 ]; then
  echo "usage: $0 <version> <outdir> [os/arch ...]" >&2
  exit 2
fi
version="$1"
outdir="$2"
shift 2
targets="${*:-$DEFAULT_TARGETS}"

if ! [[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "error: version '$version' is not X.Y.Z (no cli- or v prefix)" >&2
  exit 2
fi

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
mkdir -p "$outdir"
outdir="$(cd "$outdir" && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

cd "$repo_root"
for target in $targets; do
  goos="${target%/*}"
  goarch="${target#*/}"
  ext=""
  [ "$goos" = "windows" ] && ext=".exe"
  name="mealplanner_${version}_${goos}_${goarch}"
  stage="$work/$name"
  mkdir -p "$stage"

  echo "==> building $target"
  for bin in mealplanner mealplanner-mcp; do
    CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build -trimpath \
      -ldflags "-s -w -X ${VERSION_PKG}=${version}" \
      -o "$stage/${bin}${ext}" "./cmd/${bin}"
  done
  for doc in LICENSE LICENSE.md LICENSE.txt README.md; do
    [ -f "$doc" ] && cp "$doc" "$stage/"
  done

  if [ "$goos" = "windows" ]; then
    archive="$outdir/${name}.zip"
    rm -f "$archive"
    (cd "$work" && zip -qr -X "$archive" "$name")
  else
    archive="$outdir/${name}.tar.gz"
    # gzip -n omits the gzip header timestamp (archives are not bit-reproducible).
    (cd "$work" && tar -cf - "$name" | gzip -n -9 > "$archive")
  fi
  echo "    wrote $archive"
done

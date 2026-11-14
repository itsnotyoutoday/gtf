#!/bin/sh
# Builds the release tarball for one platform:
#
#   scripts/dist.sh VERSION GOOS GOARCH [GOARM]     -> dist/gtf-VERSION-<os>-<arch>.tar.gz
#
# The tarball unpacks to gtf-VERSION-<label>/ with
#   bin/gtf             the static client binary
#   lib/gtf/{tf,gtf,examples,docs}   the library directory (what install.sh puts in <prefix>/lib/gtf)
#   install.sh README.md LICENSE
# and `scripts/dist.sh --sums` writes dist/SHA256SUMS for everything in dist/.
set -eu
cd "$(dirname "$0")/.."

if [ "${1:-}" = "--sums" ]; then
  cd dist
  if command -v sha256sum >/dev/null 2>&1; then sha256sum gtf-*.tar.gz > SHA256SUMS
  else shasum -a 256 gtf-*.tar.gz > SHA256SUMS; fi
  cat SHA256SUMS
  exit 0
fi

[ $# -ge 3 ] || { echo "usage: $0 VERSION GOOS GOARCH [GOARM]   |   $0 --sums" >&2; exit 2; }
ver="${1#v}"; ver="${ver%-rel}"
os="$2"; arch="$3"; arm="${4:-}"
[ -n "$ver" ] && [ -n "$os" ] && [ -n "$arch" ] || { echo "version, GOOS and GOARCH must not be empty" >&2; exit 2; }
label="$os-$arch"; [ "$arch" = arm ] && label="$os-armv7"
d="gtf-$ver-$label"
stage="dist/.stage-$d"
[ -d "$stage" ] && rm -r "$stage"
mkdir -p "$stage/$d/bin" "$stage/$d/lib/gtf"

CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" GOARM="$arm" go build -trimpath \
  -ldflags "-s -w -X github.com/itsnotyoutoday/gtf/internal/client.Version=$ver" \
  -o "$stage/$d/bin/gtf" ./cmd/gtf

cp -R gtflib/tf "$stage/$d/lib/gtf/tf"
cp -R gtflib/gtf "$stage/$d/lib/gtf/gtf"
cp gtflib/TF-NOTICE.md "$stage/$d/lib/gtf/TF-NOTICE.md"
mkdir -p "$stage/$d/lib/gtf/examples" "$stage/$d/lib/gtf/docs"
cp examples/*.tf "$stage/$d/lib/gtf/examples/"
cp docs/*.md "$stage/$d/lib/gtf/docs/"
cp README.md LICENSE install.sh "$stage/$d/"
chmod 755 "$stage/$d/bin/gtf" "$stage/$d/install.sh"

tar -C "$stage" -czf "dist/$d.tar.gz" "$d"
rm -r "$stage"
echo "dist/$d.tar.gz"

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
  files=$(ls gtf-*.tar.gz gtf_*.deb gtf-*.rpm 2>/dev/null || true)
  # shellcheck disable=SC2086
  if command -v sha256sum >/dev/null 2>&1; then sha256sum $files > SHA256SUMS
  else shasum -a 256 $files > SHA256SUMS; fi
  cat SHA256SUMS
  exit 0
fi

[ $# -ge 3 ] || { echo "usage: $0 VERSION GOOS GOARCH [GOARM]   |   $0 --sums" >&2; exit 2; }
ver="${1#v}"; ver="${ver%-rel}"
os="$2"; arch="$3"; arm="${4:-}"
[ -n "$ver" ] && [ -n "$os" ] && [ -n "$arch" ] || { echo "version, GOOS and GOARCH must not be empty" >&2; exit 2; }
# the version is the VERSION file (embedded at compile time); a release must be built from a tree whose
# VERSION matches the tag. GTF_VERSION_OVERRIDE=1 builds the tree as another version (packaging tests only).
vflag=""
if [ "$ver" != "$(tr -d '[:space:]' < VERSION)" ]; then
  [ -n "${GTF_VERSION_OVERRIDE:-}" ] || { echo "version $ver does not match the VERSION file ($(tr -d '[:space:]' < VERSION)); bump VERSION first" >&2; exit 2; }
  vflag="-X github.com/itsnotyoutoday/gtf/internal/client.versionOverride=$ver"
fi

label="$os-$arch"; [ "$arch" = arm ] && label="$os-armv7"
d="gtf-$ver-$label"
stage="dist/.stage-$d"
[ -d "$stage" ] && rm -r "$stage"
mkdir -p "$stage/$d/bin" "$stage/$d/lib/gtf"

CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" GOARM="$arm" go build -trimpath \
  -ldflags "-s -w $vflag" \
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

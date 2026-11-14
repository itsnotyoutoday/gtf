#!/bin/sh
# Builds the .deb and .rpm for one Linux architecture with nfpm:
#
#   scripts/package.sh VERSION GOARCH [GOARM]      -> dist/gtf_<version>_<arch>.deb and dist/gtf-<version>.<arch>.rpm
#
# GOARCH is amd64, arm64, 386 or arm (with GOARM=7). nfpm is run as `nfpm` when it is on PATH, otherwise
# with `go run` (needs network once). The packages install /usr/bin/gtf and /usr/lib/gtf, and record in
# /usr/lib/gtf/install.json that the package manager owns them: /update only reports new versions there.
set -eu
cd "$(dirname "$0")/.."

[ $# -ge 2 ] || { echo "usage: $0 VERSION GOARCH [GOARM]" >&2; exit 2; }
ver="${1#v}"; ver="${ver%-rel}"
goarch="$2"; arm="${3:-}"
[ -n "$ver" ] && [ -n "$goarch" ] || { echo "version and GOARCH must not be empty" >&2; exit 2; }
# the version is the VERSION file (embedded at compile time); a release must be built from a tree whose
# VERSION matches the tag. GTF_VERSION_OVERRIDE=1 builds the tree as another version (packaging tests only).
vflag=""
if [ "$ver" != "$(tr -d '[:space:]' < VERSION)" ]; then
  [ -n "${GTF_VERSION_OVERRIDE:-}" ] || { echo "version $ver does not match the VERSION file ($(tr -d '[:space:]' < VERSION)); bump VERSION first" >&2; exit 2; }
  vflag="-X github.com/itsnotyoutoday/gtf/internal/client.versionOverride=$ver"
fi

nfarch="$goarch"; [ "$goarch" = arm ] && nfarch="arm${arm:-7}"

stage="dist/.pkg-root"
[ -d "$stage" ] && rm -r "$stage"
mkdir -p "$stage/bin" "$stage/lib/gtf" "$stage/doc" dist

# -tags pkgbuild: the distribution build contains no GitHub fetch/install code (the package manager owns the files)
CGO_ENABLED=0 GOOS=linux GOARCH="$goarch" GOARM="$arm" go build -trimpath -tags pkgbuild \
  -ldflags "-s -w $vflag" \
  -o "$stage/bin/gtf" ./cmd/gtf
chmod 755 "$stage/bin/gtf"

cp -R gtflib/tf "$stage/lib/gtf/tf"
cp -R gtflib/gtf "$stage/lib/gtf/gtf"
cp gtflib/TF-NOTICE.md "$stage/lib/gtf/TF-NOTICE.md"
mkdir -p "$stage/lib/gtf/docs"
cp docs/*.md "$stage/lib/gtf/docs/"
cp README.md LICENSE "$stage/doc/"
printf '{\n  "scope": "package",\n  "managed_by": "package",\n  "bindir": "/usr/bin",\n  "libdir": "/usr/lib/gtf",\n  "version": "%s"\n}\n' "$ver" > "$stage/lib/gtf/install.json"

if command -v nfpm >/dev/null 2>&1; then NFPM="nfpm"; else NFPM="go run github.com/goreleaser/nfpm/v2/cmd/nfpm@v2.41.1"; fi
export GTF_VERSION="$ver" GTF_ARCH="$nfarch"
for fmt in deb rpm; do
  $NFPM package --config packaging/nfpm.yaml --packager "$fmt" --target dist/
done
rm -r "$stage"

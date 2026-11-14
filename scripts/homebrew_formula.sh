#!/bin/sh
# Renders the Homebrew formula for a release from dist/SHA256SUMS:
#
#   scripts/homebrew_formula.sh TAG [BASE_URL] > gtf.rb
#
# TAG is the release tag (v0.0.2-rel or v0.0.2-alpha1); BASE_URL defaults to the GitHub release download
# URL. The release workflow publishes the result as gtf.rb; copy it into the tap's Formula/ directory.
set -eu
cd "$(dirname "$0")/.."
tag="${1:?usage: $0 TAG [BASE_URL]}"
ver="${tag#v}"; ver="${ver%-rel}"
base="${2:-https://github.com/itsnotyoutoday/gtf/releases/download/$tag}"
sums="dist/SHA256SUMS"
[ -f "$sums" ] || { echo "$sums not found: run scripts/dist.sh --sums first" >&2; exit 1; }

sum_of() { # os-arch -> sha256 of its tarball
  f="gtf-$ver-$1.tar.gz"
  h=$(grep " \*\{0,1\}$f\$" "$sums" | cut -d' ' -f1)
  [ -n "$h" ] || { echo "no checksum for $f in $sums" >&2; exit 1; }
  echo "$h"
}
sed -e "s|@VERSION@|$ver|g" \
    -e "s|@URL_DARWIN_ARM64@|$base/gtf-$ver-darwin-arm64.tar.gz|" -e "s|@SHA_DARWIN_ARM64@|$(sum_of darwin-arm64)|" \
    -e "s|@URL_DARWIN_AMD64@|$base/gtf-$ver-darwin-amd64.tar.gz|" -e "s|@SHA_DARWIN_AMD64@|$(sum_of darwin-amd64)|" \
    -e "s|@URL_LINUX_ARM64@|$base/gtf-$ver-linux-arm64.tar.gz|"   -e "s|@SHA_LINUX_ARM64@|$(sum_of linux-arm64)|" \
    -e "s|@URL_LINUX_AMD64@|$base/gtf-$ver-linux-amd64.tar.gz|"   -e "s|@SHA_LINUX_AMD64@|$(sum_of linux-amd64)|" \
    packaging/homebrew/gtf.rb.in

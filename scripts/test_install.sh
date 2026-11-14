#!/bin/sh
# End-to-end test of install.sh and `gtf --update` against a fake GitHub (a local web server):
#
#   scripts/test_install.sh
#
# Builds two release tarballs for this machine (0.0.2-alpha1 and 0.0.3-alpha1), serves them, installs the older
# one into a temporary prefix, checks that `gtf --update` sees and installs the newer one (binary AND
# library, keeping local.tf), then re-runs the installer and uninstalls. Needs go, python3, curl.
set -eu
cd "$(dirname "$0")/.."
T=$(mktemp -d)
trap 'kill "$SRV" 2>/dev/null || true; rm -r "$T"' EXIT INT TERM
ok() { printf 'ok   %s\n' "$*"; }
fail() { printf 'FAIL %s\n' "$*" >&2; exit 1; }

export GTF_VERSION_OVERRIDE=1   # build this one tree as two different versions
os=$(go env GOOS); arch=$(go env GOARCH); label="$os-$arch"
mkdir -p dist
for v in 0.0.2-alpha1 0.0.3-alpha1; do ./scripts/dist.sh "$v" "$os" "$arch" >/dev/null; done

# the fake server: /dl/<tag>/<file> and /api/repos/test/gtf/releases
mkdir -p "$T/srv/api/repos/test/gtf" "$T/srv/dl"
PORT=$(python3 -c 'import socket;s=socket.socket();s.bind(("127.0.0.1",0));print(s.getsockname()[1])')
BASE="http://127.0.0.1:$PORT"
for v in 0.0.2-alpha1 0.0.3-alpha1; do
  mkdir -p "$T/srv/dl/v$v"
  cp "dist/gtf-$v-$label.tar.gz" "$T/srv/dl/v$v/"
  ( cd dist && if command -v sha256sum >/dev/null 2>&1; then sha256sum "gtf-$v-$label.tar.gz"; else shasum -a 256 "gtf-$v-$label.tar.gz"; fi ) > "$T/srv/dl/v$v/SHA256SUMS"
done
python3 - "$T/srv/api/repos/test/gtf/releases" "$BASE" "$label" <<'PY'
import json, sys
out, base, label = sys.argv[1:4]
rels = []
for v in ("0.0.3-alpha1", "0.0.2-alpha1"):   # newest first, like GitHub
    tag = "v" + v
    rels.append({"tag_name": tag, "draft": False, "prerelease": True, "assets": [
        {"name": f"gtf-{v}-{label}.tar.gz", "browser_download_url": f"{base}/dl/{tag}/gtf-{v}-{label}.tar.gz"},
        {"name": "SHA256SUMS", "browser_download_url": f"{base}/dl/{tag}/SHA256SUMS"}]})
json.dump(rels, open(out, "w"))
PY
( cd "$T/srv" && exec python3 -m http.server "$PORT" --bind 127.0.0.1 >/dev/null 2>&1 ) &
SRV=$!
i=0; until curl -fs "$BASE/api/repos/test/gtf/releases" >/dev/null 2>&1; do i=$((i+1)); [ $i -lt 50 ] || fail "fake server did not start"; sleep 0.1; done

export GTF_REPO=test/gtf GTF_API_BASE="$BASE/api" GTF_RELEASE_BASE="$BASE/dl"
export GTF_UPDATE_API="$BASE/api" GTF_UPDATE_REPO=test/gtf
export HOME="$T/home"; mkdir -p "$HOME"
P="$T/prefix"

# 1. install the older release into a user prefix
sh install.sh --yes --prefix "$P" --version 0.0.2-alpha1 --no-update-check >"$T/out1" 2>&1 || { cat "$T/out1"; fail "install"; }
[ -x "$P/bin/gtf" ] && [ -f "$P/lib/gtf/tf/stdlib.tf" ] && [ -f "$P/lib/gtf/gtf/examples/lcars.tf" ] || fail "files missing after install"
grep -q '"version": "0.0.2-alpha1"' "$P/lib/gtf/install.json" || fail "install.json version"
"$P/bin/gtf" --version | grep -q "0.0.2-alpha1" || fail "installed binary version"
grep -q '"update_check": false' "$HOME/.gtf/config.json" || fail "update_check setting not written"
ok "install (binary, library, examples, record, setting)"

# 2. the user's own local.tf survives an update
echo '/echo mine' > "$P/lib/gtf/local.tf"

# 3. gtf --update says what is new, without installing
"$P/bin/gtf" --update >"$T/out2" 2>&1 || { cat "$T/out2"; fail "--update check"; }
grep -q "Update available: gtf 0.0.2-alpha1 -> 0.0.3-alpha1" "$T/out2" || { cat "$T/out2"; fail "--update did not report the new version"; }
"$P/bin/gtf" --version | grep -q "0.0.2-alpha1" || fail "check installed something"
ok "gtf --update reports 0.0.3-alpha1"

# 4. gtf --update -y installs it (binary and library)
"$P/bin/gtf" --update -y >"$T/out3" 2>&1 || { cat "$T/out3"; fail "--update -y"; }
"$P/bin/gtf" --version | grep -q "0.0.3-alpha1" || { cat "$T/out3"; fail "binary not updated"; }
grep -q '"version": "0.0.3-alpha1"' "$P/lib/gtf/install.json" || fail "record not updated"
[ -f "$P/bin/gtf.old" ] && [ -d "$P/lib/gtf.old" ] || fail "previous version not kept"
grep -q "mine" "$P/lib/gtf/local.tf" || fail "local.tf lost"
ok "gtf --update -y (binary + library swapped, old kept, local.tf kept)"

# 5. up to date now
"$P/bin/gtf" --update 2>&1 | grep -q "up to date" || fail "should be up to date"
ok "up to date after update"

# 6. the installer leaves an up-to-date install alone, and a downgrade by --version installs it
sh install.sh --yes --prefix "$P" --version 0.0.3-alpha1 2>&1 | grep -q "already installed" || fail "installer should skip the current version"
ok "re-running the installer is a no-op"

# 7. tampered download is refused
echo tamper >> "$T/srv/dl/v0.0.2-alpha1/gtf-0.0.2-alpha1-$label.tar.gz"
if sh install.sh --yes --prefix "$T/p2" --version 0.0.2-alpha1 >"$T/out4" 2>&1; then fail "installed a tampered archive"; fi
grep -q "checksum mismatch" "$T/out4" || { cat "$T/out4"; fail "no checksum error"; }
[ ! -e "$T/p2/bin/gtf" ] || fail "binary present after checksum failure"
ok "checksum mismatch refused"

# 8. uninstall
sh install.sh --yes --prefix "$P" --uninstall >/dev/null 2>&1 || fail "uninstall"
[ ! -e "$P/bin/gtf" ] && [ ! -e "$P/lib/gtf" ] || fail "files left after uninstall"
ok "uninstall"
echo "all installer tests passed"

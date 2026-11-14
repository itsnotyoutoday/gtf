#!/bin/sh
# Installs GoTinyFugue (gtf), a TinyFugue-compatible terminal MU* client.
#
#   curl -fsSL https://raw.githubusercontent.com/itsnotyoutoday/gtf/main/install.sh | sh
#   curl -fsSL https://raw.githubusercontent.com/itsnotyoutoday/gtf/main/install.sh | sh -s -- --global
#
# By default it installs for the CURRENT USER ONLY: the program goes into ~/bin and its library into
# ~/lib/gtf. No root, no Go. With --global it installs for every user of the machine: /usr/local/bin and
# /usr/local/lib/gtf (it asks for sudo when it needs it).
#
# It asks a few questions (answers are read from your terminal even when piped from curl). Every question
# has a switch, so it also runs unattended:
#
#   --user | --global          who it is for                                    (default: user)
#   --prefix DIR               install under DIR/bin and DIR/lib/gtf            (default: ~ or /usr/local)
#   --bindir DIR               where the gtf program goes                        (default: PREFIX/bin)
#   --libdir DIR               where the library goes                            (default: PREFIX/lib/gtf)
#   --update-check | --no-update-check   once-a-day look for a newer gtf       (default: yes)
#   --channel stable|prerelease   which releases to follow                       (default: newest, pre-releases included)
#   --version 0.0.2            install this release instead of the newest
#   --yes                      do not ask, use the defaults for anything not given as a switch
#   --uninstall                remove what this installer put there (your ~/.gtf is never touched)
#
# After installing, gtf updates itself: /update says what is available, /update -y installs it, and
# /restart (or /update -y -r) swaps to the new version without dropping your connections.
# Re-running this script also upgrades (an up-to-date install is left alone).
#
# Testing: GTF_REPO, GTF_API_BASE (releases API base) and GTF_RELEASE_BASE (download base URL) override GitHub.
set -eu

REPO="${GTF_REPO:-itsnotyoutoday/gtf}"
API_BASE="${GTF_API_BASE:-https://api.github.com}"
DL_BASE="${GTF_RELEASE_BASE:-https://github.com/$REPO/releases/download}"

scope="" prefix="" bindir="" libdir="" version="" channel="" want_check="" yes=n uninstall=n

say() { printf '%s\n' "$*"; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }
usage() { sed -n '2,/^set -eu/p' "$0" 2>/dev/null | sed '$d; s/^# \{0,1\}//'; }

while [ $# -gt 0 ]; do
  case "$1" in
    --user) scope=user ;;
    --global) scope=global ;;
    --prefix) [ $# -ge 2 ] || die "--prefix needs a value"; prefix="$2"; shift ;;
    --bindir) [ $# -ge 2 ] || die "--bindir needs a value"; bindir="$2"; shift ;;
    --libdir) [ $# -ge 2 ] || die "--libdir needs a value"; libdir="$2"; shift ;;
    --update-check) want_check=y ;;
    --no-update-check) want_check=n ;;
    --channel) [ $# -ge 2 ] || die "--channel needs a value"; channel="$2"; shift ;;
    --version) [ $# -ge 2 ] || die "--version needs a value"; version="$2"; shift ;;
    --yes|-y) yes=y ;;
    --uninstall) uninstall=y ;;
    -h|--help) usage; exit 0 ;;
    *) die "unknown option: $1 (try --help)" ;;
  esac
  shift
done
case "$channel" in ""|stable|prerelease) ;; *) die "--channel must be stable or prerelease" ;; esac

# --- questions ---------------------------------------------------------------------------------------
have_tty=n
if [ "$yes" = n ] && (: </dev/tty) 2>/dev/null; then have_tty=y; fi

# prompt "question" default -> answer in $ANS
prompt() {
  if [ "$have_tty" = y ]; then
    printf '%s (default %s): ' "$1" "$2" >/dev/tty
    ANS=""; read -r ANS </dev/tty || ANS=""
    [ -n "$ANS" ] || ANS="$2"
  else
    ANS="$2"
  fi
}
yesno() { # yesno "question" default -> y/n in $ANS
  while :; do
    prompt "$1 [y/n]" "$2"
    case "$ANS" in y|Y|yes|YES|Yes) ANS=y ;; n|N|no|NO|No) ANS=n ;; *) [ "$have_tty" = y ] && { printf 'please answer y or n\n' >/dev/tty; continue; }; ANS="$2" ;; esac
    break
  done
}
expand() { case "$1" in "~") echo "$HOME" ;; "~/"*) echo "$HOME/${1#\~/}" ;; *) echo "$1" ;; esac; }

if [ -z "$scope" ]; then
  if [ "$have_tty" = y ]; then
    prompt "Install for just you (user), or for everyone on this machine (global)?" "user"
    case "$ANS" in g*|G*) scope=global ;; *) scope=user ;; esac
  else
    scope=user
  fi
fi
if [ "$scope" = global ]; then def_prefix="/usr/local"; else def_prefix="$HOME"; fi
[ -n "$prefix" ] || prefix="$def_prefix"
prefix=$(expand "$prefix")
if [ -z "$bindir" ]; then
  [ "$have_tty" = y ] && [ "$yes" = n ] && [ "$uninstall" = n ] && { prompt "Install the gtf program into which folder?" "$prefix/bin"; bindir="$ANS"; } || bindir="$prefix/bin"
fi
if [ -z "$libdir" ]; then
  [ "$have_tty" = y ] && [ "$yes" = n ] && [ "$uninstall" = n ] && { prompt "Install the gtf library into which folder?" "$prefix/lib/gtf"; libdir="$ANS"; } || libdir="$prefix/lib/gtf"
fi
bindir=$(expand "$bindir"); libdir=$(expand "$libdir")
case "$bindir" in /*) ;; *) bindir="$PWD/$bindir" ;; esac
case "$libdir" in /*) ;; *) libdir="$PWD/$libdir" ;; esac
[ "$libdir" != "/" ] && [ "$bindir" != "/" ] || die "refusing to use / as an install folder"

# --- privileges: global installs may need sudo --------------------------------------------------------
SUDO=""
need_root() { # need_root DIR: does installing into DIR (or its nearest existing parent) need root?
  d="$1"; while [ ! -d "$d" ]; do d=$(dirname "$d"); done
  [ ! -w "$d" ]
}
if need_root "$bindir" || need_root "$libdir"; then
  if [ "$(id -u)" = 0 ]; then SUDO=""
  elif command -v sudo >/dev/null 2>&1; then
    SUDO="sudo"
    say "Installing into $bindir and $libdir needs administrator rights; sudo will ask for your password."
  else
    die "cannot write to $bindir / $libdir and sudo is not available; run this as root, or install for yourself with --user"
  fi
fi
run() { if [ -n "$SUDO" ]; then $SUDO "$@"; else "$@"; fi; }

record="$libdir/install.json"

# --- uninstall ----------------------------------------------------------------------------------------
if [ "$uninstall" = y ]; then
  [ -f "$record" ] || die "no gtf installation found (missing $record); pass --global/--prefix/--libdir as when installing"
  if [ "$yes" = n ] && [ "$have_tty" = y ]; then
    yesno "Remove gtf from $bindir and $libdir? (your ~/.gtf settings stay)" n
    [ "$ANS" = y ] || { say "Nothing removed."; exit 0; }
  fi
  run rm -f "$bindir/gtf" "$bindir/gtf.old" "$bindir/gtf.new"
  run rm -r "$libdir" 2>/dev/null || true
  [ ! -d "$libdir.old" ] || run rm -r "$libdir.old"
  say "gtf removed."
  exit 0
fi

# --- environment --------------------------------------------------------------------------------------
if command -v curl >/dev/null 2>&1; then
  get() { curl -fsSL "$1"; }
  getfile() { curl -fsSL -o "$2" "$1"; }
elif command -v wget >/dev/null 2>&1; then
  get() { wget -qO- "$1"; }
  getfile() { wget -qO "$2" "$1"; }
else
  die "need curl or wget"
fi
case "$(uname -s)" in
  Linux) os=linux ;;
  FreeBSD) os=freebsd ;;
  Darwin) os=darwin ;;
  *) die "this installer supports Linux, FreeBSD and macOS (got $(uname -s))" ;;
esac
case "$(uname -m)" in
  x86_64|amd64) arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
  i386|i686) arch=386 ;;
  armv7*|armv8l) arch=armv7 ;;
  *) die "unsupported CPU: $(uname -m)" ;;
esac
label="$os-$arch"

# --- which release ------------------------------------------------------------------------------------
tag=""
case "$version" in
  "") ;;
  v*) tag="$version" ;;
  *-*) tag="v$version" ;;
  *) tag="v${version}-rel" ;;   # --version 0.0.2 means the final release tag v0.0.2-rel
esac
if [ -z "$tag" ]; then
  want_pre=y; [ "$channel" = stable ] && want_pre=n
  tag=$(get "$API_BASE/repos/$REPO/releases?per_page=30" | tr ',{}' '\n\n\n' | awk -v pre="$want_pre" '
    /"tag_name"/ { t=$0; sub(/.*"tag_name"[ ]*:[ ]*"/, "", t); sub(/".*/, "", t); draft=""; next }
    /"draft"/ { draft=($0 ~ /true/) ? "t" : "f"; next }
    /"prerelease"/ { p=($0 ~ /true/) ? "t" : "f"; if (draft != "t" && (pre == "y" || p == "f")) { print t; exit } }') || true
  [ -n "$tag" ] || die "could not find a release; see https://github.com/$REPO/releases"
fi
num="${tag#v}"; num="${num%-rel}"
file="gtf-$num-$label.tar.gz"

# --- already current? ---------------------------------------------------------------------------------
if [ -x "$bindir/gtf" ] && [ -f "$record" ]; then
  have=$(sed -n 's/.*"version": *"\([^"]*\)".*/\1/p' "$record" | head -n1) || have=""
  if [ "$have" = "$num" ]; then
    say "gtf $num is already installed in $bindir (up to date)."
    exit 0
  fi
fi

if [ -z "$want_check" ]; then
  yesno "Check for a newer gtf once a day (it only tells you; /update -y installs)?" y
  want_check="$ANS"
fi

# --- download and verify ------------------------------------------------------------------------------
tmp=$(mktemp -d)
trap 'rm -r "$tmp" 2>/dev/null || true' EXIT INT TERM
say "Downloading $file ..."
getfile "$DL_BASE/$tag/$file" "$tmp/$file" || die "download failed: $DL_BASE/$tag/$file"
getfile "$DL_BASE/$tag/SHA256SUMS" "$tmp/SHA256SUMS" || die "could not download SHA256SUMS; refusing to install an unverified file"
want=$(grep " \*\{0,1\}$file\$" "$tmp/SHA256SUMS" | cut -d' ' -f1)
[ -n "$want" ] || die "SHA256SUMS has no entry for $file"
if command -v sha256sum >/dev/null 2>&1; then got=$(sha256sum "$tmp/$file" | cut -d' ' -f1)
elif command -v sha256 >/dev/null 2>&1; then got=$(sha256 -q "$tmp/$file")
elif command -v shasum >/dev/null 2>&1; then got=$(shasum -a 256 "$tmp/$file" | cut -d' ' -f1)
else die "no sha256sum/sha256/shasum found to verify the download"; fi
[ "$got" = "$want" ] || die "checksum mismatch, not installing"
say "Checksum OK."
tar -xzf "$tmp/$file" -C "$tmp"
src="$tmp/gtf-$num-$label"
[ -x "$src/bin/gtf" ] && [ -d "$src/lib/gtf" ] || die "the release archive does not look right"

# --- install ------------------------------------------------------------------------------------------
run mkdir -p "$bindir" "$(dirname "$libdir")"

# library: stage next to the destination, keep the user's local.tf, swap in, keep the old one as .old
[ ! -d "$libdir.new" ] || run rm -r "$libdir.new"
run cp -R "$src/lib/gtf" "$libdir.new"
[ ! -f "$libdir/local.tf" ] || run cp "$libdir/local.tf" "$libdir.new/local.tf"
printf '{\n  "scope": "%s",\n  "bindir": "%s",\n  "libdir": "%s",\n  "version": "%s",\n  "repo": "%s"\n}\n' \
  "$scope" "$bindir" "$libdir" "$num" "$REPO" > "$tmp/install.json"
run cp "$tmp/install.json" "$libdir.new/install.json"
if [ -d "$libdir" ]; then
  [ ! -d "$libdir.old" ] || run rm -r "$libdir.old"
  run mv "$libdir" "$libdir.old"
fi
run mv "$libdir.new" "$libdir"

# program: replace atomically (safe even while an old gtf is running), keep the old one as gtf.old
run cp "$src/bin/gtf" "$bindir/gtf.new"
run chmod 755 "$bindir/gtf.new"
[ ! -f "$bindir/gtf" ] || run cp -p "$bindir/gtf" "$bindir/gtf.old"
run mv "$bindir/gtf.new" "$bindir/gtf"

# the update-check setting belongs to the user, not to the machine
if [ "$want_check" = n ]; then
  mkdir -p "$HOME/.gtf"
  if [ ! -f "$HOME/.gtf/config.json" ]; then
    printf '{\n  "update_check": false\n}\n' > "$HOME/.gtf/config.json"
  fi
fi

say ""
say "Installed gtf $num:"
say "  program  $bindir/gtf"
say "  library  $libdir"
case ":$PATH:" in
  *":$bindir:"*) say ""; say "Start it with:  gtf" ;;
  *) say ""; say "$bindir is not in your PATH. Add it (for example to ~/.profile):"; say "  export PATH=\"$bindir:\$PATH\""; say "or start it with:  $bindir/gtf" ;;
esac
say "Keep it current with /update (says what is new) and /update -y (installs). Your own files live in ~/.gtf."

---
title: Installing gtf
topics: install, installing, install-gtf
section: Start here
order: 20
---
# Installing gtf

gtf is one program with its library files. The easiest way to get both is the installer, a shell script that downloads the release for your system and checks it before installing anything.

## The installer

```
U=https://raw.githubusercontent.com/itsnotyoutoday/gtf/main/install.sh
curl -fsSL $U | sh
curl -fsSL $U | sh -s -- --global
```

The first line installs for you alone and needs no special rights. The second installs for everyone on the machine and uses `sudo` when it has to.

| | just you (default) | `--global` |
|---|---|---|
| program | `~/bin/gtf` | `/usr/local/bin/gtf` |
| library | `~/lib/gtf` | `/usr/local/lib/gtf` |
| your own files | `~/.gtf` | `~/.gtf` for each user |

The installer asks where to put things and whether gtf should look for updates once a day. Every question has a switch, so it also runs without asking: `--user`, `--global`, `--prefix DIR`, `--bindir DIR`, `--libdir DIR`, `--update-check` or `--no-update-check`, `--channel stable` or `--channel prerelease`, `--version X`, `--yes` and `--uninstall`.

It picks the release file for your operating system and processor (Linux, FreeBSD and macOS; 64-bit and 32-bit Intel, 64-bit and 32-bit ARM), compares its SHA-256 checksum with the one published with the release, and refuses to install anything it cannot verify. It replaces the program in one step, so it is safe to run while gtf is running. Running it again upgrades; an up-to-date install is left alone. `--uninstall` removes the program and library but never `~/.gtf`.

Make sure the folder with the program (`~/bin` for a personal install) is on your `PATH`, then type `gtf`.

## Packages and Homebrew

Releases also include `.deb` and `.rpm` packages, and there is a Homebrew formula:

```
sudo apt install ./gtf_VERSION_amd64.deb
sudo dnf install ./gtf-VERSION-1.x86_64.rpm
brew install itsnotyoutoday/tap/gtf
```

A package manager owns the files it installs. For those, `/update` only tells you whether a newer version exists and which command to run (`brew upgrade gtf`, `sudo apt install --only-upgrade gtf`). See [[Updating gtf]].

## Where gtf finds its library

gtf carries a copy of its library inside the program, so a single binary works on its own. It prefers an installed library of exactly the same version, looked for in `$GTF_LIBDIR`, then next to the program (`../lib/gtf`), then `~/lib/gtf`, `/usr/local/lib/gtf` and `/usr/lib/gtf`. Without an installed library gtf unpacks its own copy into your user cache folder. The option `-L<dir>` names a folder to use as the TinyFugue library instead (see [[Command line and environment]]).

See also: [[First run]], [[Updating gtf]], [[Files and folders]].

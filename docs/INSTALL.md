# Installing and updating gtf

## Install

```
curl -fsSL https://raw.githubusercontent.com/itsnotyoutoday/gtf/main/install.sh | sh              # just you
curl -fsSL https://raw.githubusercontent.com/itsnotyoutoday/gtf/main/install.sh | sh -s -- --global   # everyone on the machine
```

| | user (default) | `--global` |
|---|---|---|
| program | `~/bin/gtf` | `/usr/local/bin/gtf` |
| library | `~/lib/gtf` (`tf/`, `gtf/` with the demos in `gtf/examples/`, `docs/`) | `/usr/local/lib/gtf` |
| privileges | none | uses `sudo` when needed |
| your files | `~/.gtf` (settings, update state, layouts) | `~/.gtf` per user |

The installer asks which folders to use and whether to check for updates daily (answers come from your terminal
even when piped from curl). Every question has a switch, so it also runs unattended:
`--user|--global`, `--prefix DIR`, `--bindir DIR`, `--libdir DIR`, `--update-check|--no-update-check`,
`--channel stable|prerelease`, `--version 0.0.2`, `--yes`, `--uninstall`.

It downloads the release tarball for your OS and CPU (Linux, FreeBSD, macOS; amd64, arm64, 386, armv7), verifies
its SHA-256 against the release's `SHA256SUMS` (it refuses to install anything it cannot verify), and replaces the
program atomically, so it is safe to run while an old gtf is running. Re-running it upgrades; an up-to-date
install is left alone. `--uninstall` removes the program and library (never `~/.gtf`).

A release tarball unpacks to `gtf-VERSION-os-arch/` with `bin/gtf`, `lib/gtf/`, `install.sh`, `README.md`, `LICENSE`.
The demos (`/layoutdemo`, `/lcars`) are part of the library and load themselves when you type their names.
The static binary needs nothing else: the library is also embedded in it, and is used when no installed library of
the same version is found.

### Where gtf finds its library

In order: `-L<dir>` on the command line; an installed library next to the binary (`../lib/gtf`), `$GTF_LIBDIR`,
`~/lib/gtf`, `/usr/local/lib/gtf`, `/usr/lib/gtf` (only when its `install.json` says it is the same version as the
program); otherwise the copy embedded in the binary, extracted to the user cache directory.
Put your own startup commands in `~/.tfrc` (gtf also reads `~/tfrc`, `./.tfrc` and `./tfrc`; the file name is TinyFugue's), and
anything meant for every user of a global install in `<libdir>/tf/local.tf`, the file the library loads (it survives updates and
reinstalls; a `local.tf` at the top of `<libdir>` is kept too).

### Debian/Ubuntu and RedHat/Fedora packages

Each release also publishes `gtf_VERSION_ARCH.deb` and `gtf-VERSION-1.ARCH.rpm` (amd64, arm64, 386, armv7; a pre-release such as `0.0.2-alpha1` is packaged as `0.0.2~alpha1`, which sorts before `0.0.2`):

```
sudo apt install ./gtf_0.0.2_amd64.deb          # or: sudo dpkg -i ...
sudo dnf install ./gtf-0.0.2-1.x86_64.rpm        # or: sudo rpm -i ...
```

They install `/usr/bin/gtf` and `/usr/lib/gtf`, and `lib/gtf/install.json` marks them as owned by the package
manager. The package build is compiled with `-tags pkgbuild`: it contains **no code to fetch or install anything
from GitHub** (the binary is about 1.6 MB smaller). `/update` there only compares the running gtf with the one the
package manager has installed on disk:

- same version: "this is the newest gtf installed on this system", and that newer releases are for your system
  administrator to install (`sudo apt install --only-upgrade gtf`, `sudo dnf upgrade gtf`);
- a newer one is installed (the administrator upgraded the package while your gtf was running): it says so, and
  `/update -r` restarts into it without dropping your connections.

`/update -y` explains that it cannot install. The same message appears once at startup when a newer installed
version is waiting. A normal-build gtf that happens to live in `/usr/bin` or has a package record is treated the
same way and never overwrites those files. The shell installer above stays the way to install without a package
manager (and without root).

### macOS and Linux: Homebrew

```
brew install itsnotyoutoday/tap/gtf          # the tap is github.com/itsnotyoutoday/homebrew-tap
brew upgrade gtf
```

Brew installs the release binary and library under its Cellar and records `managed_by: brew` in `lib/gtf/install.json`.
Like the deb/rpm packages, brew owns those files: `/update` only reports a newer version and says `brew upgrade gtf`.
Each release publishes the formula as `gtf.rb` (rendered by `scripts/homebrew_formula.sh`); copying it to the tap's
`Formula/gtf.rb` is the one manual step of a release (it can be automated with a token for the tap repository).

## Update

| | |
|---|---|
| `/update` | look on GitHub and say whether a newer version exists |
| `/update -y` | install it: the program and the library directory are replaced together; the old ones are kept as `gtf.old` and `lib/gtf.old`; your `local.tf` is carried over. The running gtf keeps going |
| `/update -y -r` | install and restart into the new version without dropping connections (see RESTART.md; where in-place restart is not available it tells you to restart by hand) |
| `/update -channel=stable` | follow only final releases (`prerelease`, the default, also takes alpha/beta) |
| `/update -check=off` | stop the once-a-day check at startup |
| `gtf --update [-y]` | the same from a shell, without starting the client; use `sudo gtf --update -y` for a global install |

Updating needs permission to write where gtf is installed. If you do not have it, gtf says so and shows the
`sudo gtf --update -y` command. Nothing is touched before the download has been verified. A development build
(no release version) never updates itself.

At startup gtf tells you in one line when a newer version is known (it asks GitHub at most once a day, never
blocks, and stays silent when offline). Set `GTF_NO_UPDATE_CHECK=1` to switch that off for a session.

Settings live in `~/.gtf/config.json` (`update_check`, `update_channel`); the last check in `~/.gtf/update-state.json`.

## Releasing (maintainers)

Tags follow the SquatClient daemon's convention: `v0.0.2-rel` is a final release (files and version `0.0.2`),
`v0.0.2-alpha1` / `-beta1` are pre-releases. Pushing such a tag runs `.github/workflows/release.yml`, which tests,
builds every platform with `scripts/dist.sh` and publishes the tarballs plus `SHA256SUMS`.

`make dist` builds the tarball for this machine; `make dist-all` every platform; `make packages` the .deb and .rpm
(needs `nfpm`, or network access for `go run`; the packaging in `packaging/nfpm.yaml` is built by CI on release). `scripts/test_install.sh` tests
the installer and the updater end to end against a local fake of GitHub.

The `.deb`, `.rpm` and Homebrew steps of the release workflow only run when the repository variable
`PUBLISH_PACKAGES` is `true` (Settings > Secrets and variables > Actions > Variables); until then a release publishes just
the tarballs and the checksums that the installer and `/update` use.

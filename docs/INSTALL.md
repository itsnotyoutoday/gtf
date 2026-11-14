# Installing and updating gtf

## Install

```
curl -fsSL https://raw.githubusercontent.com/itsnotyoutoday/gtf/main/install.sh | sh              # just you
curl -fsSL https://raw.githubusercontent.com/itsnotyoutoday/gtf/main/install.sh | sh -s -- --global   # everyone on the machine
```

| | user (default) | `--global` |
|---|---|---|
| program | `~/bin/gtf` | `/usr/local/bin/gtf` |
| library | `~/lib/gtf` (`tf/`, `gtf/`, `examples/`, `docs/`) | `/usr/local/lib/gtf` |
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
The static binary needs nothing else: the library is also embedded in it, and is used when no installed library of
the same version is found.

### Where gtf finds its library

In order: `-L<dir>` on the command line; an installed library next to the binary (`../lib/gtf`), `$GTF_LIBDIR`,
`~/lib/gtf`, `/usr/local/lib/gtf`, `/usr/lib/gtf` (only when its `install.json` says it is the same version as the
program); otherwise the copy embedded in the binary, extracted to the user cache directory.
Put your own startup commands in `~/.gtfrc`, and anything meant for every user of a global install in
`<libdir>/local.tf` (it survives updates).

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

`make dist` builds the tarball for this machine; `make dist-all` every platform. `scripts/test_install.sh` tests
the installer and the updater end to end against a local fake of GitHub.

Until the scripting engine (github.com/itsnotyoutoday/gtfparser) is published, `go.mod` carries a temporary
`replace` that points at a sibling checkout, so the release workflow can only build once that is removed
(publish the engine tag the client needs, then `go get` it and delete the `replace`).

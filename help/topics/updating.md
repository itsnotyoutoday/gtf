---
title: Updating gtf
topics: update, updating, upgrade, /update, gtf-update
section: Using gtf
order: 70
---
# Updating gtf

gtf can check for a newer release itself and install it.

| command | does |
|---|---|
| `/update` | ask GitHub whether a newer version exists and say so |
| `/update -y` | download and install it; the running gtf keeps going |
| `/update -y -r` | install, then restart into the new version without dropping connections |
| `/update -channel=stable` | follow only final releases |
| `/update -channel=prerelease` | also take alpha and beta releases |
| `/update -check=off` | stop the daily check (`-check=on` starts it again) |

From a shell, without starting gtf: `gtf --update` to check, `gtf --update -y` to install (`sudo gtf --update -y` for an install in `/usr/local`).

## What an update does

It downloads the release file for your system, verifies its SHA-256 checksum, and only then replaces the program and the library folder together. The old ones are kept as `gtf.old` and `lib/gtf.old`. Your files in `~/.gtf` and your startup file are not touched. (The library folder is replaced as a whole, except that your `local.tf` is carried over; see [[Configuration and startup files]].) If gtf cannot write where it is installed, it says so and shows the `sudo` command to use. Nothing changes before the download has been verified.

The program you are running is not changed until you restart it: `/quit` and start again, or use `/restart` — [[Restarting without dropping connections]] or `/update -y -r`.

## The daily check

Once a day at startup gtf asks GitHub whether a newer version exists and, if so, tells you in one line. It never delays the start, and it says nothing when you are offline. The settings are in `~/.gtf/config.json`:

```
{ "update_check": true, "update_channel": "prerelease" }
```

`GTF_NO_UPDATE_CHECK=1` in the environment switches the check off for one session. A development build, with no release version, never updates itself.

## Packages and Homebrew

If gtf came from a `.deb`, an `.rpm` or Homebrew, the package manager owns the files. `/update` then compares the running gtf with the one installed on disk and tells you what to run (`brew upgrade gtf`, `sudo apt install --only-upgrade gtf`, `sudo dnf upgrade gtf`); `/update -y` explains that it cannot install. If an administrator has upgraded the package while your gtf was running, gtf tells you, and `/update -r` restarts into the new one. Package builds contain no code to download or install anything from GitHub.

See also: [[Installing gtf]], [[Versions and update checks]].

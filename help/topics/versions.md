---
title: Versions and update checks
topics: versions, versioning, gtf-version, release-channels
section: About
order: 40
---
# Versions and update checks

## Reading the version

```
/version                     gtf's version and copyright
gtf -v                       the same from a shell
/eval /echo %version         the version line, from a script
/ver                         the compatible TinyFugue release
```

The banner at startup also shows the version. A version looks like `0.0.2` for a release and `0.0.2-alpha5` or `0.0.2-beta1` for a pre-release; a pre-release sorts before the release it leads up to. gtf is still early: expect rough edges, and expect behaviour to change between pre-releases.

`/version` also names the scripting engine (gtfparser) that is built in. When you report a problem, include both lines — [[Reporting a problem]].

## Which TinyFugue

gtf aims to be compatible with TinyFugue **5.0 beta 8**, whose standard library and manual it ships. `TFVERSION` and `/ver` give that number, because scripts compare against it when they need to know what the language can do. `%version` is gtf's own.

## Releases

New versions are published as releases on GitHub, with a file for each operating system and processor, a checksum file, and `.deb` and `.rpm` packages. Release tags written `vX.Y.Z-rel` are final releases and `vX.Y.Z-alpha1` or `-beta1` are pre-releases.

## Update checks

* Once a day, at startup, gtf asks GitHub whether a newer version exists and says so in one line. It never delays the start and stays quiet when offline.
* `/update` makes that check now; `/update -y` installs — [[Updating gtf]].
* The **channel** decides what counts as newer: `stable` takes only final releases, `prerelease` (the setting until final releases exist) also takes alphas and betas. `/update -channel=stable` changes it.
* `/update -check=off` stops the daily check for good; `GTF_NO_UPDATE_CHECK=1` stops it for one run. Both settings are described in [[Updating gtf]].
* A gtf built from source without a release version never updates itself.
* If gtf came from a package manager, it only compares versions and tells you which package command to run.

gtf contacts only GitHub, and only to ask about releases and to download one you chose to install. The request identifies itself only as `gtf-update`; GitHub sees your network address, as any web server does.

See also: [[Installing gtf]], [[License and credits]].

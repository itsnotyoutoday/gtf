---
title: Reporting a problem
topics: gtf-bugs, report-problem, bug-report
section: About
order: 30
---
# Reporting a problem

**Problems with gtf go to the gtf project, not to the TinyFugue authors.** gtf is a separate program. Something that behaves differently from TinyFugue is a gtf problem even when the script is an old TinyFugue one; the TinyFugue authors cannot help with it and did not write gtf.

Report it as an issue at [github.com/itsnotyoutoday/gtf/issues](https://github.com/itsnotyoutoday/gtf/issues). Before you do, a quick look at [[Where gtf differs from TinyFugue]] tells you whether it is a known difference.

## What to put in a report

* The version: the output of `/version`, or `gtf -v` from a shell. Add how you installed it (installer, package, Homebrew, built from source).
* Your operating system and terminal program, and the value of `TERM`. Say whether you use tmux or screen, and whether the terminal does 24-bit colour.
* What you did, what you expected, and what happened. Exact steps, in order, help most. If a game is involved, say which one and whether it uses GMCP or the console protocol (`/cmcp status` shows it).
* For a scripting problem: the smallest script that shows it. `/list name` shows a macro as gtf has it.
* For a display problem: the size of your terminal and a copy of the screen as text, or a picture.
* Whether `gtf -f` (no startup file) changes anything.

## What to leave out

Do not post passwords, the world file (it holds passwords in clear), pairing codes, anything from `~/.gtf/squatter`, or a restart state file. Check logs for things you would not want public — [[Logging]].

A security problem should not be described in a public issue: open an issue that says only that you have a security report and ask how to send it, or use the repository's private vulnerability reporting if it is offered. See [[Squatter security]] for how the remote-access feature is protected.

## Documentation problems

A page that is wrong, unclear or missing is a problem too. These pages are generated from files in the repository (`help/topics`), so say which page and what is wrong, and it can be fixed there.

See also: [[License and credits]], [[Versions and update checks]].

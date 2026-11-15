---
title: Command line and environment
topics: gtf-options, gtf-environment, gtf-command-line, environment-gtf
section: Reference
order: 30
---
# Command line and environment

```
gtf [-L<dir>] [-f] [-c<cmd>] [-nlq] [-v] [world | host port]
gtf --update [-y] [-channel=stable|prerelease]
```

| option | meaning |
|---|---|
| `world` | connect to this saved world when the start is finished |
| `host port` | connect to this address, as `/connect host port` does |
| `-L<dir>` | use `<dir>` as the TinyFugue library folder instead of the installed or built-in one |
| `-f` | do not load your startup file (`~/.tfrc` and its alternatives) |
| `-c<cmd>` | run a command at startup, after your startup file; repeat the option for more commands |
| `-n` | do not connect automatically |
| `-l` | no automatic login |
| `-q` | quiet login (hides the first lines of the server's output while logging in) |
| `-v`, `--version` | print the version and exit |
| `-h`, `--help` | print the usage and exit |
| `--update` | check for a newer version from a shell; with `-y` install it — [[Updating gtf]] |

The letters `n`, `l` and `q` can be combined (`-nq`). gtf must be run in a terminal: with its input or output redirected it exits with "standard input and output must be a terminal". An option it does not know stops it with a message and the usage line.

```
gtf -n -c'/layout use sidebar'  no connection, start with the sidebar layout
gtf -c'/set more=on' mymud      set one thing, then connect
```

## Environment variables

| variable | effect |
|---|---|
| `HOME` | where `~/.tfrc`, `~/.gtf` and `~/tiny.world` are |
| `TERM` | your terminal type; gtf tells games about it (it is also the variable `%TERM` inside gtf) |
| `LANG`, `LC_ALL`, `LC_CTYPE` | when none of them says UTF-8, console screens draw with ASCII only (see `%cmcp_ascii`) |
| `GTF_LIBDIR` | the first place gtf looks for an installed library — [[Installing gtf]] |
| `GTF_NO_UPDATE_CHECK` | set to `1` to switch off the daily update check for that run |
| `GTF_RESTART_TIMEOUT` | how long `/restart` waits for the new copy, for example `20s` — [[Restarting without dropping connections]] |

There are a few more that gtf uses internally or that exist for developers (`GTF_RESTART_STATE`, `GTF_RESTART_FDS`, `GTF_RESTART_READY` carry a restart to the new process; `GTF_UPDATE_API`, `GTF_UPDATE_WEB` and `GTF_UPDATE_REPO` point the updater at another server for testing; `GTF_DUMP_DIR` records console screens). Do not set them in normal use.

## Exit status

0 after a normal `/quit`, `-v` or `-h`; 2 for an unknown option; 1 when gtf cannot start (for example when it is not run in a terminal).

See also: [[First run]], [[Files and folders]].

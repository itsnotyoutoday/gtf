---
title: Squatter settings
topics: squatter-variables, squatter-settings
section: Remote access
order: 70
---
# Squatter settings

All are ordinary variables: `/set name=value`. gtf re-reads them after every command, so a change applies at once.

| variable | default | meaning |
|---|---|---|
| `squatter` | off | `on` starts it |
| `squatter_port` | 10328 | TCP port (0 = any free port, see `squatter_addr`) |
| `squatter_bind` | 0.0.0.0 | address to listen on (127.0.0.1 = this machine only): [[Squatter address and port]] |
| `squatter_name` | host name | the name announced to clients |
| `squatter_dir` | ~/.gtf/squatter | certificate, paired devices, pairing codes |
| `squatter_tls` | on | off = plain ws:// (loopback only) |
| `squatter_cert`, `squatter_key` | unset | your own certificate and key (both or neither) |
| `squatter_kind` | tf | tf = a TinyFugue-style squatter on /tfsp (SquatClient enables control, worlds, exec); swp = a plain squatter on /swp, sessions only |
| `squatter_scrollback` | 20000 | entries kept per session for replay |
| `squatter_max_clients` | 8 | simultaneous clients |
| `squatter_max_sessions` | 32 | sessions held (oldest ended ones go first) |
| `squatter_pair_ttl` | 600 | seconds a pairing code is valid (10 to 604800) |
| `squatter_pair_role` | admin | the role `/pair` gives: viewer, operator, admin |
| `squatter_commands` | on | off = nothing a remote sends is run as a command |
| `squatter_restrict` | file | what code from a remote may touch: none, shell, file, world (each includes the ones before it) |
| `squatter_gtf` | on | off = serve every client as a plain squatter |
| `squatter_naws` | active | which terminal width the game is told: active, min, max or local |
| `squatter_local` | off | on = also forward gtf's own text to remotes: [[What remote clients see]] |
| `squatter_world_save` | on | off = worlds a remote edits are not written to the world file |

## Written by the squatter (read only)

`squatter_state` (off, listening, error), `squatter_error`, `squatter_addr`, `squatter_fingerprint`, `squatter_peers`. A start failure (port in use, bad certificate) sets `squatter_state=error`, prints why, and is retried when a variable changes or you type `/squatter on`. A bad value prints a warning once and keeps the default.

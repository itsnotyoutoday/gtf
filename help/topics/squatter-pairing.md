---
title: Pairing devices
topics: squatter-pairing, pair, /pair, pairing
section: Remote access
order: 40
---
# Pairing devices

`/pair [viewer|operator|admin]` prints a one-time pairing code (default role: `squatter_pair_role`, admin), when it expires, the TLS fingerprint and the address to connect to. The code works once. Nothing `/pair` prints goes into history, the log or any session's scrollback.

## Roles

| role | can |
|---|---|
| viewer | read output and scrollback only |
| operator | also type into games |
| admin | also run commands in this client (`/` commands, scripts), edit worlds, and change squatter settings |

An admin can do whatever a command typed here can do, including `/sh` and `/load` unless restricted (`squatter_restrict`, `/restrict`). Pair only your own devices as admin, and give other people viewer or operator. See [[Squatter security]].

## The fingerprint

The first time SquatClient connects it shows the certificate's fingerprint and remembers it. Compare it with `/squatter fingerprint` or the line `/pair` prints. A different fingerprint later means a different certificate: do not accept it unless you changed it yourself.

## Managing devices

| command | does |
|---|---|
| `/squatter list` | connected clients, paired devices, sessions |
| `/squatter who` | who is attached, which world, who is typing |
| `/squatter kick <name or id>` | disconnect a client (it may reconnect) |
| `/squatter unpair <name or id>` | forget a device for good and disconnect it |

The name is the whole rest of the line (device names have spaces); an id prefix needs 4 or more characters.

Five failed pairing attempts from one address in five minutes lock that address out for five minutes.

# gtf as a squatter and tf bridge

With `squatter=on`, gtf itself is the always-on session holder that SquatClient pairs with, and
at the same time the "tf bridge": SquatClient sees gtf's world connections, gets their scrollback
on every (re)connect, types into them, and `/commands` it sends run in gtf's own scripting engine
(triggers, hooks, macros), exactly as they do for the terminal user. With the default
(`squatter=off`) gtf behaves exactly as before: no listener, no state directory, no work per line.

This is **one program**. There is no `squatterd`, no `tf-squatterd`, no helper process, no
subprocess and no local socket or pipe between a bridge and gtf. The listener, TLS, pairing and the
wire protocol live in `internal/squatter`, inside the gtf process, and reach gtf's sessions, screens,
input path and engine through direct Go calls that are posted onto the engine Runner's goroutine like
every other event source.

## Quick start

In your rc file (gtf loads `~/.tfrc`; `/set` at runtime works the same):

```
/set squatter=on
```

Then, in gtf:

```
/squatter            show state, listen address, TLS fingerprint
/pair                print a one-time pairing code
```

In SquatClient add a squatter of type **"gtf (GoTinyFugue)"** (it is stored as kind `tf-squatterd`
with flavor `gtf`; older SquatClients can use kind "TinyFugue", with the TinyFugue-only features
unavailable) with host `localhost` (or the machine's name), port `10328`, enter the pairing code and check that the fingerprint SquatClient shows is the one gtf
printed. From then on the device is remembered; the code is dead after one use.

## Variables

All ordinary tf variables. gtf re-reads them after every command and every 250 ms, so a `/set`
(typed, from a macro, or sent by a remote admin) takes effect at once: the listener is restarted on
the new settings, connected clients are dropped (they reconnect and replay), **sessions and paired
devices are kept**. Switching `squatter=off` drops everything the squatter held.

| variable | default | meaning |
|---|---|---|
| `squatter` | `off` | `on` starts the daemon |
| `squatter_port` | `10328` | TCP port (`0` = any free port, see `squatter_addr`). 10328 is tf-squatterd's port; squatterd itself uses 10327 |
| `squatter_bind` | `127.0.0.1` | address to listen on. Anything but loopback prints a warning (see Security) |
| `squatter_name` | the host name | the instance name announced in the welcome |
| `squatter_dir` | `~/.gtf/squatter` | state directory (mode 0700): TLS key and certificate, paired devices, pairing codes |
| `squatter_kind` | `tf` | `tf`: announce `tf-squatterd` on `/tfsp` (SquatClient treats gtf as a TinyFugue, enabling control/world/exec features); `swp`: announce plain `squatterd` on `/swp`, sessions only |
| `squatter_tls` | `on` | `off` serves plain `ws://`, only allowed with a loopback `squatter_bind` |
| `squatter_cert`, `squatter_key` | unset | use this certificate and key instead of the generated one (both or neither) |
| `squatter_scrollback` | `20000` | entries kept per session for replay (memory only) |
| `squatter_max_clients` | `8` | simultaneous clients |
| `squatter_max_sessions` | `32` | sessions held; the oldest *ended* ones are forgotten first |
| `squatter_pair_ttl` | `600` | seconds a pairing code stays valid (10 to 604800) |
| `squatter_pair_role` | `admin` | role given by `/pair` without an argument: `viewer`, `operator`, `admin` |
| `squatter_commands` | `on` | `off`: nothing a remote client sends is ever executed as a command (admin `tf.exec` and `/lines` become refused / plain text) |
| `squatter_restrict` | `file` | what code from a remote client may touch: `none` (everything, as the owner), `shell` (no shell: `/sh`, `/quote !`, pipes), `file` (also no file access: `/load`, `/log`, `/save`, `tfopen`, ...), `world` (also no new connections). The restriction follows the code: macros, triggers, hooks and timers a peer defines carry the peer's origin and stay restricted when they fire later; the owner at the terminal is never restricted. Applies at once; lifting it is the owner's choice (`/set squatter_restrict=none`). See the engine's origin documentation (`docs/engine/origins.md`) |
| `squatter_gtf` | `on` | `off`: never negotiate the gtf extension ([SWP-GTF.md](SWP-GTF.md)); every peer is served as a plain tf-squatterd client |
| `squatter_naws` | `active` | which terminal width the game is told when several clients are attached: `active` (whoever last sent input: a peer or this terminal), `min`, `max` of all attached, or `local` (only this terminal, the behaviour before the extension). See "Shared control" below |
| `squatter_world_save` | `on` | `off`: a world edited by a peer is not written to the world file (it stays in memory until you `/saveworld`) |

Output variables, written by the squatter (set them yourself and they are overwritten):
`squatter_state` (`off`, `listening`, `error`), `squatter_error` (the reason), `squatter_addr`
(`host:port` actually bound), `squatter_fingerprint` (the TLS fingerprint, empty without TLS),
`squatter_peers` (clients connected now). A bad value in any variable prints a warning once and keeps
the default; a start failure (port in use, bad certificate) sets `squatter_state=error`, prints the
reason, and is not retried until a variable changes or you type `/squatter on`.

## Commands

| | |
|---|---|
| `/squatter` or `status` | state, address, fingerprint, name, directory, number of clients and sessions |
| `/squatter on`, `off` | sets `%squatter` (and retries after an error) |
| `/squatter list` | connected clients, paired devices (role, last seen, connected) and sessions held |
| `/squatter who` | everyone attached (this terminal included): role, the world each views, who is typing |
| `/squatter kick NAME` | disconnect a client by device name (the whole rest of the line: names like `SquatClient on my host` work) or id prefix of 4+ characters (it may reconnect). Several different devices matching is an error that lists them: use the id |
| `/squatter unpair NAME` | forget a paired device (same matching) and disconnect it |
| `/squatter fingerprint` | print the TLS fingerprint |
| `/squatter port N`, `bind ADDR`, `name NAME` | set `%squatter_port`, `%squatter_bind`, `%squatter_name` |
| `/pair [viewer\|operator\|admin]` | a one-time pairing code, printed like `squatterd pair`:<br>`Pairing code:    ABCD-EFGH   (role: admin)`<br>`Valid until:     <time> (10m0s, single use)`<br>`TLS fingerprint: AA:BB:...`<br>`Connect to:      wss://host:10328/tfsp` |

`/pair` prints the code only to the person who ran it: it is not added to history or the log
(`/recall`, `/log`) and, when an admin runs it from SquatClient, it comes back in the `tf.exec` reply and
never enters a session's scrollback. **The same holds for everything `/squatter` prints** (state, TLS
fingerprint, state directory, device names, ids and roles) and for the squatter's own notices and audit
lines (start failures, world edits by peers, world file backups): they appear on your screen only, never in a
session's scrollback, which every paired device (viewers included) can replay. Codes are stored in `pairing.json` (0600) until used or expired, as in the reference daemon.

Hooks (for scripts; enabled by gtf, args are `device-name role`, the device name may contain spaces
so use `%{*}` or `%1`..): `SQUAT_PAIR` (a device was just paired), `SQUAT_CONNECT`,
`SQUAT_DISCONNECT`:

```
/def -hSQUAT_CONNECT = /echo -aB phone connected: %{*}
```

The hooks are an engine feature (`core:` commit "optional hook events SQUAT_CONNECT, SQUAT_DISCONNECT,
SQUAT_PAIR" in gtfparser, listed in its CORE-CHANGES.md); gtf needs an engine that has them.

## Walk-through of a pairing

1. `/set squatter=on`. gtf creates `~/.gtf/squatter/` (0700) and a self-signed ECDSA P-256 certificate
   (key 0600) the first time, and listens.
2. `/pair` prints the code and the certificate fingerprint.
3. SquatClient connects, receives the TLS certificate and shows its fingerprint (trust on first use);
   compare it with the printed one, accept, enter the code. SquatClient sends its ed25519 public key
   with a signature over the server's challenge plus the code; gtf registers the device (`devices.json`)
   and consumes the code.
4. Later connections need no code: the signature proves possession of the key. `/squatter unpair NAME`
   revokes a device immediately (its connection is dropped and a reconnect is refused).
5. Five failed attempts from one address in five minutes lock that address out for five minutes (the
   reference daemon's rate limit, `rate_limited`).

## What SquatClient sees: mapping gtf onto the protocol

The wire format, authentication, pairing, fingerprint display and error codes are those of
squatterd / tf-squatterd (SWP v1, kind `tf-squatterd`, endpoint `/tfsp`); the code is a copy of the
reference packages (below), not a reimplementation.

| protocol | gtf |
|---|---|
| **session** | one gtf `Session` (one world socket). Id `g<time>-<n>`, `name` = the world name, host/port/tls from the world. States: `connecting` while resolving/dialing, `connected`, `ended` (end reasons `connect_failed`, `remote_closed`, `client_quit` for `/dc` and a remote disconnect, with `endDetail`). An ended session stays (history kept) until dismissed or pushed out by `squatter_max_sessions`; reconnecting a world makes a **new** session id (the zombie keeps the old one, as tf keeps the zombie). Connections that already exist when the squatter is switched on are registered, with the text their screens still hold (up to 2000 lines) |
| **line** | everything that is shown on that world's screen, after gtf's processing: server text with the triggers' highlights and substitutions applied (`kind: game`), the user's and remote clients' sent lines (`echo`, `origin` = `tf` or a device id), and everything else gtf prints into that world (`system`: trigger/macro output, `% Connection to ... closed`, results of commands run by a remote admin). **Gagged lines (`/def -ag`, `/gag`) and quiet-login lines are not shown to remote clients** (the real tf-bridge flags them instead; deviation, see below). Colours come from the cells gtf draws (palette 0-255 and truecolor, bold, dim, italic, underline, reverse) |
| **prompt** | a partial line flushed by GA/EOR or gtf's 250 ms rule is a `line` with `prompt: true` (gtf consumes it, so no duplicate follows) |
| **scrollback** | memory ring per session (`squatter_scrollback`), seq-stamped; `attach` with `since`/`maxBacklog` replays exactly as in PROTOCOL.md 5.1 (`attached`, `backlog` chunks of 500 entries, live entries after `done`, `gap` when entries fell out). `ack` tracks `unread` per device. Nothing is written to disk |
| **send** | `send` from an operator or admin writes the text raw to the game (never parsed, so `%`, `;` and `/` mean nothing) and records an `echo` line. From an **admin**, with `squatter_commands=on`, a text starting with `/` is run as a gtf command in that session's world (as if typed there: `%sub` rules, hooks, macros), its output lands on that world's screen and so in the scrollback; the command line itself is echoed. `send` with `noEcho` (passwords) is delivered with no echo, record or history. While the game has asked for no echo (password prompt) ordinary echoes are masked as `********` |
| **session.connect** | `world.ref` = a world defined in gtf (autologin applies, as `/connect`), or `host`+`port`(+`tls`) for an ad-hoc connection, which becomes a temporary world named after `world.name` (made unique with `-2`, `-3`.. and limited to letters, digits and `-_.:[]`; without a usable name `(unnamedN)`). Always opened **in the background** (`/connect -b`) |
| **disconnect / dismiss** | `/dc` equivalent; dismiss only for ended sessions |
| **GMCP / MSDP** | received packages become `gmcp` / `msdp` entries and are kept as the latest-value snapshot (`state`, sent after every attach and on `state.get`); `gmcp.send` / `msdp.send` go through the connection's negotiated protocol (an error if the game never negotiated it). gtf's own scripts see the same data (`gmcp_get()`, `/def -hGMCP`) |
| **control.\*** | gtf has one model: the terminal and every remote operator type at the same time (`input: both`) and gtf's own triggers always run (`automation: tf`). `control.get` reports that, `control.set` answers with the real state and changes nothing (no error). Peers with the gtf extension also get the real shared state in `shared` |
| **session.tf.fg** | true for the session of the world gtf's own terminal shows (the foreground world); changes with `/fg`, and a `session.state` goes out when it does |
| **world.list** | gtf's world database (never passwords: `passSet` only, never macro files). Peers with the extension also get each world's `rev` |
| **tf.exec** | admin only, `squatter_commands=on`: one line starting with `/`, run on the engine goroutine with its output captured and returned (`ok=false` when the command reported an error or the command is unknown); nothing is displayed locally or recorded |
| **resize** | ignored for a plain peer. With the extension it feeds the `squatter_naws` policy (below). `caps` is ignored: capabilities toward the game stay those of gtf's own terminal |
| roles | `viewer` (read only), `operator` (send, connect, disconnect, dismiss, GMCP/MSDP), `admin` (and legacy full-trust `owner`: commands and `tf.exec`) |
| features announced | `tf control worlds state gmcp msdp noEcho` and `tfexec` when commands are on |

### Foreground model

A remote client never changes gtf's foreground world. A remote `session.connect` opens in the
background, a remote command runs with the target session's world as its context (like a trigger in a
background world) and prints on that world's screen. The local user sees remote activity as ordinary
text in the corresponding world (and the usual background-activity alerts); the remote client sees what
the local user types and the replies. A script a remote admin runs can of course call `/fg` itself.

### Not implemented (answered with an error or ignored)

`world.put`, `world.delete`, `world.rename` for a peer that did not negotiate the gtf extension (answered `unsupported`; use `tf.exec` with `/addworld`, `/unworld`, `/renameworld`; with the extension they work, see below), `tf.script.*` (`unsupported`; use `tf.exec` with `/load`), takeover/ask control, `reload.status` / `admin.reload`, named users and
ssh-key logins (`user.*`, `admin.*`: the reference code is present and answers them, but gtf only creates
paired devices, so they have no accounts to work on), disk logs of sessions. Compatibility with a real
SquatClient is covered by its own test (`scripts/gtf-integration.sh` in the SquatClient repository, which
pairs with a real gtf and exercises pairing, replay, sending, `tf.exec`, GMCP/MSDP, reconnects, kick and
unpair); the extension below is tested here against the reference wire code with a plain and an extension
peer connected at once, not yet against SquatClient itself.

## Shared control (the gtf extension)

A SquatClient that announces the extension (docs/[SWP-GTF.md](SWP-GTF.md) is the specification) gets the gtf
model instead of the TinyFugue one: no takeover, no ask. The game sessions, their scrollback, the world
database, macros and variables are shared; every client has its own input line, its own current session and
its own width. Whoever sends can send at any time; every echoed line carries who sent it (`author`: the
device name, or `local`). Everybody can see who is attached, which session each views and who is typing
(`/squatter who` shows it on this side). Roles still gate everything; commands run under `squatter_commands`
(and the owner's execution policy: see SWP-GTF.md, "Roles, limits, policy").

**Width the game is told (`squatter_naws`).** Lines travel unwrapped, each client wraps for itself; the
one shared number is the NAWS width, per session: `active` = the client that last sent input (a peer's
`send`, or you submitting a line); `min` / `max` = smallest / largest of all attached; `local` = only this
terminal. Corner cases: a peer that detaches or disconnects drops out at once; no peers attached means this
terminal; sizes are clamped to 10..1000 and zero, negative or over-1000 sizes are refused; a viewer cannot
resize; changes are debounced (300 ms) and sent once per session; the rows the game is told are always this
terminal's. Only peers that negotiated the extension take part; a plain peer's `resize` stays ignored.

**World edits by peers.** An admin peer with the extension can add, update, rename and delete worlds
(`world.put` / `world.rename` / `world.delete`), with optimistic concurrency: each world has a `rev`
that changes on every change, an edit must name the `rev` it is based on, and a stale one is refused with the
current world so the peer can merge. The change goes to the engine's world database through its public API
(`PutWorld`, `RenameWorld`, `DeleteWorld`), every attached extension peer is told (`world.changed`, with
`author`), and one audit line on your screen says who did what (field names, never values or the password).
Edits you make yourself with `/addworld`, `/unworld`, `/renameworld` or `addworld()` are announced to the
peers too. A peer can set a world's password (write-only) but never read it, and can neither read nor set a
world's macro file (that is code gtf would load).
*Persistence:* after each accepted edit gtf rewrites the file named by the `WORLDFILE` macro (what a bare
`/saveworld` writes; the stock library defines it) with every world, mode 0600, through a temporary
file and a rename, unless `squatter_world_save=off`, `/restrict` forbids file access, or no `WORLDFILE` is
defined (the reply then says `saved:false`). Like `/saveworld` this writes only `addworld()` lines: comments
and other commands in a hand-edited file are not kept. So the previous file is copied to
`<file>.gtf-bak-<date-time>` first whenever it is not exactly what gtf itself wrote last (the first edit of a
run therefore backs up your hand-edited file; the 5 newest backups are kept) and you are told on your screen.
Worlds that only exist in memory (typed `/addworld`, not yet saved) are written too, as with `/saveworld`.
The file is written on the engine goroutine (it is small).

## Security notes

* **Loopback by default.** `squatter_bind` is `127.0.0.1`. Reaching gtf from another machine means
  either an ssh tunnel (`ssh -L 10328:localhost:10328 host`) or binding a public address, which prints a
  warning each time the listener starts: the pairing port is then reachable by anyone who can route to
  it. Everything is TLS and device-key authenticated, the rate limit applies per address, but the
  attack surface is a TLS+WebSocket server in the process that holds your game passwords.
* **A paired admin device can run commands.** `/pair` defaults to role `admin` (`squatter_pair_role`)
  because forwarding TinyFugue scripting is the point; an admin can do whatever gtf's engine lets a
  typed command do, including `/sh` and `/load` unless you restricted it (`/restrict`). Pair
  operator or viewer devices with `/pair operator`, or set `squatter_commands=off`.
* TLS is on unless `squatter_tls=off`, which is refused for non-loopback binds. The certificate is
  self-signed; trust is by fingerprint (shown by `/squatter fingerprint` and `/pair`). Supply
  `squatter_cert`/`squatter_key` to use your own.
* The state directory is created 0700, the key 0600, `devices.json` and `pairing.json` 0600. Pairing
  codes and passwords are never written to a log (the squatter has no log; the wire server's logger is
  discarded). Pairing codes are displayed once, privately (see `/pair`).
* A client that cannot keep up (1000 queued frames, 10 s blocked write, 60 s silence) is dropped alone;
  frames over 1 MiB, binary frames and 20 malformed frames in a row close the connection.

## Limits (from the reference)

pre-auth connections 64; hello within 10 s; frames 1 MiB (16 KiB before auth); `send` text 64 KiB;
replay default 2000, maximum 20000 entries, chunks of 500 entries / 256 KiB; 5 failed logins per
address per 5 minutes; pairing code 8 characters, single use; ping every 20 s, pong within 60 s;
`squatter_max_clients`, `squatter_max_sessions`, `squatter_scrollback` as above.

## Code layout

```
internal/squatter/            the gtf-specific daemon (config, listener, sessions, backend, TFSP subset, handoff)
internal/squatter/{wire,auth,tlsutil,proto,scrollback}
                              COPIES of the reference packages from github.com/itsnotyoutoday/squatClientDaemon
                              pkg/ (same author, GPL-3.0-or-later, SPDX headers kept); import paths rewritten.
                              scrollback has one addition, Store.Seed, for state handoff.
internal/client/squatter.go   the glue: variables, squatter.Host, taps, /squatter, /pair
internal/client/squatter_test.go, internal/squatter/*_test.go
```

Decision: the five small, self-contained packages are **copied** rather than depending on the
whole reference module (which carries a TinyFugue bridge, Lua, a TLS fork and more gtf does not need).
To resync, copy the files again and re-apply the import rewrite and `Seed`. `internal/squatter`
imports no gtf package and no engine (enforced by `internal/archtest`); only `internal/client` imports
it. The squatter reaches gtf through the `squatter.Host` interface (`Do`/`Post` plus the operations
it needs); `internal/ui.Screen.Tap` is the one hook in the UI that mirrors every line added to a world's
screen.

## Surviving /restart

`/restart` (docs/RESTART.md, "The squatter") carries the squatter: `internal/client/restart_squatter.go` registers the carrier
`squatter`, with these pieces from this package:

* `Export() ([]byte, error)` / `DecodeState` / `ImportState(st, ring)`: the sessions (ids, metadata, scrollback entries with
  their sequence numbers, acks, GMCP/MSDP snapshots, remote-echo flag, ended sessions, NAWS widths, the id counter). The format is
  versioned (`ErrStateTooNew`, `ErrStateCorrupt`, optional additions never bump the version). The new process calls `Activate()`
  and `ImportState` before the first `Apply`, so nothing is seeded. Paired devices and the certificate are files in
  `squatter_dir` and are reused: same fingerprint, no re-pairing. A client that reconnects resumes with its last sequence number.
* `PlanConns`, `QuiesceConns`, `ExportConns`, `ResumeConns`, `PrepareConns`, `AdoptConns` (`handoff_conns.go`): the listening
  socket and the connected clients, TLS ones included. `wire.Server.Quiesce/Export/Adopt/Resume` stop a client's reader and writer
  at a clean point and hand over its unread bytes, half-received message, undispatched message and unwritten frames with its
  identity; the server side of WebSocket is `wire/wsconn.go` (a small RFC 6455 implementation, because a library's read-ahead
  buffers cannot be exported). TLS comes from `Options.TLS` (`TLSMigrator`, implemented in `internal/client/squatter_tls.go` with
  `internal/net/tlsmigrate`; this package imports no gtf package). A client that cannot be carried reconnects and resumes.
* `ReloadNotice()` sends `server.restarting` to clients that are not carried.

What is carried per client: id (so the extension's `cid` stays valid), device, role, hello extensions, the sessions it follows,
the gtf-extension state (features, view, attached sessions, widths, last input). Not carried: typing indicators and rate-limit
buckets (they start fresh). World revisions are the engine's and larger after a restart than before it: a peer's old `baseRev`
is merely stale, never wrongly accepted.


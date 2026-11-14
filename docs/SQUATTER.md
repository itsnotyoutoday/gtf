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

In SquatClient add a squatter of kind "TinyFugue" with host `localhost` (or the machine's name),
port `10328`, enter the pairing code and check that the fingerprint SquatClient shows is the one gtf
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
| `/squatter kick NAME` | disconnect a client by device name or id prefix (it may reconnect) |
| `/squatter unpair NAME` | forget a paired device (name or id prefix of at least 4 characters) and disconnect it |
| `/squatter fingerprint` | print the TLS fingerprint |
| `/squatter port N`, `bind ADDR`, `name NAME` | set `%squatter_port`, `%squatter_bind`, `%squatter_name` |
| `/pair [viewer\|operator\|admin]` | a one-time pairing code, printed like `squatterd pair`:<br>`Pairing code:    ABCD-EFGH   (role: admin)`<br>`Valid until:     <time> (10m0s, single use)`<br>`TLS fingerprint: AA:BB:...`<br>`Connect to:      wss://host:10328/tfsp` |

`/pair` prints the code only to the person who ran it: it is not added to history or the log
(`/recall`, `/log`) and, when an admin runs it from SquatClient, it comes back in the `tf.exec` reply and
never enters a session's scrollback. Codes are stored in `pairing.json` (0600) until used or expired, as in the reference daemon.

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
| **session.connect** | `world.ref` = a world defined in gtf (autologin applies, as `/connect`), or `host`+`port`(+`tls`) for an ad-hoc connection. Always opened **in the background** (`/connect -b`) |
| **disconnect / dismiss** | `/dc` equivalent; dismiss only for ended sessions |
| **GMCP / MSDP** | received packages become `gmcp` / `msdp` entries and are kept as the latest-value snapshot (`state`, sent after every attach and on `state.get`); `gmcp.send` / `msdp.send` go through the connection's negotiated protocol (an error if the game never negotiated it). gtf's own scripts see the same data (`gmcp_get()`, `/def -hGMCP`) |
| **control.\*** | gtf has one model: the terminal and every remote operator type at the same time (`input: both`) and gtf's own triggers always run (`automation: tf`). `control.get` reports that, `control.set` answers with the real state and changes nothing (no error) |
| **world.list** | gtf's world database (never passwords: `passSet` only, never macro files) |
| **tf.exec** | admin only, `squatter_commands=on`: one line starting with `/`, run on the engine goroutine with its output captured and returned (`ok=false` when the command reported an error or the command is unknown); nothing is displayed locally or recorded |
| **resize / caps** | ignored: the game keeps being told the size and capabilities of gtf's own terminal |
| roles | `viewer` (read only), `operator` (send, connect, disconnect, dismiss, GMCP/MSDP), `admin` (and legacy full-trust `owner`: commands and `tf.exec`) |
| features announced | `tf control worlds state gmcp msdp noEcho` and `tfexec` when commands are on |

### Foreground model

A remote client never changes gtf's foreground world. A remote `session.connect` opens in the
background, a remote command runs with the target session's world as its context (like a trigger in a
background world) and prints on that world's screen. The local user sees remote activity as ordinary
text in the corresponding world (and the usual background-activity alerts); the remote client sees what
the local user types and the replies. A script a remote admin runs can of course call `/fg` itself.

### Not implemented (answered with an error or ignored)

`world.put`, `world.delete`, `world.rename` (use `tf.exec` with `/addworld`, `/unworld`), `tf.script.*`
(use `tf.exec` with `/load`), takeover/ask control, `reload.status` / `admin.reload`, named users and
ssh-key logins (`user.*`, `admin.*`: the reference code is present and answers them, but gtf only creates
paired devices, so they have no accounts to work on), disk logs of sessions, resizing the game's
window from a peer. Not tested against a real SquatClient (none available here): compatibility rests on
using the reference wire code and its own client library in the tests.

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

## Surviving /restart (not wired yet)

`/restart` (branch `hot-restart`, separate work) restarts gtf without dropping connections. The squatter
is built so the listener and the peers' state can follow, with no dependency on that code:

* `Export() ([]byte, error)` serialises the sessions (ids, metadata, scrollback entries with their seq
  numbers, acks, GMCP/MSDP snapshots); `Import(blob) ([]string, error)` restores them in the new
  process (call `Activate()` first, before the first `Apply`).
* `ListenerFile() (*os.File, error)` is a duplicate of the listening socket to inherit;
  `AdoptListener(net.Listener)` makes the next start use the inherited socket instead of binding.
* `ReloadNotice()` sends `server.restarting` to every client so they reconnect and replay from their
  last seq at once. Peers themselves are not carried (a TLS WebSocket cannot move between processes
  here); with the preserved seq numbers a reconnect is seamless.

When the `RegisterRestartSection(name, carrier)` registry exists on the restart branch (it does in
that worktree's working tree at the time of writing), the integration is one carrier in
`internal/client`, section `"squatter"`, `Required()` false:

```
RestartQuiesce  sq.ReloadNotice()
RestartSave     blob := sq.Export(); slot := w.AddFile("listener", sq.ListenerFile());
                data = {blob, slot, map[world-name]squatID of the carried sessions}
RestartRestore  sq.Activate(); sq.AdoptListener(net.FileListener(ctx.File(slot)));
                sq.Import(blob); for each restored Session set s.squatID from the map;
                then c.squatCheck() (reads the variables, adopts the listener, starts serving)
RestartRollback nothing (the listener was only duplicated)
```

The sessions section must have rebuilt the `Session` objects before the squatter section restores
(registration order), so the ids can be re-attached. Tested here: Export/ListenerFile/Import/AdoptListener
across two squatter instances in one process (`TestStateExportImport`), not an actual exec.

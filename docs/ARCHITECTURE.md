# GoTinyFugue (gtf) architecture

GoTinyFugue (binary `gtf`) is a terminal MU* client that is compatible with TinyFugue (tf, by Ken Keys); it is its own program, not TinyFugue. It is
**only the client**: the TinyFugue-compatible scripting language and its state live in the shared
engine module [`github.com/itsnotyoutoday/gtfparser`](https://github.com/itsnotyoutoday/gtfparser),
which this repository consumes as an ordinary pinned dependency (the same
engine that backs SquatClient). The client attaches to the engine only through
its public extension points.

## Directory layout (enforced)

```
cmd/gtf/             main only: flags -> client.Main. No logic.
cmd/vtdump/          developer tool: render a captured byte stream through internal/ui/vt
cmd/helpwiki/        developer tool: build the GitHub wiki pages from the help topics (writes files only)
internal/client/     application wiring: sessions (one per world connection), event routing
                     net<->interpreter<->ui, implements the engine's host interfaces
internal/client/data per-session store of GMCP/MSDP/MSSP data (gmcp:Char.Vitals.hp ...)
internal/net/        transports (tcp, tls) and Conn, which yields typed events; no knowledge of
                     the interpreter or the UI
internal/net/telnet  the telnet state machine (RFC 1143 Q method) + the plugin registry
internal/net/proto/  one package per protocol extension, each self-registering:
                     ttype (TTYPE/MTTS), naws, charset, eor, sga (SGA+ECHO), mccp2, gmcp, msdp, mssp
internal/net/tlsmigrate vendored crypto/tls that can export/import a live TLS session (for /restart; see RESTART.md)
internal/handoff     the versioned state file /restart leaves for the next process (envelope only)
internal/net/nettest fake-server harness: scripts telnet negotiations byte for byte
internal/ui/         terminal UI: raw tty, compositor, scrollback, wrapping, more-pager, input
                     window, status line, panels
internal/ui/vt       small VT100/ANSI screen model used by tests (and by scripts/compare_tf.py)
internal/update/     self-update: GitHub releases lookup, checksums, atomic replacement of the binary and
                     library directory, install record (used by /update and gtf --update)
internal/squatter/   squatter daemon + tf bridge built into gtf (SquatClient pairs with it); one gtf-specific
                     package plus copies of the reference wire/auth/tlsutil/proto/scrollback packages
                     as subdirectories; imports nothing from gtf; only internal/client imports it
                     (docs/SQUATTER.md)
internal/cmcpui/     CMCP cell renderer: a bound screen tree (cmcp core `pack`) -> a grid of truecolor cells, keyboard
                     operation of the widgets, themes (plain + empire accents); no protocol logic (docs/CMCP.md)
internal/layout/     panel/layout system: templates, bindings, widgets, layout engine -> cell grids
help/                gtf's own documentation: one Markdown file per topic (topics/), the plain-text renderer behind /help and the wiki generator; see help/README.md
gtflib/              embedded TF library: the unmodified real tf library (gtflib/tf/) and gtf-only scripts (gtflib/gtf/layout.tf, gtflib/gtf/layouts/*.layout)
docs/                this file, PROTOCOLS.md, LAYOUTS.md, COMMAND-STATUS.md
scripts/             compare_tf.py (screen diff vs real tf), e2e_evennia.py, env.sh
```

TinyFugue's unmodified standard library and manual (`lib/tf/*.tf`: stdlib.tf, kbbind.tf, tfstatus.tf,
tf-help, ..., copyright their authors, notices intact) are embedded in `gtflib/tf/` (the scripting engine does not contain it; the
engine's tests compare against a real tf build instead); `gtflib/gtf/` holds only what is new
in gtf and is loaded on top of it.

### Allowed imports

```
cmd/gtf          -> internal/client, gtflib
internal/client  -> internal/ui, internal/net(/...), internal/layout, internal/cmcpui, internal/update, internal/handoff, help, gtflib, the engine module
internal/handoff -> nothing internal
internal/client  -> internal/ui, internal/net(/...), internal/layout, internal/squatter(/...), help, gtflib, the engine module
internal/squatter -> itself only (no gtf package, no engine)
internal/ui      -> internal/layout
internal/net     -> internal/net/... only
internal/layout  -> nothing internal
internal/cmcpui  -> internal/layout
internal/update  -> nothing internal
help             -> nothing internal
cmd/helpwiki     -> help
gtflib           -> nothing internal
```

Only `internal/client` may import the engine; the engine never imports anything
from gtf. `internal/archtest` parses every package's imports and fails the build
on a violation, on a package directory that is not in this table, and on a
`cmd/gtf/main.go` that grows logic.

## Runtime model

Everything runs on the engine `Runner`'s goroutine (an actor). Socket readers,
the keyboard reader, SIGWINCH, timers and the render debounce only `Post` closures
to it, so the UI, the sessions and the interpreter need no locks.

```
tty bytes ──► client.onKeys ──► in.KeyPressed (engine: bindings, kbbind.tf, /dokey, input buffer)
net.Conn events ──► Session.feed ──► in.ProcessText (triggers, gags, attributes) ──► ui.Screen
interpreter output ──► Client.Echo / EchoAttr / HookMessage ──► ui.Screen
state changes ──► Notifier events / syncUI ──► ui repaint (coalesced, diffed against the shadow screen)
```

The UI composes the whole screen as a cell grid (output window bottom-aligned
above the status line, status row from `in.StatusRows`, an `isize`-row input
window that keeps entered lines scrolled up like tf, docked panels) and writes
only the cells that changed. With no panel defined the result is
cell-for-cell what tf draws (`scripts/compare_tf.py`).

### What the engine owns, what the client owns

| engine (gtfparser)                                   | client (this repo)                                      |
|------------------------------------------------------|---------------------------------------------------------|
| input buffer, key bindings, `/dokey`, `kb*()`        | feeding bytes (`KeyPressed`), drawing the input window  |
| status fields, `/status_*`, `StatusRows`             | drawing the rows, attributes -> SGR                     |
| world database, `/connect /dc /fg /listsockets`      | `WorldHost/ConnectHost/FgHost/SocketHost`: real sockets |
| history, `/recall`, `/limit`, `/log`                 | feeding lines (`RecordWorldLine`), `LimitHost` redraw   |
| triggers, hooks, attributes, hook messages           | `ProcessText`, `FireHookArgs`, `HookDisplayHost`        |
| paging counters/functions                            | the pager itself (`MoreHost`, `Screen`)                 |
| `/help` (asks `HelpHost`)                            | reading `tf-help`                                        |
| (nothing)                                            | telnet, MCCP, GMCP/MSDP/MSSP, panels, layouts           |

## Connections (tf's socket life cycle)

`internal/client/session.go`, `dial.go`, `alert.go`; compared with real tf by the `conn-*`
scenarios of `scripts/compare_tf.py`.

* A `/connect` made from the foreground context brings the new world to the foreground *first*
  (tf `opensock`), onto the world's own screen, so "Trying to connect..." and a failure are
  shown there. `-f`/`-b` and commands run by a background world's trigger decide otherwise.
* The progress messages are the engine's hook messages (`PENDING`, `ICONFAIL`, `CONFAIL`,
  `CONNECT`, `DISCONNECT`); the client only fires the events with tf's arguments
  (`Dialer.Dial` reports the lookup, each address tried and each failure through `DialTrace`).
  A lookup failure uses tf's own wording (`Unable to connect to W: host port: Unknown host`).
  Like the reference tf (built without IPv6) the IPv4 addresses of a name are tried in turn;
  IPv6 only when there is no IPv4 address.
* A socket whose connection ends (failure, `/dc`, the server closing) becomes a *zombie*: it
  keeps its place in `/listsockets` (`!`), the status line shows `!name`, and its screen stays.
  It is forgotten (`reap`) after it has been in the foreground and the foreground changed.
  A world's screen outlives its socket (reconnecting shows the old text); a temporary
  `(unnamedN)` world and its screen go with the socket.
* Messages about a world are written into that world's screen like server text; when the world
  is in the background they are also raised as a status-line *alert* (`alert.go`: over the field
  after `@world`, `%alert_attr`, `%alert_time`, cleared when the world comes to the foreground).
* `%virtscreen` on (the default): the `---- World X ----` / `---- No world ----` line is
  *not* printed (tf prints it only without virtual screens); a divider (`%textdiv_str`) is
  inserted before the lines that arrived while the screen was in the background.
  `%virtscreen off` (one shared screen) is not implemented; only the switch lines are printed.
* `/connect -q` with auto-login hides the first lines of the server's output (tf `numquiet`).

## gtf-only additions (registered through the public API)

* commands `/panel`, `/layout`, `/canvas`, and the experimental `/sprite` (see LAYOUTS.md);
* functions `gmcp_get gmcp_json gmcp_has msdp_get mssp_get gmcp_send msdp_send`;
  `gmcp()` and `msdp()` keep real tf's meaning (they *send*);
* hooks `GMCP`, `MSDP`, `GMCP_LOGIN` (enabled with `EnableHookEvents`).

## Restarting without dropping connections

`/restart` (`internal/client/restart*.go`, full description in RESTART.md) execs the same binary in place while
the game sockets stay open. The old process quiesces (key reader parked, socket readers paused, in-flight
events drained), writes a 0600 versioned state file (`internal/handoff`: terminal termios, the engine's
`SaveState`, screens with attributes and the pager, sessions with their telnet machine / MCCP2 inflater /
line state / TLS session export, panels), rehearses the restore in a probe child that must report ready, and
only then execs; any failure before the exec rolls everything back. `Client.RestartPlan` and
`Client.Restart(force)` are the Go API (used by `/restart` and by whatever updates the binary);
`RegisterRestartSection` lets other components carry their own descriptors and state.
## Squatter (remote clients)

`squatter=on` makes gtf serve SquatClient from inside the process (docs/SQUATTER.md). The listener and
the wire protocol run on their own goroutines; they never touch the interpreter: the squatter calls the
`squatter.Host` interface, implemented in `internal/client/squatter.go`, whose `Do`/`Post` run closures on
the engine Runner's goroutine. The other direction is the tap: every line added to a world's `ui.Screen`
(`Screen.Tap`, called by `UI.AddLine`) is converted to spans and handed to the squatter, which keeps a
seq-stamped ring per session for replay; connection state changes, prompts, GMCP/MSDP and local sends are
reported the same way (`squatOpen`, `squatConnected`, `squatEnded`, `squatPrompt`, `squatLocalEcho`).
The `squatter_*` variables are compared after every task and on the 250 ms poll (`squatCheck`), so a `/set`
applies at once. A remote `tf.exec` captures output by diverting `addLine` (`c.capture`). Disabled, none of
this does any work beyond that comparison.

The gtf extension to the protocol (docs/SWP-GTF.md) lives in `internal/squatter/ext_gtf.go` (negotiation,
presence, typing, rate limits, author tags), `naws.go` (the shared terminal width policy) and `worlds_x.go`
(world edits with revisions; `ApplyWorldEdit` is pure over a `WorldDB`). The client half is
`internal/client/squatter_gtf.go`: the engine's world database as a `WorldDB`, persistence of the world file
with backups, `nawsCols`, and the local terminal's view and typing reported to the squatter. World changes
reach the squatter through the engine's `world` / `renameworld` Notifier events (a core change, CORE-CHANGES
v0.0.11), so typed `/addworld` and a peer's edit take the same path. Everything the squatter itself prints
goes through `privateLine`, never into a scrollback.

## Merge checklist (things in the client that exist only because of an engine gap)

Each item is marked `TODO(merge)` / `TODO(core)` in the code.

1. `cmds.go`: `limit()`, `nread()`, `nmail()` functions (the stock `@more` status field
   calls `limit()`); engine should provide them (`TODO(core)`).
2. `hosts.go processLine`: `-ag` gag is honoured by reading `Match.Macro.Attrs` because the
   engine's `LineResult.Gag` was not set for `/def -ag` in the version this was written
   against; drop it when the engine sets it.
4. `hosts.go processLine`: BEL in server text rings the bell through `%beep`; use the
   engine's `LineResult.Bell` only (both are honoured today).
5. `client.go`: `localecho` events are accepted without telnet ECHO negotiation (server
   `WILL ECHO` is reported with `SetLocalEcho`, which hides the input line).
   `WILL ECHO` is reported with `SetLocalEcho`, which hides the input line).
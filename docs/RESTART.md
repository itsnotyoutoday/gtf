# /restart: replacing gtf without dropping connections

`/restart` replaces the running gtf with a fresh copy of its own binary (the same executable path, so a
binary replaced on disk by an update is picked up) while every game connection stays open and the screen
looks unchanged. The design follows squatClientDaemon's `docs/RELOAD.md` (state file, version map,
rollback); where gtf differs the reason is given.

```
/restart          restart; refuses if some connection would end
/restart -f       restart anyway: connections that cannot be carried are closed and reconnected
/restart -n       only print what would be carried and what would end
```

Go API (`internal/client`, call on the client goroutine: from a command, or inside `Do`/`Post`):

```go
func (c *Client) RestartPlan() RestartPlan    // Carried / Ends (with reasons), Supported
func (c *Client) Restart(force bool) error    // does not return on success; *RestartRefused when
                                              // sessions would end and force is false; any other error
                                              // means the restart was abandoned and nothing changed
```

`Restart` blocks the client goroutine until the new binary has reported ready (default 10 s;
`GTF_RESTART_TIMEOUT`, e.g. `1.5s` or `2`, overrides it; `Config.RestartTimeout` sets it in code).

## Mechanism

A terminal client cannot do what the daemon does (start the new binary as a child and exit): the shell
waits for its job's pid and takes the terminal back when it exits, which would leave a child without the
terminal. So gtf execs in place (same pid, same terminal, job control and `kill` keep working, like
TinyFugue's own `/restart`) but rehearses first, which keeps the daemon's rollback guarantee:

1. **Quiesce** (old process). The key reader is parked (`ui.TTY.Pause`: a `select(2)` reader that has no key
   in hand while parked, so keys typed during the handover are delivered later, not lost). Every carried
   socket's reader is stopped (`net.Conn.Pause`; for TLS a read deadline, which keeps the session intact),
   the events already read are processed (`Session.drain`, so nothing is in flight), partial-line timers are
   stopped, extra sections are quiesced.
2. **Save**: the state file (0600, in `~/.gtf/restart-<pid>.state`, directory 0700) and one duplicate
   descriptor per carried socket. Exporting is non-destructive.
3. **Rehearse**: the binary is started as a *probe child* (own session, no terminal, descriptor 3 = report pipe,
   4.. = the sockets) with `GTF_RESTART_STATE`, `GTF_RESTART_FDS`, `GTF_RESTART_READY`. It runs the whole
   restore (read and check the file without deleting it, load the interpreter state, rebuild screens and
   sessions, adopt every socket paused and import every TLS session), then releases everything without
   reading or sending a byte and reports `ok`. If it fails to start, exits, closes the pipe, hangs past the
   timeout, or reports an error (state too new, corrupt, a section it cannot restore): it is killed and the
   old process **takes everything back**: readers resume, timers re-arm, carriers are told
   (`RestartRollback`), the state file is deleted, and the reason is printed
   (`% /restart: ...; gtf keeps running`). Nothing on the terminal was touched.
4. **Commit**: sessions that cannot be carried (with `-f`) are closed cleanly (TLS close_notify), the
   descriptors lose close-on-exec, and the binary is exec'd in place with the same arguments.
5. **New process**: `Main` sees `GTF_RESTART_STATE`, reads the file and deletes it at once, opens the tty from the
   saved termios (the terminal is already raw; the saved *original* settings are what quitting restores),
   builds the client from the state (no library, no tfrc, no startup commands), repaints the whole screen,
   resumes the sockets, reconnects what could not be carried.

What cannot be undone: a new process that passed the rehearsal and then fails after the exec (the same binary
and file passed seconds earlier, so this is a crash like any other), and sessions closed at the commit step
when `exec` itself then fails (they are marked dead). If the new process cannot apply the state it prints
why and starts fresh (the connections are lost, the inherited descriptors are closed).

## What survives

| | |
|---|---|
| Terminal | the tty descriptors are inherited (0, 1, 2); raw mode stays on; the original termios travels so `/quit` restores the shell's settings; everything is repainted (full clear and redraw) |
| Screens | every world's scrollback **with attributes** (compact runs), the base screen, the pager (`/more`: paused, how many lines are held, the page counter), unseen-line counts, `/limit` filtering (re-applied), wrap geometry, the "entered lines shown in the input window" list, non-visual mode's printed position |
| Foreground | the foreground world (or none); the status line is recomputed from the restored interpreter state |
| Input line | text, cursor and input history (interpreter state); a held-back key prefix (a lone ESC) is flushed to its key before the save |
| Game connections | plain TCP as an inherited descriptor; telnet state (RFC 1143 option states of every plugin, a subnegotiation in progress, a half-read IAC sequence), the MCCP2 decompressor mid-stream, an incomplete UTF-8 sequence, TTYPE cycle position, charset, the window size told to the server (a NAWS update is sent if the window changed meanwhile), the partial line and prompt state (the prompt timer re-arms), `numquiet`, the per-session GMCP/MSDP/MSSP store, timestamps, local-echo/no-echo mode |
| TLS connections | carried, with no new handshake (see below) |
| Dead sockets | zombies keep their place in `/listsockets` and their screen |
| Interpreter | everything gtfparser's `SaveState` carries (see the engine's `docs/state-export.md`): variables, macros with their ids, triggers, hooks, bindings, worlds, histories, `/repeat` `/quote` `/at` processes with remaining times, `/limit`, status line, ... |
| Panels | layout definitions (rebuilt from their `/layout save` script), the active layout and per-world assignments, canvases (name and size) |
| Alert | the status-line alert with its remaining time |

## What does not survive

* **Pixels of canvases**: a canvas exists again with its size but is empty until the script draws on it.
* **Connections still being established**, and TLS sessions that cannot be exported (below): `RestartPlan` lists
  them with the reason, `/restart` refuses without `-f`, and with `-f` they are closed cleanly and the new
  process connects the world again by itself (a line `% Reconnecting W after /restart (reason).` is put on its
  screen). The game sees a normal disconnect and a new login.
* Anything the interpreter does not carry (engine docs): open pipes, the macro call stack, the random sequence.
* **Library scripts are not re-run.** A new binary's embedded `stdlib.tf` / `layout.tf` do not replace the
  macros the old process loaded; a changed library needs `/load` or a full restart. `%version`,
  `%TFLIBDIR`, `%GTFLIBDIR` are refreshed.
* Keys typed *after* the key reader is parked and before the exec are delivered to the new process (they wait in
  the tty), but a non-pausable key reader (tests, pipes) can lose the few typed in that window.
* The old process's goroutines and memory (of course): `/restart` is also a way to shed them.
* Windows and other platforms without a POSIX terminal: `Restart` returns `not supported on this platform`.

## TLS

TLS sessions are carried, using squatClientDaemon's `tlsmigrate`, copied into `internal/net/tlsmigrate` (a
vendored `crypto/tls` from Go 1.24 with a small patch that exports and imports the record-layer state: traffic
secrets or keys/IVs, sequence numbers, undecrypted and decrypted-but-unread bytes). The copy is the author's
own GPL-3.0-or-later package plus the Go BSD-licensed vendored tree (its LICENSE/NOTICE/PATENTS are kept);
`golang.org/x/crypto` is the one new module dependency. All `net.TLS` transports use it (a `tls.Config` it
cannot express falls back to the standard library: such a session works but cannot be carried).
Carried: TLS 1.3, and TLS 1.2 with an AEAD suite. Not carried: TLS 1.2 with CBC/RC4/3DES, a renegotiated
session, a handshake not finished, a session made by the standard library. Tested: TLS 1.3 and 1.2-GCM through
the real transport, a partial line, data both ways after the handoff, a CBC session refusing to export.
The TLS blob is **key material of a live session**: it exists only inside the state file (0600, refused by the
reader if group/other-readable, deleted on read and on rollback), the old process wipes its copy after
encoding, the new one after importing. A rehearsal drops its adopted sessions without `close_notify`.

## State file format

`~/.gtf/restart-<pid>.state`, JSON (`internal/handoff`):

```
{"magic":"GTFRESTART","format":1,"minReader":1,"writer":"GoTinyFugue version ...","created":<unix>,
 "fds":[{"section":"sessions","item":"<world>"}, ...],       // slot i = i-th inherited descriptor
 "sections":{"<name>":{"v":<n>,"required":<bool>,"data":{...}}, ...},
 "sum":"<sha256 of the sections>"}
```

| section | v | required | content |
|---|---|---|---|
| `terminal` | 1 | yes | original termios (opaque, same OS/arch), size |
| `engine` | 1 | yes | gtfparser `SaveState` document (its own versioned format) |
| `ui` | 1 | yes | base screen, per-world screens (`ScreenState`: lines as text + attribute runs, pager, wrap geometry), `UIState` |
| `sessions` | 1 | yes | per world: state, options, partial line, prompt, ANSI colour state, GMCP/MSDP/MSSP store, `conn` (`kind` tcp/tls, descriptor slot, `HandoffState`: telnet machine state v1, UTF-8 carry, env, TLS blob), `reconnect`+`endReason` |
| `panels` | 1 | no | layouts (as scripts), current/per-world/file assignment, canvases (name, size) |
| `client` | 1 | no | start time, alert |
| *carriers* | their own | their own | see "Extending" |

Rules (as RELOAD.md): a reader always reads older versions of a section it knows; a newer *envelope* is read only
if `minReader` allows; an unknown or newer-than-known section is **skipped if optional and refused if `required`**
(`ErrTooNew`); everything is checked before anything is adopted, so refusing is clean; a corrupt, truncated or
checksum-failing file is refused (`ErrCorrupt`); the descriptor list must match the descriptors inherited. Adding
optional fields never bumps a version. Per connection, `net.HandoffState` and `telnet.MachineState` carry their
own `v`.

**Secrets**: nothing typed in no-echo mode is written: the engine drops the input line (and the editor's current
slot) when the foreground/current world is in no-echo mode, `UIState.Old` records a hidden line as empty,
keys read while parked are not saved, and the state file never holds keystrokes. Tested by typing a password
and searching the file. World passwords from the world database are in the file in clear (as `/saveworld`
writes them): that is another reason for 0600 and deletion.

## Extending: carriers

Another component (the squatter daemon, for instance) registers a section instead of editing the core, in
`restart_sections.go`:

```go
client.RegisterRestartSection("squatter", carrier) // RestartCarrier
type RestartCarrier interface {
    Version() int; Required() bool
    RestartPlan(c *Client) (carried, ends []RestartItem)   // feeds RestartPlan/refusal
    RestartQuiesce(c *Client) error                        // stop accepting/reading
    RestartSave(c *Client, w *RestartWriter) (any, error)  // data + w.AddFile(item, *os.File) -> slot
    RestartRollback(c *Client)                             // resume after an abandoned restart
    RestartRestore(c *Client, ctx *RestoreContext, v int, data json.RawMessage) error // ctx.File(slot)
}
```

There is no fixed descriptor layout: `AddFile` appends to a list, the file records which descriptor belongs to
which section and item (`fds`), and the section stores the slot. The inherited numbers are passed in
`GTF_RESTART_FDS` (probe: 4..; exec: the real numbers). A listener's descriptor and each peer's socket are just
`AddFile` calls (pass duplicates, e.g. `TCPConn.File()`). `RestartRestore` runs twice per restart: in the
rehearsal with `ctx.Probe` set (validate, adopt paused, do not read/accept/bind/send: the old process still owns
everything) and in the new process. A section is optional unless `Required()`; a binary that does not know it
ignores it, and a carrier whose data must not be dropped silently returns `Required() == true` so an older binary
refuses the file and the old process keeps running. Carriers' `RestartPlan` `ends` make `/restart` refuse
without `-f` like sessions do. Tested with a registered carrier (data, a descriptor, plan refusal, rollback
notification, an unknown required section refusing, an unknown optional one skipped).

## Testing

* `internal/handoff`: round trip, 0600/0700, deletion, version rules, corruption (truncated, bit flip, bad
  magic), world-readable refusal.
* `internal/net/telnet`: the stream is cut at **every byte position** (including inside a zlib stream and a
  subnegotiation), snapshot to JSON, restore, compare with the uninterrupted run. `internal/net`: TCP
  mid-line + mid-MCCP2, TTYPE cycle continuity, TLS 1.3 and 1.2-GCM through the real transport, CBC refusal.
* `internal/ui`: screen round trip (attributes, wide characters, pager), the parked key reader loses no key.
* `internal/client/restart*_test.go`: (in-process, the exec replaced by a hook that builds the new client from
  the file and duplicated descriptors on the same virtual terminal) screen before == screen after, foreground,
  input line and cursor, pager, partial line completing, MCCP2 mid-stream, background world, rollback on
  exit/hang/refusal, no-echo secrets, TLS carried / TLS refused then reconnected with `-f`, panels and
  canvases, carriers, unknown required section, too-new file. (real processes, the test binary re-exec'd)
  `TestRestartRealExec`: real `syscall.Exec`, same pid, the same TCP connection continues, two restarts in a
  row, clean quit; `TestRestartRealRollback`: the probe child exits, hangs, exits silently, or meets a
  bit-flipped file, and the old process keeps the connection.
* `scripts/restart_pty.py <gtf>`: the real binary under a pseudo terminal: pid kept, connection kept, terminal raw
  while running and **restored to cooked mode after /quit** (run it after `go build ./cmd/gtf`).
* Engine side: see the engine's `docs/state-export.md` and its `state*_test.go`.

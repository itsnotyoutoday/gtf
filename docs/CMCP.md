# CMCP in gtf (the cell client)

CMCP is the CobraMUSH client protocol (spec: `~/mush/squat/cmcp/spec`): consoles, gauges, radar, a viewscreen and damage-control
screens carried inside GMCP, while the game stays a text game. gtf is the **`cell` renderer class**: a terminal of 24-bit UTF-8 cells.
Protocol logic lives in the shared Go core (`github.com/itsnotyoutoday/cmcp/go`: frame, msg, data, session, pack, fetch);
gtf adds only rendering, keys and platform glue.

A game that does not offer CMCP, or a gtf with `%cmcp off`, is plain text as always. Nothing CMCP is sent before gtf declared it.

## Layout and themes

This is primarily a text game: **the output stays big**. The default theme is `tng` (an LCARS-style frame; `%cmcp_theme plain`
turns the frame off and draws plain text widgets). On a built-in `std.*` screen the theme arranges the screen: a band of pictures
across the top (the viewscreen or schematic, the radar dish on the right), under it the framed **game output** on the left and a
column of data and pill buttons on the right, a switch column (decoration) down the left side, a title band at the top and a status
strip (hull, shields, core, lock, alert) at the bottom. A red alert recolours the frame and the controls. A pack screen is arranged
by the pack's own tree and its `layout.cell` hints. Rules the layout keeps: the output gets at least 45% of the height or 60% of
the width (tested at 80x24, 120x40 and 200x50), never less than 8 rows, rows restack or wrap when they do not fit, long lists
scroll, nothing is drawn on the text pane or the input line.

* **F3** (`%cmcp_key_log`, `/cmcp log`): the log view. The output grows to about 80% of the height and every other panel becomes
  one summary line. Press again to go back; remembered for the session.
* **F4** (`%cmcp_key_hide`, `/cmcp hide`, `/cmcp show`): the screen off. The text view gets the whole area, data keeps streaming,
  the same key brings the screen back. `%cmcp_start_collapsed on` starts with it off.
* `%cmcp_layout side` is an alternative arrangement (output on the whole left, widgets stacked on the right) for packs and built-ins.
* `%cmcp_ascii` (on, off, auto: on when the locale is not UTF-8) draws only ASCII.

Other themes (`klingon`, `romulan`, `ferengi`, `tos`, `soft`, `ops`) currently change only the palette; their frames wait for the
theme-pack format (docs/CMCP-THEMES.md).

## What you see

When a server puts you at a console (`man opera` on the Evennia PoC), the output area becomes the console screen: widgets around
**the real text pane**, which is never covered and keeps at least 4 rows and 30 columns (on a smaller terminal the screen steps aside
and the text pane takes everything). The status line and the input line are untouched. Row 0 of the screen is a header (title, ship,
`[read-only]`, the key hint, short notices).

Screens: the built-in `std.operations`, `std.power`, `std.damage`, `std.sensors`, `std.tactical`, `std.helm` (and `std.transporter`
when the core provides it), or a layout pack's screens (`fspace-klingon/tactical`) after the pack was fetched, verified and
allowed. Until a pack is usable the built-in screen of the same console is shown (spec 16.6).

Widgets (all core widgets): textport, label, bar, bar-list, list, table, gauge, graph, button, toggle, slider, helm-controls (a pack's joystick resolves to it), radar,
schematic (a sprite rasterised into half-block cells; damage zones tinted from `Space.Damage.Stat`, glows from power), viewscreen
(starfield and contacts in view, effects for fire/hit/destroyed), contact-picker, team-assigner, group-picker, and simple versions
of the later-edition widgets (log, sector-map, portrait, pad-diagram). `scope` falls back to `radar`; `lights` is not drawn.

## Keyboard (keyboard first)

The input line always works and is never taken over: typing, Enter, history and completion behave exactly as without CMCP.
The screen has a **focus mode**:

| key | action |
|---|---|
| **Tab** on an empty input line | enter the screen (with text in the line Tab stays tf's completion key); also **F2**, and `/cmcp focus` |
| Tab / Shift-Tab | next / previous panel; past the last (first) one they return to the input line |
| Up / Down | move in a list, table, picker, team assigner, helm controls; at the ends the focus moves on |
| Left / Right | adjust a slider (Left/Down -, Right/Up +), choose a radar contact or schematic zone, change a helm value |
| `+` `-` | sliders (and helm values) |
| Enter, Space | activate: a button's click, the selected row's select, a zone, a team |
| an **underlined letter** | press that button or toggle (first free letter or digit of its text, or the pack's `key` prop) |
| Esc | back to the input line; so does any printable key the screen does not use (it is then typed there) |
| y / n | answer a pack consent prompt |

A control with `hold_ms` (the transporter's energize) takes its key **three times within two seconds** (a terminal has no key
release). Shift-Arrow and Alt-Arrow are never bound (terminals and tmux swallow them). Every action is also a typeable command
and echoes it. Rebinding: `%cmcp_key_focus` sets the key that enters focus mode (`^I` default, `F2`, `^[OQ`, any control letter
`^X`); or leave it and use the engine's own key bindings: `/bind ^[OS = /cmcp focus`. **Mouse: gtf has none** (the spec makes it
optional; `Hello.input.mouse` is never claimed by gtf).

The helm has **no joystick** in gtf: the pack's `joystick` resolves to the core widget **helm-controls** (heading with step,
speed, warp, engage, stop, course to the selected contact, dock, undock), each of which sends the ordinary command.

A click is a command: pressing a button sends the verb to the server exactly as the typed text would, and gtf echoes the equivalent
text as a client-generated line (`cmcp> dmg assign 3=Port_Nacelle`). A refused command shows the server's hint (and the
`hint_cmd` to try) in the output. Buttons whose verb the console may not send are dimmed; in a read-only console (the ship is on
AI, someone else took the console) nothing is offered.

## Commands

```
/cmcp [status]            handshake state, server, ship, console, screen, subscriptions, command counts
/cmcp packs               layout packs for this world, their state, the cache
/cmcp pack allow|deny [id] answer a consent prompt
/cmcp screen [id]         say which screen is shown; with an id (std.damage, pack/screen) show it locally
/cmcp send <verb> k=v ... send a catalogue verb like a button would (validated, echoed)
/cmcp focus               toggle screen keys (F2)
/cmcp on|off              set %cmcp (new connections); off also ends CMCP on the foreground connection
```

## Settings (ordinary variables)

| variable | default | meaning |
|---|---|---|
| `%cmcp` | on | declare `Cobra 1` to servers |
| `%cmcp_key_focus` | `^I` (Tab) | the key that enters the screen (see Keyboard) |
| `%cmcp_layout` | auto | `side` stacks the widgets in a right-hand column |
| `%cmcp_key_log`, `%cmcp_key_hide` | F3, F4 | the log view and the screen-off keys |
| `%cmcp_start_collapsed` | off | start with the screen off |
| `%cmcp_ascii` | auto | ASCII-only drawing |
| `%cmcp_screens` | on | draw console screens (off: protocol and data only, for scripts) |
| `%cmcp_theme` | (plain) | `plain`, `tng`, `klingon`, `romulan`, `ferengi`: palette tokens; plain leaves colours to the terminal |
| `%cmcp_ship_theme` | off | when on and `%cmcp_theme` is unset, follow the console's/ship's/screen's suggested theme (spec 6.6) |
| `%cmcp_bell` | off | ring the terminal bell for alerts (`red_alert`, `yellow_alert`, `proximity_warning`, `comm_beep`, `hail`) |
| `%cmcp_era` | tng | sound era; stored, unused (gtf plays no sound) |
| `%cmcp_sounds_dir` | | local sound folder; stored, unused, never sent anywhere |
| `%cmcp_pack_consent` | ask | `ask` once per pack, `always`, or `never` (built-in screens only) |
| `%cmcp_origins` | | extra allowed pack origins (`https://host[:port]`, space separated); the game host is always allowed |
| `%cmcp_debug` | | non-empty: show protocol diagnostics as `% cmcp:` lines |

gtf plays **no audio**. It does not claim to in `Hello.audio`, so a server sends it no `Sound.*`. **Deliberate deviation:** with
`%cmcp_bell on` the bell also rings for the alert `Cobra.Event`s (red/yellow alert, proximity, comm, hail), because a client without
audio would otherwise never hear about them; spec 9.9 only names `Sound.Play`. `Sound.Play` of the bell events rings it too.

## Packs, cache, consent

`Cobra.Welcome`/`Cobra.Console` announce a pack; the shared `fetch` package gets it (inline chunks, or HTTPS from the announced
origin only, which must be the game host or one in `%cmcp_origins`), checks the SHA-256, parses it whole-or-nothing and caches it
under `~/.gtf/cmcp/packs` by (game host, hash). The **first use of a pack asks** (a line in the output, a notice in the screen
header; answer with `/cmcp pack allow|deny` or y/n in screen-key mode); the answer is remembered per (host, pack id) in
`~/.gtf/cmcp/consent.json`, as are trust-on-first-use signature keys. A pack with an invalid signature is rejected, never offered.
Any failure keeps the built-in screens and the session running.

## Architecture

* `internal/client/cmcp.go`: one `session.Client` per connection, fed from the gmcp handler (`Cobra 1` is added to
  `Core.Supports.Set` by the handler when the connection's environment says so), output written back through the handler,
  notes turned into output lines; `cmcp_cmds.go` the `/cmcp` command and settings; `cmcpscreen.go` the screen and keys;
  `cmcppacks.go` the pack flow.
* `internal/cmcpui`: bound tree in, cells out (`Draw`), widget key handling (`Frame.HandleKey`), themes. Knows no protocol.
* `internal/ui`: a `Surface` (a drawn grid plus the rectangle of the text pane) replaces docked panels while a screen is shown.
* The expression language, binding, actions and pack validation are the core's (`pack.Bind`, `Node.Perform`); gtf never evaluates an
  expression itself.

## /restart

After `/restart` the new process has no CMCP state, so it declares afresh on every carried connection (`Core.Supports.Remove` then
`Set`, which a server treats as a new offer) and the server answers with a full handshake, the console and snapshots.

## Squatter peers (what workstream W needs)

Not done here: the local direct connection is the one wired. For a squatter that holds the connection and serves gtf as a peer the
SWP `cmcp` extension must (a) deliver game GMCP frames to the peer unchanged tagged with the connection id, (b) accept frames from
the peer, with `Cobra.Cmd` ids rewritten by `session.IDMap`, (c) carry the peer's capability announcement so the squatter builds the
combined `Cobra.Hello`, (d) replay cached `Welcome`, `Cobra.Console`/`Cobra.Context` and full snapshots to a late peer. gtf-side, the
`session.Client` is created per connection from a frame source; a peer would create it from the SWP stream instead of from the
telnet handler (`cmcpOption`/`cmcpReceive`/`cmcpPump` are the three seams).

## Tests

`internal/client/cmcp_test.go` (handshake through the core's `session.Server`), `cmcp_screen_test.go` (every built-in screen on the
spec's example data as goldens in `testdata/golden`, keyboard operation, read-only, tiny terminal), `cmcppacks_test.go` (consent,
inline delivery, cache, refusal, hostile packs). `go test ./internal/client -run Cmcp -args -update` rewrites the goldens.
To look at a screen: run with `GTF_DUMP_DIR=dir` and render with `go run ./cmd/vtdump -w 120 -h 40 -png out.png dir/damage.bytes`.

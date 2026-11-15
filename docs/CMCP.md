# CMCP in gtf (the cell client)

CMCP is the CobraMUSH client protocol (spec: `~/mush/squat/cmcp/spec`): consoles, gauges, radar, a viewscreen and damage-control
screens carried inside GMCP, while the game stays a text game. gtf is the **`cell` renderer class**: a terminal of 24-bit UTF-8 cells.
Protocol logic lives in the shared Go core (`github.com/itsnotyoutoday/cmcp/go`: frame, msg, data, session, pack, fetch);
gtf adds only rendering, keys and platform glue.

A game that does not offer CMCP, or a gtf with `%cmcp off`, is plain text as always. Nothing CMCP is sent before gtf declared it.

## What you see

When a server puts you at a console (`man opera` on the Evennia PoC), the output area becomes the console screen: widgets around
**the real text pane**, which is never covered and keeps at least 4 rows and 30 columns (on a smaller terminal the screen steps aside
and the text pane takes everything). The status line and the input line are untouched. Row 0 of the screen is a header (title, ship,
`[read-only]`, the key hint, short notices).

Screens: the built-in `std.operations`, `std.power`, `std.damage`, `std.sensors`, `std.tactical`, `std.helm` (and `std.transporter`
when the core provides it), or a layout pack's screens (`fspace-klingon/tactical`) after the pack was fetched, verified and
allowed. Until a pack is usable the built-in screen of the same console is shown (spec 16.6).

Widgets (all core widgets): textport, label, bar, bar-list, list, table, gauge, graph, button, toggle, slider, joystick, radar,
schematic (a sprite rasterised into half-block cells; damage zones tinted from `Space.Damage.Stat`, glows from power), viewscreen
(starfield and contacts in view, effects for fire/hit/destroyed), contact-picker, team-assigner, group-picker, and simple versions
of the later-edition widgets (log, sector-map, portrait, pad-diagram). `scope` falls back to `radar`; `lights` is not drawn.

## Keyboard

The input line always works. **F2** toggles *screen keys* (also `/cmcp focus`); while they are on and the input line is **empty**:

| key | action |
|---|---|
| Tab / Shift-Tab | next / previous widget |
| Up / Down | move in a list, table, picker; at the ends the focus moves on |
| Left / Right | slider, radar contact, schematic zone; otherwise previous/next widget |
| Enter, Space | activate a button, toggle, row, zone, team |
| `+` `-`, arrows, `0` | joystick throttle, steering, centre (helm) |
| Esc | leave a sub-step (team assigner), then leave screen keys |
| y / n | answer a pack consent prompt |

Any other key, and every key while the input line has text, goes to the input line. A control with `hold_ms` (the transporter's
energize) is pressed twice, because a terminal reports no key release. Mouse: gtf has no mouse support; the screen is keyboard-only.

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
| `%cmcp_screens` | on | draw console screens (off: protocol and data only, for scripts) |
| `%cmcp_theme` | (plain) | `plain`, `tng`, `klingon`, `romulan`, `ferengi`: palette tokens; plain leaves colours to the terminal |
| `%cmcp_ship_theme` | off | when on and `%cmcp_theme` is unset, follow the console's/ship's/screen's suggested theme (spec 6.6) |
| `%cmcp_bell` | off | ring the terminal bell for alerts (`red_alert`, `yellow_alert`, `proximity_warning`, `comm_beep`, `hail`) |
| `%cmcp_era` | tng | sound era; stored, unused (gtf plays no sound) |
| `%cmcp_sounds_dir` | | local sound folder; stored, unused, never sent anywhere |
| `%cmcp_pack_consent` | ask | `ask` once per pack, `always`, or `never` (built-in screens only) |
| `%cmcp_origins` | | extra allowed pack origins (`https://host[:port]`, space separated); the game host is always allowed |
| `%cmcp_debug` | | non-empty: show protocol diagnostics as `% cmcp:` lines |

gtf plays **no audio**. It does not claim to in `Hello.audio`, so a server sends it no `Sound.*`; the bell rings for the
alert `Cobra.Event`s (and for `Sound.Play` of the bell events, should a server send them).

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

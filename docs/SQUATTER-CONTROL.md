# Remote control modes (`squatter_control`)

**Status: design. Only `shared`, the mode gtf has always had, is implemented. The rest is specified here so
the code, `/help squatter-control` and SquatClient agree before anything is built.**

When a remote client (SquatClient) is attached, two things need a policy: who may *type* into the game, and how
control changes hands. gtf's default stays what it is today: you and every attached remote type at the same time
and gtf runs the triggers (`shared`). `squatter_control` is a policy on top of that.

```
/set squatter_control=shared        the default, today's behaviour
/squatter control ask               short form, same variable
```

## Modes

| value | who may type | control changes hands |
|---|---|---|
| `shared` | gtf's terminal and every remote (role permitting) | never needs to |
| `ask` | gtf's terminal; a remote only after you allow it | a remote asks, you answer `/allow` or `/deny` on your terminal |
| `polite` | gtf's terminal; a remote only while your terminal has been idle | automatic; you get priority back the moment you type |
| `takeover` | a remote; your terminal's lines to the game are locked while a remote holds control | `/takeback`, the remote detaching, or the lapse timer |
| `watch` | nobody remote: remotes only watch | no handover; change the mode to change this |

The mode is one setting for the whole squatter, not per session or per device. It sits on top of the device's
role (`viewer`, `operator`, `admin`): the **stricter of the two wins**, so a viewer never types in any mode, and
`watch` turns every device into a viewer. Triggers stay with gtf in every mode (handing triggers to SquatClient
is a separate, larger feature and is not part of this).

### What counts as "typing"

Typing is anything a remote does that sends text to a game or runs a command: `send`, `tf.exec` and the lines of
an `/command` a remote admin runs. Watching is attaching, replaying scrollback, seeing presence, and resizing the
client's own window. World edits (`world.put` and friends) are gated by role as before; only `watch` also blocks
them.

On your terminal, "typing" is submitting a line that goes to a game. Local **commands** are never locked: in
`takeover` you can still run `/takeback`, `/squatter`, `/dc` and `/quit`, so a lock can never trap you.

## Settings

| variable | default | meaning |
|---|---|---|
| `squatter_control` | `shared` | the mode above |
| `squatter_control_polite` | `20` | `polite`: seconds your terminal must be idle (no key, no submitted line) before a remote may type. 3 to 3600 |
| `squatter_control_timeout` | `30` | `ask`: seconds a request waits for your answer; no answer counts as `/deny`. 5 to 600 |
| `squatter_control_lapse` | `0` | `takeover`: seconds of remote silence after which your terminal is unlocked again; `0` = never (until `/takeback` or the remote detaches) |

They are ordinary variables, re-read like the other `squatter_*` ones, and **a change applies at once without
disconnecting anyone** (like `squatter_local`). Changing the mode ends every current grant and lock.

## Commands

| | |
|---|---|
| `/squatter control` | show the mode, its settings, and who currently holds what |
| `/squatter control MODE` | set `squatter_control` |
| `/allow [DEVICE]` | `ask`: grant the waiting request (the only one waiting, or the named device) |
| `/deny [DEVICE]` | `ask`: refuse it (the device may ask again) |
| `/release [DEVICE]` | take a grant away from one device, or from all |
| `/takeback` | `takeover`: unlock your terminal at once and end the remote's hold |
| `/squatter trust DEVICE` | `ask`: never prompt for this device (see below) |
| `/squatter untrust DEVICE` | remove that |

`DEVICE` matches like `/squatter kick`: the whole rest of the line as a device name, or an id prefix of 4+ characters.

## Behaviour in detail

### `ask`
1. A remote that is not yet granted tries to type. gtf refuses the line (`control_denied`, reason `ask`) and shows a
   notice on your terminal, once per request:
   `% squatter: "Pixel 8" wants to type. /allow or /deny (30s)`
2. `/allow` grants that device. A grant lasts until the device detaches, you `/release` it, the mode changes, or
   the squatter restarts. While granted, the device types exactly as in `shared`.
3. `/deny`, or no answer within `squatter_control_timeout`, refuses it; the device may ask again. A device that is
   denied or times out three times in ten minutes is not shown to you again for ten minutes (a remote must not be
   able to flood your terminal with prompts).
4. A **trusted** device (`/squatter trust`) is granted automatically when it attaches. Trust is a flag stored with
   the device record next to its role (`devices.json`), so it survives restarts, and it is shown in
   `/squatter list`. It never raises the role.
5. Your own terminal is never gated in `ask`.

### `polite`
A remote line is accepted only if your terminal has been idle for `squatter_control_polite` seconds. Otherwise it
is refused (`control_denied`, reason `busy`, `retryAfter` = seconds left) and nothing is sent. Local activity is any
keypress in the input line or a submitted line, including command lines. Your own typing is never gated and always
wins: the moment you type, remotes are `busy` again until the idle time has passed. No prompts and no grants.

### `takeover`
A remote **holds** control from its first accepted line (role `operator` or `admin`; a viewer cannot hold). While
any remote holds control, the lines you submit that would go to a game are refused with
`% squatter: "Pixel 8" has control: /takeback to take it back`. Your local commands still run. The hold ends when
the holder detaches, when you `/takeback` (the remote is told: reason `takeback`), when the mode changes, or after
`squatter_control_lapse` seconds without input from the remote (if set). Two remotes may both hold; the lock
stays until the last hold ends.

### `watch`
Every remote input of the "typing" kind above, `world.*` edits, `gtf.*` writes and `tf.script.*` are refused
(`control_denied`, reason `watch`). Attach, replay, presence and the remote's own resize still work.

## What SquatClient shows

The bottom bar line for a gtf connection stops being a fixed sentence and follows the mode the squatter reports:

| mode | text | control |
|---|---|---|
| `shared` | gtf: you and gtf both type, gtf runs the triggers | none |
| `ask` | gtf: ask first | **Request control** while not granted; "Control granted" afterwards |
| `polite` | gtf: types when its terminal is idle | the reason when refused: "gtf is busy, try in N s" |
| `takeover` | gtf: you have control / gtf has control | **Release** (to hand it back) |
| `watch` | gtf: watch only | none |

Typing that is refused keeps the text in the input line and shows the reason; nothing is lost.

## Wire protocol (additions to docs/SWP-GTF.md, negotiated with the extension)

The tf-squatterd `control.get` / `control.set` / `control.state` messages keep their meaning for plain peers:
`control.state` still reports `input: both`, `automation: tf`, and `control.set` still changes nothing. A peer that
negotiated the gtf extension additionally gets, inside `shared` of `control.state`:

```json
{"mode":"ask","canType":false,"reason":"ask","retryAfter":0,"holder":"","timeout":30}
```

`reason` is `""` (may type), `ask` (waiting for approval / not granted), `busy` (polite), `takeover` (another
device holds control, or gtf's terminal does not apply to this device: see `holder`), or `watch`.
`control.state` is pushed whenever it changes for that device.

New requests (extension peers only; others get `bad_request`):

* `control.request` `{ "session": "...", "reqId": "..." }`: ask for control (`ask`: raises the prompt on gtf's terminal; `takeover`: takes
  the hold; other modes: answered `ok` with the current state). Answer `{ "ok": true, "canType": false }`.
* `control.release` `{ "reqId": "..." }`: give control back (a grant or a hold).

A refused `send` / `tf.exec` is answered `{ "ok": false, "error": "control_denied", "reason": "...", "retryAfter": N }`. This is a new error code;
`docs/SWP-GTF.md` lists it, and a client that does not know it shows the `reason` text.

## Interaction with other settings

* `squatter_commands=off`, roles and `/restrict` still apply on top; the control mode never grants more.
* `squatter_naws=active` follows whoever last **sent input that was accepted**: a refused line does not move the width.
* The hooks `SQUAT_CONNECT` / `SQUAT_DISCONNECT` are unchanged; new hooks `SQUAT_CONTROL` (`device-name granted|denied|released|took|lost`) fire on each change,
  so a script can react (for example a bell on a request).

## Decisions (and what was left out)

* **One global mode**, not per device: a per-device policy is what the role is for.
* **`block` is not a mode.** "No remote at all" is `squatter=off`; read-only is `watch`.
* **Local commands are never locked**, only lines to a game, so no mode can lock you out of your own client.
* **Triggers stay in gtf.** A later feature may let SquatClient run them; it would be its own setting
  (`squatter_automation`), not a control mode.
* **Prompt flooding** is bounded (ask, point 3) and the prompts go to your terminal only, never into a session's scrollback.

## Implementation notes

* Gate in one place: `session.go` (`send`, `tf.exec`) and `worlds_x.go` ask `Squatter.mayType(peer)` which returns
  `(ok, reason, retryAfter)`; everything above is that function plus the timers.
* Local side: `Client` reports submitted lines and keypresses (`localActivity()`) and asks `Squatter.localMayType()` before
  sending a line to a game; commands bypass it.
* State that survives a restart: only `trusted` (in `devices.json`). Grants, holds and prompts are in memory.
* Tests: a table of mode × role × local idle/active × grant state → (ok, reason), plus end-to-end tests for each
  mode with a real peer (ask grant/deny/timeout, polite idle boundary with a fake clock, takeover lock and
  `/takeback`, watch refusing everything, changing the mode without dropping peers).

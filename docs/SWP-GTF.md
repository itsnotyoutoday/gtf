# SWP-GTF: the gtf extension to SWP / TFSP (version 1)

Shared control between gtf (GoTinyFugue, the squatter built into it) and SquatClient, negotiated on top of
the protocol both already speak (SWP v1 and its tf-squatterd dialect TFSP, endpoint `/tfsp`; see the
reference docs PROTOCOL.md, TF-SQUATTER.md in squatClientDaemon). Nothing here changes a byte for a
peer that does not ask for it. Implementation: `internal/squatter/ext_gtf.go`, `naws.go`, `worlds_x.go`;
behaviour for owners: docs/SQUATTER.md.

Key words MUST/SHOULD are meant literally for implementers of the peer side.

## 1. Model

gtf has one model, not takeover/ask. Shared by every attached peer and by gtf's own terminal user:
the game sessions, their scrollback, the world database, macros and variables. Per attached client:

* its **own input line** (lines are never merged; whoever sends, sends);
* its **own current session ("view")**: a peer never moves anyone else's view, nor gtf's foreground world;
* its **own terminal width**: lines travel unwrapped and each client wraps for itself.

The one shared number is the width gtf tells the game (NAWS), chosen by a policy (section 7).
Every echoed line says who sent it (section 5). Everybody, the local terminal user included, can see who is
attached, what each views and who is typing (section 6).

## 2. Negotiation

### 2.1 Canonical form: `ext.gtf`

Hello (client to server, after the challenge) gets an optional member `ext`, an object keyed by
extension name. Welcome answers in a member `ext` with the same key. Both are absent unless used.

```json
hello   {"v":1,"t":"hello", ... ,"ext":{"gtf":{"v":1,"minV":1,"features":["author","presence","typing","naws","worlds"]}}}
welcome {"v":1,"t":"welcome", ... ,"ext":{"gtf":{
          "v":1, "serverV":1, "features":["author","naws","presence","typing","worlds"],
          "serverFeatures":["author","presence","typing","naws","worlds"],
          "cid":"7", "naws":"active",
          "limits":{"typingTtlMs":6000,"typingMinGapMs":800,"nawsDebounceMs":300,"minCols":10,"maxCols":1000}}}}
```

Client fields: `v` (integer >= 1, the highest version it speaks, required), `minV` (the lowest it can speak,
default 1), `features` (strings it can use).

Server rules:

1. No `ext.gtf` (and no valid `offer`, 2.2), `squatter_gtf=off`, `squatter_kind=swp`, a `v` that is not an
   integer >= 1, or malformed JSON: the server behaves exactly as if nothing was announced. The welcome has
   no `ext`.
2. `minV` > the server's highest version: **mismatch**. The server answers
   `"ext":{"gtf":{"v":0,"error":"version_mismatch","serverV":1}}` and treats the peer as plain.
3. Otherwise the agreed version is `min(v, serverV)` and the agreed features are the **intersection** of
   `features` and the server's. Unknown feature names are ignored, in both directions. The answer's
   `features` is the agreed list (possibly empty: then only `ext` bookkeeping, no extension messages).
4. A feature is in effect only when it is in the answer's `features`. A peer MUST NOT send a message of a
   feature that was not agreed. Sent anyway, a `gtf.*` message is `bad_request` when the extension was
   negotiated and ignored (an unknown type) when it was not; `world.put/delete/rename` are `unsupported`.
5. `cid` is this connection's id in presence lists (a string; the same device connected twice has two).

Feature flags of version 1:

| flag | what it enables |
|---|---|
| `author` | `author` on echo lines (5) |
| `presence` | `gtf.view`, `gtf.presence`, `gtf.presence.get` (6) |
| `typing` | `gtf.typing` both ways (6) |
| `naws` | `resize` is honoured, `gtf.naws` is sent (7) |
| `worlds` | `world.put/delete/rename/list` with revisions, `world.result`, `world.changed` (8) |
| `macros` | **reserved** (10). Never announced by gtf; a client may include it, it is ignored |

Versioning: a new version only adds optional fields, messages and flags. A server never sends a message
of a feature that was not agreed; a client ignores unknown members and unknown message types (SWP rule).
Version 2, if it ever breaks something, will be refused to a client with `minV` 1 only if it cannot also
speak 1; the server answers with the highest version both support.

### 2.2 Simple form: `offer` / `negotiated`

SquatClient's current placeholder is a flat list. gtf accepts it as an alias (and answers both forms
when `ext.gtf` is absent but `offer` is present):

```json
hello   {... "offer":["gtf","gtf.presence","gtf.worlds"]}
welcome {... "negotiated":["gtf","gtf.presence","gtf.worlds"], "ext":{"gtf":{...as above...}}}
```

* The offer must contain `"gtf"`; without it nothing is accepted (other names are not looked at).
* `"gtf"` alone: version 1, every feature the server has. Feature names beside it narrow the offer; a
  name is either `"gtf.<feature>"` or the bare `<feature>`.
* `negotiated` lists `"gtf"` and every offered feature that was accepted, **spelled as offered**. With
  `"gtf"` alone it is just `["gtf"]`: read the features from `ext.gtf.features`.
* If both `ext.gtf` and `offer` are sent, `ext.gtf` wins.

JSON field names, exactly: hello `ext` (object of objects), `offer` (array of strings); welcome `ext`,
`negotiated` (array of strings); inside `ext.gtf`: `v`, `minV` (hello only), `features`, `serverV`,
`serverFeatures`, `cid`, `naws`, `limits`, `error`.

### 2.3 What a plain peer sees

Exactly today's behaviour: the same welcome (no `ext`, no `negotiated`), the same `line` entries (no
`author`), `world.list` without `rev`, `control.state` without `shared`, `resize` accepted and ignored,
`world.put/delete/rename` answered with an error (`unsupported`, text names the extension), no `gtf.*`
traffic and no `world.changed`. This is tested with a plain and an extension peer connected at once.

## 3. Roles, limits, policy

* Roles gate every mutating message: `viewer` may view, list and receive; `operator` may send, resize, type
  and connect; `admin` may edit worlds and run commands. Legacy `owner` devices count as admin. A refused
  message gets `forbidden`.
* Rates per connection (token buckets, in messages per second / burst): `send` 20/40 (extension peers
  only; plain peers keep today's unlimited behaviour), `gtf.view` 10/20, `gtf.typing` 5/5 (excess dropped
  silently), `resize` 5/10, `world.*` edits 5/10. Excess is answered `rate_limited` except typing.
  Frame size limits are SWP's (1 MiB frame, 64 KiB `send`).
* Nothing blocks gtf's engine goroutine: requests that need the engine wait for it with a 30 s limit
  (`internal` error after that); a slow peer is dropped alone (SWP).
* **Peer-authored code runs under the owner's execution policy.** Everything that executes text that came
  from a peer (a `send` of a `/command` by an admin, `tf.exec`, and in the future `macro.put`) is subject
  to a restriction level configured by the gtf owner (shell and file access blocked by default, the owner
  can lift it); the engine tags the origin so the policy can tell peer code from the owner's own. A refused
  execution answers with the reserved error code **`policy_denied`** (message names what was denied, not
  the policy file). Today (this version) the existing `squatter_commands` switch and gtf's `/restrict`
  level apply and `policy_denied` is not yet emitted; clients MUST already handle it.

## 4. Errors

SWP error message: `{"v":1,"t":"error","reqId":"..","code":"..","message":".."}` plus, for the extension, an
optional `data` member (JSON) with machine-readable detail.

| code | meaning |
|---|---|
| `bad_request` | malformed message, invalid value (message says which), message of a feature not agreed |
| `forbidden` | the device's role does not allow it |
| `rate_limited` | too many messages |
| `no_such_session` | unknown session id |
| `unsupported` | gtf does not implement this message for this peer (`world.*` without the `worlds` feature, `tf.script.*`) |
| `conflict` | optimistic concurrency failed; `data` = `{"current":<world or null>}` |
| `not_found` | no such world |
| `exists` | rename target exists |
| `busy` | world is connected (delete) |
| `policy_denied` | **reserved**: refused by the owner's execution policy for peer-authored code |

## 5. Author tagging (`author`)

`line` entries of kind `echo` (live and in backlogs) carry `"author":"<device name>"`, or `"local"` for
gtf's own terminal or a script. `origin` stays as it was (`"tf"` or the device id) so the id is still
available; two devices can have the same name. A client SHOULD show the author when it is not itself.

```json
{"v":1,"t":"line","session":"g1-1","seq":42,"ts":1760000000000,"kind":"echo","spans":[{"s":"look"}],"origin":"9a8b...","author":"phone"}
```

## 6. Presence, views, typing

* `gtf.view` (client to server, `presence`): `{"t":"gtf.view","session":"<id>"}`; `""` = none. The session the
  client currently displays. Independent of `attach` (a client may be attached to many sessions to receive
  lines, and views one). Unknown session: `no_such_session`. Viewers may send it.
* `gtf.presence` (server to client, `presence`): full snapshot, sent right after the welcome, on
  `gtf.presence.get`, and whenever someone joins, leaves or changes view. First entry is always gtf's own
  terminal user.

```json
{"v":1,"t":"gtf.presence","ts":1760000000000,"peers":[
  {"cid":"local","name":"local","role":"local","local":true,"gtf":true,"view":"g1-1","typing":""},
  {"cid":"7","device":"9a8b..","name":"phone","role":"operator","gtf":true,"view":"g1-1","typing":"g1-1","since":1760000000000},
  {"cid":"8","device":"11cd..","name":"old client","role":"viewer","view":"","typing":"","since":1760000001000}]}
```

  `gtf` is true for clients that negotiated (their `view` is real); others have `view:""`.
* `gtf.typing` (both ways, `typing`; sending needs operator): client `{"t":"gtf.typing","session":"<id>","typing":true}`;
  server to the others `{"t":"gtf.typing","cid":"7","name":"phone","session":"g1-1","typing":true}`.
  A client sends `typing:true` when its input line becomes non-empty and repeats it at most every ~3 s
  while it stays so; `typing:false` when emptied. The server drops updates faster than `typingMinGapMs`,
  expires an indicator not refreshed within `typingTtlMs` (sending `typing:false` to the others) and ends
  it when the peer sends a line. The local terminal user appears as `cid:"local"`.
* `gtf.presence.get`: asks for a snapshot.
* A plain `session.state` of every session carries `tf.fg`: true for the session gtf's own terminal shows.

## 7. Terminal width (NAWS)

`resize` (client to server, `naws`; operator or better): `{"t":"resize","session":"<id>","cols":132,"rows":40}`.
Without the extension it is ignored (today). `cols`/`rows` must be 1..1000 (else `bad_request`, as in SWP).
The width is per client and session; a client that never resized a session counts with its last width.
`rows` are accepted and ignored: the game keeps gtf's own row count.

The width told to the game is chosen per session by the owner's policy `squatter_naws`:

| policy | width |
|---|---|
| `local` | gtf's own terminal only (peers never matter; no extension needed). |
| `active` (default) | the client that last sent input: a peer's `send` to that session, or the local user submitting a line. If nobody ever did, the local terminal. |
| `min` / `max` | the smallest / largest of all attached clients (local terminal included) |

Candidates for a session: the local terminal always; a peer that negotiated `naws`, has the operator role
or better, has attached the session and has sent a width. Corner cases:

* a peer detaches or disconnects: it drops out at once and the width is recomputed;
* no peers: the local terminal's width;
* the value used is clamped to 10..1000 (a peer sending 5 counts as 10);
* zero, negative or over-1000 values are refused with `bad_request` and change nothing;
* a viewer's `resize` is `forbidden`;
* changes are debounced (300 ms, reset by each change) and sent once per session; if the result equals what
  the game already has nothing is sent; with `local` gtf resizes at once as before.

When the reported width of a session changes the server sends the `naws` peers
`{"t":"gtf.naws","session":"g1-1","cols":132,"policy":"active"}` (informational).

## 8. World database

Feature `worlds`. A world has a **revision** (`rev`, a positive integer): it changes on every change to the
record (put, rename) and is never reused, not even by a world of the same name created later, and is larger
after a gtf restart than before it. Revisions are opaque: compare for equality only.

* `world.list` (any role): `{"t":"world.list","id":"r1"}` returns `{"t":"world.list","reqId":"r1","worlds":[...]}`
  (as TFSP, plus `rev`). Never a password (`passSet` only), never a macro file; temporary worlds
  (`temp:true`) are listed.
* `world.put` (admin): create or replace

```json
{"t":"world.put","id":"r2","baseRev":0,"world":{"name":"mush","host":"mush.example","port":"4201","type":"tiny","char":"bob","tls":true,"echo":false,"charset":"UTF-8"},"password":"write-only","clearPassword":false}
```

  `baseRev` is the revision the edit is based on, 0 meaning "the world does not exist". It applies only when
  that is still the current state; otherwise the reply is `error` `conflict` with `data.current` (the world as
  it is now, or `null` when it was deleted) so the client can merge and retry with the new `rev`. Fields are
  **replaced**: an omitted/empty field clears the old value. Not part of the wire record and therefore
  kept as they are: macro file, source address, proxy flag. The password is kept unless `password` is non-empty
  (set) or `clearPassword` is true. It is never echoed, logged, listed or put in an event or audit line.
  The macro file (code gtf would load) cannot be read or set by a peer (`world.mfile` or `setMfile` is
  `bad_request`).
  Validation (`bad_request` with the reason): name 1-64 characters without spaces, quotes, `;` `%` `\`, no leading
  `(` or `/`, not `default`; host `[A-Za-z0-9._:%\[\]-]` up to 253 and port both set or both empty; port 1-65535
  or a service name; `type`/`charset` `[A-Za-z0-9_.+-]` up to 32; `char` up to 64 without control characters,
  quotes or backslashes; password up to 256 without control characters, quotes or backslashes.
* `world.delete` (admin): `{"t":"world.delete","id":"r3","name":"mush","baseRev":123}` (`baseRev` required).
  `busy` while connected, `not_found`, `conflict`.
* `world.rename` (admin): `{"t":"world.rename","id":"r4","name":"mush","newName":"mush2","baseRev":123}`; works while
  connected (the session keeps its id); `exists` if the target exists.
* Success reply to all three: `{"t":"world.result","reqId":"r2","op":"put","created":true,"world":{...with rev...},"oldName":"","saved":true,"saveError":""}`
  (`world` is null for delete; `oldName` set for rename). `saved` says whether gtf rewrote its world file.
* `world.changed` (server to every `worlds` peer, the requester included): `{"t":"world.changed","name":"mush","op":"put|delete|rename","created":true,"deleted":true,"oldName":"..","rev":124,"world":{...},"author":"phone","ts":..}`.
  `author` is the device name or `"local"` (the change was typed or scripted in gtf: `/addworld`, `/unworld`,
  `/renameworld`, `addworld()` all notify). `world` is absent for delete.
* Persistence: a peer's edit rewrites the world file the way `/saveworld` does (the file named by the
  `WORLDFILE` macro, every world, mode 0600) unless `squatter_world_save=off`. Details and the backup rule
  are in docs/SQUATTER.md.
* Audit: every accepted peer edit prints one line on gtf's own screen only, never into a scrollback:
  `% squatter: phone (admin) updated world mush [host,port,password], world file saved` (field names,
  never values).

## 9. control.* for extension peers

`control.get` / `control.set` still answer `input:"both"`, `automation:"tf"` (the real model: everyone
types, gtf's triggers run). For a peer with the extension the `control.state` also has
`"shared":{"model":"shared","naws":"<policy>","cols":<width told to the game>,"attached":<n clients attached to the session>}`.
`control.set` changes nothing and does not fail.

## 10. Reserved: macro / trigger / gag sync (not implemented)

Reserved so that the negotiation and the namespace need no change later.

* Feature flag `macros`; message family `macro.*` (`macro.list`, `macro.put`, `macro.delete`,
  `macro.changed`, `macro.get`), error `policy_denied`.
* A macro definition (`/def` with its trigger, hook, gag, hilite or substitution options; variables the
  user marks shared) is replicated between SquatClient and gtf. Each carries a **scope**: `both` (default),
  `local` (runs only in SquatClient: notifications, sounds, GUI scripts) or `remote` (runs only in gtf).
* **Exactly-once rule.** A `both` macro is *known* on both ends, so it works whether SquatClient is attached
  to gtf or connected straight to a game, but it *executes* on exactly one side: the side that owns the game
  connection. While SquatClient is attached to gtf that is gtf; a trigger that sends text or echoes
  therefore never fires twice. When SquatClient owns the connection it is SquatClient. `local` macros run
  in SquatClient only and `remote` ones in gtf only, regardless. The executing side is decided per
  connection, not per macro, and a client is told (in `gtf.presence`-like fashion) which side executes.
* Versioning and conflicts reuse section 8: a macro has a `rev`; `macro.put` carries `baseRev`; a stale put
  is `conflict` with the current definition in `data.current`; deletions and renames follow `world.*`;
  `macro.changed` is broadcast with `author`. Definitions are identified by name (plus the engine's macro
  number only locally).
* Peer-authored macros run under the owner's execution policy (3): `macro.put` can be answered
  `policy_denied`, and a macro that is allowed to run is restricted to the level configured for peers.

## 11. Next: layouts, panels and canvases for native widgets (sketch, not implemented)

gtf has layouts, panels and canvases (`/panel`, `/canvas`, `/layout`), drawn by its terminal UI. So that
SquatClient can render native widgets instead of cells, stream the definition and the data it binds to,
not pixels:

```json
{"t":"layout.def","session":"g1-1","name":"vitals","rev":3,"kind":"panel","dock":"left","size":24,
 "title":"Vitals","body":"HP {bar \"gmcp:Char.Vitals.hp\" \"gmcp:Char.Vitals.maxhp\" width=10} {gmcp:Char.Vitals.hp}",
 "binds":["gmcp:Char.Vitals.hp","gmcp:Char.Vitals.maxhp"]}
{"t":"layout.changed","name":"vitals","rev":4,"deleted":false}
{"t":"layout.values","session":"g1-1","asOf":1042,"values":{"gmcp:Char.Vitals.hp":"85","var:foo":"x"}}
```

Reasoning: the definition is small and versioned like worlds (section 8), values already travel as `gmcp`,
`msdp` and the `state` snapshot, so only gtf variables (`var:`) need a new push (`layout.values`, sent
for the names a definition binds to, coalesced). The client interprets `body` itself (the format is tf-style
text with `{bar ...}`, `{gmcp:...}` placeholders) or maps `kind` to a native widget; gtf stays the source of
truth. Feature flag `layouts`. Macro/script sync is section 10; script upload keeps using `tf.exec` with
`/load` (admin, under the execution policy).

## 12. Checklist for SquatClient

1. Announce `ext.gtf` (or `offer:["gtf"]`) in hello; read `ext.gtf.features` from the welcome; use only agreed features.
2. Echo lines: show `author`. Keep working when it is absent.
3. Per window: send `gtf.view` when the displayed session changes; `resize` with the window width on attach
   and on change; `gtf.typing` while the input line is non-empty.
4. Render `gtf.presence` and `gtf.typing`; handle `gtf.naws` (informational).
5. Worlds: list with `rev`; edit with `world.put/delete/rename` carrying `baseRev`; on `conflict` merge
   `data.current` and retry; update from `world.changed`; show `saved`/`saveError`.
6. Treat `unsupported` as "not available here", `policy_denied` as "the owner forbids this", `rate_limited`
   as back off.
7. Never assume `control.set` changed anything; read `control.state`.
8. A peer must not send a feature message that was not agreed.

## 13. Examples

Negotiation, mismatch and rejection (plain behaviour):

```
hello {"ext":{"gtf":{"v":1,"features":["author","telepathy","macros"]}}}
welcome {"ext":{"gtf":{"v":1,"serverV":1,"features":["author"],...}}}      unknown and reserved flags dropped
hello {"ext":{"gtf":{"v":3,"minV":2,"features":["author"]}}}
welcome {"ext":{"gtf":{"v":0,"error":"version_mismatch","serverV":1}}}      stays plain
hello {"ext":{"gtf":{"v":0}}}     welcome (no ext)                           invalid: as if nothing was said
```

A stale world edit:

```
-> {"t":"world.put","id":"r9","baseRev":100,"world":{"name":"mush","host":"h2","port":"1"}}
<- {"t":"error","reqId":"r9","code":"conflict","message":"the world changed since the revision the edit is based on",
    "data":{"current":{"name":"mush","host":"h","port":"4201","passSet":true,"rev":105}}}
```

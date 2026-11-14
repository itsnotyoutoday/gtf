# Requests for the scripting engine (gtfparser)

The client never works around an engine bug; each item here says what differs from real
tf, where in the engine, and gives a script that shows it. Engine changes are made in the
engine repository. Compared against the reference tf build used by `scripts/compare_tf.py`.

## 1. `${world_name}` (and the other `${world_*}` bodies) with no current world must expand to nothing

**Where:** `interp.go`, `runSub` (the `subMacro` case): when `WorldMacroBody` reports "no current
world" the code falls through to `findMacro` and then prints `% macro not defined: world_name`.

**Real tf:** expands to the empty string, silently. The library relies on it: `prev_world_hook`
(`-hWORLD`, stdlib via world-q.tf) runs `/set fg_world=${world_name}` on *every* WORLD event,
including `/fg -n` ("no world"), so in gtf every `/fg -n` (and every switch to no world) prints
`% macro not defined: world_name`.

**Repro** (needs a connected world so that something is in the foreground first):

    /addworld A localhost 4000
    /connect A
    /fg -n          <- gtf: "% macro not defined: world_name"; tf: nothing

or without any socket: `/def -hWORLD w = /echo [${world_name}]` then `/fg -n` while another world is
foreground. Scenario `conn-fg-none` in `scripts/compare_tf.py` shows the difference.

## 2. `/limit -m` error text has one space where tf has two

**Where:** `misc_state.go:240`: `Invalid -m value "%s". Valid values are: ...`.
tf (and `match.go:253`, `attrs.go:917` in the engine itself) use two spaces after the period:
`% LIMIT -m: Invalid -m value "".  Valid values are: simple (0), glob (1),`.

**Repro:** `/limit -m glob x*` (scenario `limit`).

## 3. Macro numbers: the first user macro is #776 in gtf, #779 in tf

`/def foo = /echo hi` then `/list foo` prints `% 776: /def foo = /echo hi` in gtf and
`% 779: ...` in real tf started with the same library (`-f`, no `.tfrc`; scenario `list-def`).
Three macro numbers are consumed in tf during startup that the engine does not consume
(candidates: macros tf creates itself while loading the library, or numbers used by
`/def` calls that fail/redefine). Not a client issue: the client loads the identical library
files.

## 4. `/listsockets` for a dead socket

For a socket whose connection ended tf prints `(null)` in the HOST and PORT columns (it freed
the strings but kept the address), and nothing for a socket that never resolved. The engine
prints the `Host`/`Port` it is given when `Addr != ""`, so the client passes the literal
`(null)` for dead sockets to get the same output. If the engine would rather own this rule,
make it print `(null)` for `State >= SockZombie` with a non-empty `Addr`, and the client can
then pass the real host and port.

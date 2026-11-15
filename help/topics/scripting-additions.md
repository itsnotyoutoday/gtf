---
title: What gtf adds to scripting
topics: gtf-functions, gmcp-functions, scripting-additions
section: Scripting
order: 20
---
# What gtf adds to scripting

These commands, functions and hooks exist in gtf and not in TinyFugue. All are ordinary parts of the language: you use them in macros, triggers, expressions and your startup file.

## Data the game sends (GMCP, MSDP, MSSP)

Games that speak these protocols send structured data (health, room, exits and so on). gtf keeps the latest of it for every connection and lets scripts read it:

| function | returns |
|---|---|
| `gmcp_get("Char.Vitals.hp")` | one GMCP value as text; arrays count from 0 (`Room.Info.exits.0`) |
| `gmcp_json("Char.Vitals")` | a package as JSON text; `gmcp_json("")` is everything received |
| `gmcp_has("Char.Vitals")` | whether anything has arrived for that path |
| `msdp_get("HEALTH")` | an MSDP variable |
| `mssp_get("NAME")` | a value from the server's status report |
| `gmcp_send("Char.Vitals.Get")` | send a GMCP message; an optional second argument is the JSON body |
| `msdp_send("REPORT", "HEALTH", "MANA")` | send an MSDP command with its arguments |

Each reader takes an optional last argument naming a world (`gmcp_get("Room.Info.name", "mymud")`); without it, the world in front is used.

`gmcp("Package json")` and `msdp("VAR=value")` keep TinyFugue's meaning: they **send**. That is why the readers have the longer names.

Two hooks announce the data as it arrives:

| hook | arguments |
|---|---|
| `GMCP` | `Package {json}` |
| `MSDP` | `VAR=value` (arrays as `{ a b }`, tables as `[ k=v ]`) |

```
/def -hGMCP -t'Char.Vitals *' lowhp = \
    /if (gmcp_get("Char.Vitals.hp") < 20) \
        /echo -aBCred LOW HP%; \
    /endif
```

Panels read the same data directly, with no script at all: see [[Panels and layouts]].

## Screen

| | |
|---|---|
| `term_width()`, `term_height()` | the size of the terminal, for scripts that draw to fit it |
| `worldfile()` | the path of the world file the `WORLDFILE` macro names (`~` expanded), or an empty string |
| macro `gtf_startup` | what gtf does after your startup file (it loads the world file); `/undef` or redefine it in your startup file: [[Worlds and world files]] |
| hook `RESIZE` | the terminal changed size; the arguments are the new width and height |
| `/panel`, `/layout` | panels around the output window — [[Panels and layouts]] |
| `/canvas`, `/sprite` | drawing surfaces and vector pictures — [[Canvases and sprites]] |
| `/sidebar`, `/panelhp` | helpers written in the language: [[Panels and layouts]] |

## Running the program

| | |
|---|---|
| `/restart` | replace gtf without dropping connections — [[Restarting without dropping connections]] |
| `/update` | check for and install a new version — [[Updating gtf]] |
| `/version` | gtf's version; `/ver` and the variable `TFVERSION` give the TinyFugue release gtf is compatible with, which is what scripts compare against |
| `/cmcp` | the console protocol — [[CMCP keys and commands]] |
| `/squatter`, `/pair` | remote access — [[Squatter]]; its hooks and variables are in [[Squatter hooks]] and [[Squatter settings]] |

## Differences in familiar things

* `%version` holds gtf's own version text. Scripts that need to know which TinyFugue language level is available should test `TFVERSION` (or call `/ver`).
* The terminal type gtf announces to a game is `TINYFUGUE` first and `GTF` afterwards (see [[Network protocols]]), because games recognise TinyFugue by name.

See also: [[Scripting in gtf]], [[Commands and variables index]].

# GoTinyFugue (gtf)

GoTinyFugue (`gtf`) is a terminal MU* client written in Go. It is TinyFugue-compatible: it reads and runs
TinyFugue scripts and library files, but it is its own program, not TinyFugue, and is not made or endorsed by
the TinyFugue authors. It is built on the shared scripting engine
[gtfparser](https://github.com/itsnotyoutoday/gtfparser).

- A screen laid out like TinyFugue's (output window, status line, input window), the same default key bindings
  (TinyFugue's own `kbbind.tf` runs unmodified), and TinyFugue's commands (the gtfparser engine implements them).
- Telnet with GMCP, MSDP, MSSP, MCCP2, NAWS, TTYPE/MTTS, CHARSET, EOR/GA, TLS.
- Panels and layouts (HP bars, sidebars) driven by GMCP/MSDP/TF variables: `docs/LAYOUTS.md`. Try the demos right away (they ship with gtf and load themselves): `/layoutdemo` for a guided tour of panels and canvases, `/lcars` for a TNG-style bridge console.

```
go build ./cmd/gtf && ./gtf -f -c'/addworld mymud host 4000' mymud
```

The command-line options follow TinyFugue's: `-L<dir> -f -c<cmd> -n -l -q -v`, then a world name or `host port`.

Docs: `docs/ARCHITECTURE.md` (layout, import rules, merge checklist), `docs/PROTOCOLS.md`,
`docs/LAYOUTS.md`, `docs/COMMAND-STATUS.md`, `docs/ENGINE-REQUESTS.md` (open requests to the engine).

Tests: `GOWORK=off go test ./...`. Against real tf: `python3 scripts/compare_tf.py`
(needs `TF_REF`/`TF_LIB`, see `scripts/env.sh`; `conn-*` scenarios drive both clients against local TCP
servers the harness starts; `TF_CACHE=dir` reuses real-tf screens between runs). Against the local Evennia test game:
`python3 scripts/e2e_evennia.py` (skipped when it is not running).

## Install

    curl -fsSL https://raw.githubusercontent.com/itsnotyoutoday/gtf/main/install.sh | sh              # just you (~/bin)
    curl -fsSL https://raw.githubusercontent.com/itsnotyoutoday/gtf/main/install.sh | sh -s -- --global   # everyone (/usr/local)

Then `/update` checks for a newer version and `/update -y` installs it. See `docs/INSTALL.md`.

## Building

    go build -o gtf ./cmd/gtf      # or: make build / make run

The scripting engine is a separate module, [gtfparser](https://github.com/itsnotyoutoday/gtfparser), pulled in as a
normal dependency (pinned in `go.mod`). To work on both repositories at once, a git-ignored `go.work`
(`use ( . ../gtfparser )` with a sibling checkout) gives one compile with engine edits seen immediately. Any change
the client needs in the scripting engine is made in the engine repository, not here.

## License and credits

Copyright (C) 2026 Rick Bird. GoTinyFugue is free software, licensed
GPL-3.0-or-later; see `LICENSE`.

- [TinyFugue](https://tf.tcp.com/~hawkeye/tf/), copyright Ken Keys and contributors (GPL-2.0-or-later), is the
  program gtf is compatible with; gtf is not affiliated with its authors. TinyFugue's standard library and manual
  are embedded unmodified in `gtflib/tf/` (see `gtflib/TF-NOTICE.md`) and keep their own copyright notices; the
  scripting engine itself does not contain them. `/help copyright` shows gtf's notice first, then TinyFugue's.
- The [PCRE2](https://www.pcre.org/) project, whose regexp semantics the engine's `pcre`
  package follows.

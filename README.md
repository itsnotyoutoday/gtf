# GoTinyFugue (gtf)

GoTinyFugue (`gtf`) is a terminal MU* client that looks and behaves like TinyFugue, built on the shared
scripting engine [gtfparser](https://github.com/itsnotyoutoday/gtfparser).

- Same screen as tf (output window, status line, input window), same key bindings
  (the real `kbbind.tf` runs unmodified), same commands (the engine implements them).
- Telnet with GMCP, MSDP, MSSP, MCCP2, NAWS, TTYPE/MTTS, CHARSET, EOR/GA, TLS.
- Panels and layouts (HP bars, sidebars) driven by GMCP/MSDP/TF variables: `docs/LAYOUTS.md`. Try the tour: `/load examples/layout-demo.tf` then `/layoutdemo`.

```
go build ./cmd/gtf && ./gtf -f -c'/addworld mymud host 4000' mymud
```

Options follow tf: `-L<dir> -f -c<cmd> -n -l -q -v`, then a world name or `host port`.

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

- [TinyFugue](https://tf.tcp.com/~hawkeye/tf/) by Ken Keys (GPL-2.0-or-later) is the
  inspiration for GoTinyFugue and its compatibility target. Its standard library is embedded
  unmodified in `gtflib/tf/` (see `gtflib/TF-NOTICE.md`); the scripting engine itself does not
  contain it, and the original copyright notice is shown by `/help copyright`.
- The [PCRE2](https://www.pcre.org/) project, whose regexp semantics the engine's `pcre`
  package follows.

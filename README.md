# gtf

A terminal MU* client that looks and behaves like TinyFugue, built on the shared
scripting engine [gtfparser](https://github.com/itsnotyoutoday/gtfparser).

- Same screen as tf (output window, status line, input window), same key bindings
  (the real `kbbind.tf` runs unmodified), same commands (the engine implements them).
- Telnet with GMCP, MSDP, MSSP, MCCP2, NAWS, TTYPE/MTTS, CHARSET, EOR/GA, TLS.
- Panels and layouts (HP bars, sidebars) driven by GMCP/MSDP/TF variables: `docs/LAYOUTS.md`.

```
go build ./cmd/gtf && ./gtf -f -c'/addworld mymud host 4000' mymud
```

Options follow tf: `-L<dir> -f -c<cmd> -n -l -q -v`, then a world name or `host port`.

Docs: `docs/ARCHITECTURE.md` (layout, import rules, merge checklist), `docs/PROTOCOLS.md`,
`docs/LAYOUTS.md`, `docs/COMMAND-STATUS.md`.

Tests: `GOWORK=off go test ./...`. Against real tf: `python3 scripts/compare_tf.py`
(needs `TF_REF`/`TF_LIB`, see `scripts/env.sh`). Against the local Evennia test game:
`python3 scripts/e2e_evennia.py` (skipped when it is not running).

## Building

The scripting engine is a separate module, [gtfparser](https://github.com/itsnotyoutoday/gtfparser),
pulled in as a normal dependency. It is not part of this repository. Until the engine release this
client needs is published, `go.mod` carries a temporary `replace` that points at a sibling checkout:

    git clone https://github.com/itsnotyoutoday/gtfparser ../gtfparser
    go build ./cmd/gtf

(Delete the `replace` line in `go.mod` once the engine tag is on GitHub; `go build` then downloads it.)
To work on both repositories at once, a git-ignored `go.work` (`use ( . ../gtfparser )`) gives one compile
with engine edits seen immediately. Any change the client needs in the scripting engine is made in the
engine repository, not here.

Licensed GPL-3.0-or-later; TinyFugue is GPL-2.0-or-later.

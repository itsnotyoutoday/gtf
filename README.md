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

`_engine/` holds the `tflib` package (the unmodified real tf library embedded for both clients)
staged for the engine repository; see docs/ARCHITECTURE.md.

Licensed GPL-3.0-or-later; TinyFugue is GPL-2.0-or-later.

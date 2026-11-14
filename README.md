# GoTinyFugue (gtf)

GoTinyFugue (`gtf`) is a terminal MU* client that looks and behaves like TinyFugue, built on the shared
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
pulled in as a normal dependency (`go.mod` pins its version). It is not part of this repository.

    go build ./cmd/gtf          # builds against the version pinned in go.mod

To work on the client and the engine together (one compile, edits to the engine seen at once), keep a
checkout of the engine next to this one and add a git-ignored `go.work`:

    git clone https://github.com/itsnotyoutoday/gtfparser ../gtfparser
    cat > go.work <<'EOF'
    go 1.24
    use ( . ../gtfparser )
    replace github.com/itsnotyoutoday/gtfparser v0.0.5 => ../gtfparser
    EOF

(`go.work` is in `.gitignore`; use the version number that `go.mod` requires.) Any change the client needs
in the scripting engine is made in the engine repository, not here.

## License and credits

Copyright (C) 2026 Rick Bird. GoTinyFugue is free software, licensed
GPL-3.0-or-later; see `LICENSE`.

- [TinyFugue](https://tf.tcp.com/~hawkeye/tf/) by Ken Keys (GPL-2.0-or-later) is the
  inspiration for GoTinyFugue and its compatibility target. Its standard library is embedded
  unmodified through the engine's `tflib` package (see the NOTICE there); the original
  copyright notice is shown by `/help copyright`.
- The [PCRE2](https://www.pcre.org/) project, whose regexp semantics the engine's `pcre`
  package follows.

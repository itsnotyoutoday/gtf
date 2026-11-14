# gtf

A Go implementation of the TinyFugue scripting language and client.

- `gtfparser/` — the language: expressions, `%`/`$` substitution, macros, triggers, hooks, `/load`. Embeddable; talks to its front end through a small `Host` interface.
- `cmd/gtf/` — the client (batch mode so far).
- `scripts/difftest.py` — differential tests: runs a script through real TinyFugue and through gtf and diffs the output. `source scripts/env.sh` points it at a reference build.

Goal: run TinyFugue's own library (`lib/tf/*.tf`) unchanged. Licensed GPL-3.0-or-later; TinyFugue is GPL-2.0-or-later.

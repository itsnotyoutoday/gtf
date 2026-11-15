---
title: Files and folders
topics: gtf-files, folders, file-locations
section: Reference
order: 20
---
# Files and folders

Where gtf reads and writes things. `~` is your home folder.

## Files you write

| file | purpose |
|---|---|
| `~/.tfrc` | your startup file; also looked for as `~/tfrc`, `./.tfrc` and `./tfrc`, in that order, and the first one found is loaded — [[Configuration and startup files]] |
| `~/tiny.world` | the world file written by `/saveworld` and read by `/loadworld`; its name comes from the macro `WORLDFILE` — [[Worlds and world files]] |
| any `.tf` file | your scripts, run with `/load` |
| any `.layout` file | layouts; searched in the current folder, `~/.gtf/layouts` and gtf's own — [[Panels and layouts]] |
| log files | wherever you point `/log` — [[Logging]] |

## Files gtf keeps in ~/.gtf

| path | contents |
|---|---|
| `config.json` | update settings: `update_check`, `update_channel` — [[Updating gtf]] |
| `update-state.json` | when gtf last looked for an update, and what it found |
| `layouts/` | layouts saved with `/layout save` |
| `cmcp/packs/` | layout and theme packs fetched from games, filed by host and checksum |
| `cmcp/consent.json` | your answers to pack questions and the pack signing keys you have trusted |
| `cmcp/prefs.json` | the saved width of the game output on console screens |
| `squatter/` | the squatter's certificate, paired devices and pairing codes (`squatter_dir`; see [[Squatter security]]) |
| `restart-<pid>.state` | exists only for a moment during `/restart`, readable by you alone |

A few of these hold secrets: the squatter folder, the world file and a restart state file. gtf writes them readable by you alone.

## The program and its library

| | user install | global install |
|---|---|---|
| program | `~/bin/gtf` | `/usr/local/bin/gtf` (a package: `/usr/bin/gtf`) |
| library | `~/lib/gtf` | `/usr/local/lib/gtf` (a package: `/usr/lib/gtf`) |

The library folder has `tf/` (TinyFugue's standard library files and its manual, unmodified), `gtf/` (gtf's own scripts: `layout.tf`, `identity.tf`, the demos in `examples/` and the layouts in `layouts/`), `docs/` and `install.json`, which records who installed it. See [[Installing gtf]].

When gtf runs without an installed library of its own version, it unpacks the library embedded in the program into your cache folder: `~/Library/Caches/gtf` on macOS, `~/.cache/gtf` on Linux, so that a single copied binary works.

Inside gtf the variables `TFLIBDIR` (the TinyFugue library, `tf/`), `GTFLIBDIR` (gtf's own scripts) and `TFHELP` (the bundled manual) tell you which copy is in use; `/echo %TFLIBDIR` prints one.

## Licenses and notices

The text of the GNU General Public License is `LICENSE` in the source tree and in release files, and `/help copyright` shows the notice of the TinyFugue library. See [[License and credits]].

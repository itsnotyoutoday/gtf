---
title: CMCP layout packs
topics: cmcp-packs, packs
section: Console protocol
order: 40
---
# CMCP layout packs

A game can describe its own console screens, or its own themes, in a **pack**: a small data file, not a program. gtf shows a pack as cells and keys like any built-in screen. A pack cannot run code on your machine.

## How a pack arrives

The game announces a pack to gtf (its name, version and a checksum). gtf then:

1. fetches it, either in pieces over the same connection or over HTTPS from the **game's own host** (or from a place you listed in `%cmcp_origins`; nowhere else);
2. checks its SHA-256 checksum and size limits, and reads the whole file or rejects it;
3. keeps a copy under `~/.gtf/cmcp/packs`, filed by the game's host and the checksum, so it is not fetched again;
4. shows its screens. Until a pack is usable, the built-in screen of the same console is shown. A bad pack leaves the built-in screens and the session working.

A pack with an invalid signature is rejected and never offered to you. Keys that signed a pack are remembered the first time you see them (trust on first use), together with your answers, in `~/.gtf/cmcp/consent.json`.

## Consent

`%cmcp_pack_consent` decides whether you are asked:

| value | meaning |
|---|---|
| `always` (default) | a pack or theme announced by the game's own address is taken without a question; one quiet line in the output says so. All the checks above still apply |
| `ask` | the first use of a pack asks: a line in the output and in the status line; answer with **F5** (allow) or **F6** (refuse), or `/cmcp pack allow` and `/cmcp pack deny`. Your answer is remembered per game and pack. A question belongs to the ship that announced the pack and is dropped when you hop to another ship |
| `never` | built-in screens only |

`/cmcp packs` lists the packs known for the world in front, their state and the cache. `/cmcp themes` does the same for themes — [[CMCP settings]].

## Notices

The top row of a console screen shows one short notice at a time: `Downloading ... theme pack`, replaced in place by `Downloaded ... theme pack`, then `Accepted interface pack ...`, and command errors and hints. They stay a few seconds.

See also: [[CMCP console protocol]], [[CMCP settings]].

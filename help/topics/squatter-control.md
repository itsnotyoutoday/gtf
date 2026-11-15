---
title: Who types
topics: squatter-control, control, remotecontrol
section: Remote access
order: 60
---
# Who types

Today there is one mode, **shared**: you at this terminal and every attached remote client (role permitting) can type at the same time, every line shows who sent it, and gtf runs the triggers. `/squatter who` shows who is attached and who is typing.

## Planned, not available yet

A setting, `squatter_control`, will let you change that:

| mode | who types |
|---|---|
| shared | everyone (today) |
| ask | a remote may type only after you `/allow` it |
| polite | a remote may type only while this terminal has been idle (`squatter_control_polite` seconds, default 20) |
| takeover | a remote holds control; your lines to the game are locked until `/takeback` (commands still work) |
| watch | remotes can only watch |

The design, commands and settings are in `docs/SQUATTER-CONTROL.md` in the repository. Until it ships, setting `squatter_control` has no effect.

## What you can do today

* give a device the viewer role (`/pair viewer`) so it cannot type: [[Pairing devices]]
* set `squatter_commands=off` so nothing a remote sends runs as a command
* `/squatter kick <device>` to disconnect one

See [[Squatter security]] and [[Squatter settings]].

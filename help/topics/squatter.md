---
title: Squatter
topics: squatter, /squatter
section: Remote access
order: 10
---
# Squatter

The squatter keeps your connections up and lets SquatClient (desktop and phone) attach to them over an encrypted connection. The games never see you disconnect when SquatClient closes. SquatClient shows the scrollback and lets you type, and gtf keeps running your triggers.

## Quick start

```
/squatter on          start it; it says which address it listens on
/pair                 print a one-time pairing code
```

Then in SquatClient add a squatter of type "gtf (GoTinyFugue)" with this machine's address, port 10328 and the code. Check that the fingerprint SquatClient shows is the one gtf printed. The same steps, in detail: [[Squatter quick start]].

## More

* [[Squatter address and port]]: change where it listens
* [[Pairing devices]]: codes, roles, the fingerprint, removing devices
* [[What remote clients see]]: what is forwarded and what stays on this screen
* [[Who types]]: you, the remote, or both
* [[Squatter settings]]: every `squatter_*` variable
* [[Squatter security]]: what is exposed and how to reduce it
* [[Squatter hooks]]: react to devices pairing and connecting
* [[Squatter troubleshooting]]: when it does not work

## Commands

`/squatter [status | on | off | list | who | kick | unpair | fingerprint | port | bind | name | pair | help]` and `/pair [viewer | operator | admin]`.

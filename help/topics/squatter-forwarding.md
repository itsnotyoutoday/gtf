---
title: What remote clients see
topics: squatter-forwarding, squatter-output, squatter-local
section: Remote access
order: 50
---
# What remote clients see

A remote client shows what the game sent, plus the lines you and it typed. gtf knows where every line came from and forwards only what belongs there.

## Forwarded

* game text, after your triggers, gags and highlights have run
* what you (or a remote) sent to the game, marked as an echo
* what a trigger or hook prints while a game line is being handled, as a "system" line

## Not forwarded

* the output of a command a paired device typed (the typed line itself is echoed, so the device shows what it typed, but gtf's answer, an error or the `/restart` plan for example, stays on this screen)
* gtf's own messages and script errors (`% ...`, for example `% SQUAT: not supported by this client`)
* the output of commands you type here, including `/recall` (it carries its own `[hh:mm]` stamps, which would show twice next to the remote client's timestamps)
* the output of hooks that run at connect time
* none of the above is replayed to a client that attaches later either

`/set squatter_local=on` forwards your client's own text as well, as system lines. It takes effect at once and disconnects nobody.

Everything `/squatter` and `/pair` print (state, fingerprint, device names) stays on this screen and is never put in a session's scrollback, which every paired device can replay. See [[Squatter settings]].

---
title: Restarting without dropping connections
topics: gtf-restart, restart, /restart
section: Using gtf
order: 60
---
# Restarting without dropping connections

`/restart` replaces the running gtf with a fresh start of the same program file while your game connections stay open. The game does not see a disconnect, and the screen looks the same afterwards. Use it after an update, or just to start from a clean process.

```
/restart          restart; refuses if a connection
                  could not be carried
/restart -n       only say what would be carried and what would end
/restart -f       restart anyway; what cannot be
                  carried is reconnected
```

`/update -y -r` installs a new version and restarts into it in one step — [[Updating gtf]].

## What is kept

* every connection that is fully established, plain or TLS, with its telnet state, compression and any half-received line;
* the text of every world's window, with colours, the pager state and the line you were typing, together with your input history;
* which world is in front, the status line, and your variables, macros, triggers, hooks, key bindings, worlds, running timers (`/repeat`, `/at`) and limits;
* your panels and layouts, and the existing size of canvases;
* dead sockets, with their windows;
* the [[Squatter]]: its listening socket, its sessions with their scrollback, and the devices that are connected stay connected, with no new pairing and the same fingerprint;

A game that supports the console protocol is told to start the exchange again afterwards; see [[CMCP console protocol]].

## What is not kept

* Connections that are still being set up, and a few TLS connections that cannot be carried over (TLS 1.2 with a non-AEAD cipher, or a connection that renegotiated). `/restart` refuses and tells you which, and `/restart -f` closes them and connects the world again by itself, printing "Reconnecting ... after /restart" in its window. The game sees a normal disconnect and a new login.
* The pixels of canvases: the canvas exists again, empty, until a script draws on it.
* A device of the [[Squatter]] whose connection cannot be carried (TLS 1.2 with a non-AEAD cipher, or one that renegotiated): it reconnects by itself and carries on from the last line it had. `/restart` does not refuse for it.
* Open pipes and the stack of macros being run.
* **Library files are not run again.** The new program's copy of the standard library does not replace macros the old process already loaded. If the new version changed the library, `/load` the file or quit and start gtf again.

## If something goes wrong

gtf first tries the restart on the side: it starts the new program as a test copy, which restores everything and reports back without touching a connection. If the test copy fails, hangs (10 seconds by default) or complains, gtf kills it, takes everything back and says why (`% /restart: ...; gtf keeps running`). Nothing on your terminal changed. Set `GTF_RESTART_TIMEOUT` (for example `20s`) if a big session needs more time.

The hand-over is a file named `~/.gtf/restart-<pid>.state` that only you can read. It contains live connection keys and your world passwords, so gtf deletes it as soon as it has been read, or when the restart is rolled back.

`/restart` works where gtf can run a program in place (Linux, macOS and other Unix systems). Elsewhere it says that it is not supported.

See also: [[Updating gtf]], [[Several connections]].

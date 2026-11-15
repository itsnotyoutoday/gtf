---
title: Connecting to a world
topics: connecting, connect-to-world
section: Start here
order: 40
---
# Connecting to a world

In gtf a **world** is a saved address: a name, a host, a port, and optionally a character name and password. You connect to worlds by name.

## Save a world and connect

```
/addworld mymud mud.example.com 4000
/connect mymud
```

With a character, give its name and password before the host: `/addworld mymud Rick secret mud.example.com 4000`. gtf then logs in for you when it connects (for an ordinary world it sends `connect name password`; the world's type can change that, see `/help tf autologin`; `-l` on `/connect` or on the command line turns it off for one connection). For a server that uses TLS, add the `x` flag to the world or use `/connect -x`:

```
/addworld -x mymud mud.example.com 4443
```

**gtf does not check the server's TLS certificate.** The connection is encrypted, but gtf does not verify who is at the other end.

Without saving anything, `/connect host port` opens a temporary world. gtf forgets it when the connection is gone.

```
/connect mud.example.com 4000
```

`/listworlds` shows what is saved. Worlds you add last only for this session until you save them: see [[Worlds and world files]].

## While connecting

gtf prints "Trying to connect" lines, then "Connected to mymud." and the game's text. If the name cannot be looked up or the connection is refused, you get a message in that world's window and the world stays in your socket list, marked `!`, until you leave it (see [[Several connections]]). A name that resolves to several IPv4 addresses is tried address by address.

When the game turns on telnet ECHO (a password prompt), gtf hides what you type. It also speaks the common telnet extensions games use (window size, character set, GMCP, MSDP, MSSP, MCCP2 compression): see [[Network protocols]].

## Useful options

| | |
|---|---|
| `/connect -x world` | use TLS |
| `/connect -l world` | do not log in automatically |
| `/connect -q world` | log in quietly |
| `/connect -b world` | connect in the background, keep your current world in front |

From a shell: `gtf mymud`, `gtf host port`, `gtf -n` (do not connect), `gtf -l`, `gtf -q`. The full syntax of `/addworld` and `/connect` is in the TinyFugue manual: `/help tf /addworld` and `/help tf /connect`.

See also: [[Getting around]], [[Worlds and world files]].

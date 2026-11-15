---
title: Where gtf differs from TinyFugue
topics: differences, unsupported, command-status, tf-differences
section: Scripting
order: 30
---
# Where gtf differs from TinyFugue

gtf aims to behave like TinyFugue 5.0 beta 8 for scripts, and its screen is meant to look like TinyFugue's when no panels are defined. It is not a copy, and a few things differ. If you find a script that works in TinyFugue and not in gtf, that is a gtf problem: see [[Reporting a problem]].

The repository's `docs/COMMAND-STATUS.md` keeps the command-by-command table; this page is the summary.

## Not available

* `/reboot`: gtf answers "not supported by this client". Use [[Restarting without dropping connections]], which keeps your connections.
* The Lua and Python bridges that some patched TinyFugue builds have: `/loadlua` and `/calllua` answer "not supported by this client", and `/python` does nothing when tried.
* One shared window for all worlds (`virtscreen` off): not implemented. gtf always keeps a window per world.
* MCCP version 1 and MXP. gtf uses MCCP version 2 compression, which is what current games offer — [[Network protocols]].
* `/xtitle` (setting the terminal window title) is not defined.
* Your terminal's termcap entry is not used. gtf reads keys as an xterm sends them; see [[Key bindings]].

## Same name, different behaviour

* `/version` prints gtf's own version, copyright and the engine version. `/ver` and the variable `TFVERSION` still report the TinyFugue release gtf is compatible with, which is what version tests in scripts need.
* `/features` lists TinyFugue's build features and does not match what gtf can do: it shows `-ssl`, `-MCCPv2` and `-IPv6` although gtf connects over TLS, uses MCCP2 and falls back to IPv6 addresses. Use the banner and [[Network protocols]] for what gtf supports.
* A name that resolves to several IPv4 addresses is tried address by address, and IPv6 addresses are used only when there are no IPv4 ones.
* The wording of some messages differs slightly (for example `RECALL` messages), and the numbers `/list` gives to macros differ, because the engine defines a different number of library macros at startup.
* The `@more` field of the status line shows `0+ N` instead of `More N` in one corner case (a wrapped list with the pager off).
* TLS connections do not verify the server's certificate — [[Connecting to a world]].

## What the engine and the client share

The commands are implemented by a scripting engine, gtfparser, which has its own documentation; the client provides sockets, the screen and the keyboard. When a problem is in the language itself (a command that parses differently, a function that returns something else), it is fixed in the engine. You do not need to know which; report it to the gtf project either way.

## Checking a script

The quickest test is to load it: `/load script.tf`. If a command is missing, gtf prints `no such command or macro`. `/help tf <command>` shows how TinyFugue documents that command, and [[Commands and variables index]] lists what gtf adds on top.

See also: [[gtf and TinyFugue]], [[Scripting in gtf]].

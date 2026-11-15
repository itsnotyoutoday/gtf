---
title: First run
topics: first-run, firstrun, getting-started
section: Start here
order: 30
---
# First run

Type `gtf` in a terminal. gtf needs a real terminal (it will not start with its input or output redirected), and a terminal that handles UTF-8 and, ideally, 24-bit colour.

## What you see

A few banner lines (the version, the copyright, the features built in), then lines saying which library files were loaded, and then the screen:

* the **output window**, which fills most of the terminal and shows what the game sends;
* the **status line**, a bar that shows the current world, a clock and other fields;
* the **input window** at the bottom, where you type. It is three lines tall by default; see [[Windows and the status line]].

Nothing is connected yet. gtf does not connect to anything until you ask it to.

## First things to try

```
/help topics          what gtf's own documentation covers
/addworld mymud mud.example.com 4000
/connect mymud
/quit                 leave gtf
```

The next topics explain [[Connecting to a world]] and [[Getting around]].

## What happens at startup

1. gtf loads its library: TinyFugue's standard library files, which define many of the commands you will use, and then gtf's own additions.
2. Unless you gave `-f`, it reads your personal startup file, the first of `~/.tfrc`, `~/tfrc`, `./.tfrc` or `./tfrc` that exists. This is where your own settings and macros belong. See [[Configuration and startup files]].
3. It loads your world file (`~/tiny.world`) if there is one, so your saved worlds are there ([[Worlds and world files]]).
4. It runs any commands given with `-c`.
5. If you named a world (or a host and port) on the command line, and did not give `-n`, it connects.

```
gtf mymud                     connect to a saved world
gtf mud.example.com 4000      connect to a host and port
gtf -n                        start without connecting
```

The full list of options is in [[Command line and environment]].

## Where to read more

`/help` works inside gtf. gtf's own topics come first; if the name you ask for is not one of them, gtf looks in the TinyFugue manual that ships with it and says so. `/help tf <topic>` looks only in that manual. These pages are the same documentation as the gtf wiki.

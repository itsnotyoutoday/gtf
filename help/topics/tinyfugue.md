---
title: gtf and TinyFugue
topics: tinyfugue, tf, tf-compat, compat
section: About
order: 10
---
# gtf and TinyFugue

gtf is a separate client written in Go. It aims to run the TinyFugue language and library: the commands, functions, macros, triggers, hooks, worlds and the standard `.tf` library files. It also has things TinyFugue does not (the [[Squatter]], layouts, graphics panels, the CMCP console protocol), and it differs from TinyFugue in places.

gtf is not affiliated with, made by or endorsed by the TinyFugue authors. If something behaves differently from TinyFugue, that is a gtf issue, not a TinyFugue one: please do not report gtf problems to them.

## How gtf was written

gtf is an independent implementation, written from scratch in Go. It was built to behave like TinyFugue: its output and its error messages were checked against what TinyFugue produces, using TinyFugue's own standard library (`stdlib.tf`) as the baseline. Where gtf and TinyFugue turned out to differ, TinyFugue's source was consulted to understand why.

## The bundled library and manual

gtf ships an unmodified copy of TinyFugue's standard library and its manual (`tf-help`). Those files are copyright Ken Keys and contributors, keep their own notices, and are distributed under the GNU General Public License, as is gtf. The manual describes TinyFugue, so it can be wrong about gtf. When `/help` shows text from it, the first line says so. gtf's own code and help are copyright Rick Bird: `/help copyright` shows gtf's notice first, then TinyFugue's.

## Looking something up

| you type | you get |
|---|---|
| `/help <topic>` | gtf's own topics first, then the TinyFugue manual |
| `/help tf <topic>` | only the TinyFugue manual, for example `/help tf intro` |
| `/help topics` | the list of gtf's topics |

## Differences and status

`docs/COMMAND-STATUS.md` in the repository lists, command by command, what works, what differs and what is not supported by this client.

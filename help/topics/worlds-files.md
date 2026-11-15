---
title: Worlds and world files
topics: gtf-worlds, world-files, worldfile
section: Using gtf
order: 10
---
# Worlds and world files

A world is a name with an address and, optionally, a character and password. gtf keeps the worlds you define in memory; a **world file** is how they survive between sessions.

## Defining and changing worlds

| command | does |
|---|---|
| `/addworld name host port` | define a world (or redefine one with the same name) |
| `/addworld name char pass host port` | the same, with a login |
| `/addworld -x name host port` | a world that uses TLS |
| `/listworlds` | show the defined worlds |
| `/renameworld old new` | give a world another name |
| `/unworld name` | forget a world |

The complete options (world types, a login file, a source address and more) are in the TinyFugue manual: `/help tf /addworld`.

## Saving and loading

```
/saveworld              write every world to the world file
/loadworld              read the world file
```

The world file is `~/tiny.world` unless you redefine the macro `WORLDFILE` (for example `/def WORLDFILE = ~/mine.world` in your startup file). It is a plain text file of `/test addworld(...)` lines and gtf writes it readable by you alone, because it holds passwords in clear text. You may edit it by hand.

**gtf reads the world file at startup**, after your startup file, so the worlds you saved with `/saveworld` are there every time without any setup. (TinyFugue leaves that to a `/loadworld` line in the startup file; a `/loadworld` of your own still works and does no harm.) Worlds your startup file defines come first, so they are the ones gtf connects to when you name no world. `-f` skips the world file along with the startup file.

This step is a macro in gtf's own library, `gtf_startup`, written in the language. To turn it off, put `/undef gtf_startup` in your startup file; to change it, define your own. See [[Configuration and startup files]]. Worlds can also be defined directly in `~/.tfrc` with `/addworld` lines if you prefer one file.

## When a remote client edits worlds

If you use the [[Squatter]] with SquatClient, the app can add, change and remove your worlds. gtf then rewrites the world file the way `/saveworld` would (turn that off with `squatter_world_save=off`). If the file on disk was not written by gtf, for instance because you edited it by hand, gtf first keeps a copy named `tiny.world.gtf-bak-<date>-<time>`, and it keeps the five newest of those.

See also: [[Connecting to a world]], [[Files and folders]].

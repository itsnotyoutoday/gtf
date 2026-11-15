---
title: Configuration and startup files
topics: gtf-config, gtf-configuration, rc-files, tfrc-gtf
section: Using gtf
order: 80
---
# Configuration and startup files

gtf has no settings screen. You configure it with variables and commands, and you keep the ones you want in a startup file that gtf reads every time it starts.

## Variables

A setting is a variable. `/set name=value` changes it at once (no spaces around the `=`), `/set name` shows it, and `/set` alone lists them all. In text and expressions a variable is written `%name`. gtf re-reads its own settings (the `squatter_*` and `cmcp_*` families and the screen and wrapping variables) after every command, so a change takes effect immediately.

```
/set more=on                  pause output a screenful at a time
/set isize=2                  a two-line input window
/set wrap=off                 do not wrap long lines
```

Variables are not saved by themselves. To keep a setting, put its `/set` line in your startup file. `/save file` writes your macros to a file, and `/saveworld` writes your worlds — [[Worlds and world files]].

## Your startup file

When gtf starts (unless you gave `-f`) it loads the first of these that exists:

```
~/.tfrc     ~/tfrc     ./.tfrc     ./tfrc
```

The file is an ordinary script: one command per line, with the same syntax you type at the input line. A typical file:

```
; ~/.tfrc
/set more=on
/set isize=2
/def -t'* tells you *' tellhi = /echo -aBCred >> %{*}
```

`Ctrl-X Ctrl-R` reloads it while gtf runs. gtf does not read a file called `~/.gtfrc`, whatever an install note may say; use `~/.tfrc`.

## Settings for every user of an installed copy

The standard library ends by loading a file called `local.tf` from its own folder (`%TFLIBDIR`, the `tf` folder inside the library), after everything else and before your startup file. It is meant for commands every user of a shared install should get, and it is normally empty. An update or a reinstall replaces the library folder but carries your `local.tf` over (the one in the `tf` folder, and one at the top of the library folder if you have it). Packages (`.deb`, `.rpm`) do not own the file and leave it alone. For your own settings use `~/.tfrc`.

## gtf's own settings folder: ~/.gtf

Things gtf writes itself live in `~/.gtf`: `config.json` (update settings), saved layouts in `layouts/`, console protocol data in `cmcp/`, the squatter's certificate and paired devices in `squatter/`. The full list is in [[Files and folders]].

## Where settings come from, in order

1. the standard library and gtf's own library, loaded first;
2. your startup file;
3. commands given with `-c` on the command line;
4. the world you named, if any, which is connected last.

So a `/set` in `~/.tfrc` can be overridden by a `-c` option, and everything is in place before the connection is made.

See also: [[Command line and environment]], [[gtf variables]], [[Scripting in gtf]].

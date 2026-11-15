---
title: Scripting in gtf
topics: scripting, gtf-scripting, tf-scripts
section: Scripting
order: 10
---
# Scripting in gtf

gtf runs the TinyFugue scripting language. If you have macros, triggers or whole `.tf` files written for TinyFugue, they are the best place to start, and anything you learn from TinyFugue's documentation about the language applies here. This page tells you what that covers and how to try it; [[Where gtf differs from TinyFugue]] lists the exceptions and [[What gtf adds to scripting]] the extras.

## What the language covers

| you want | use | read more |
|---|---|---|
| a command of your own | macros: `/def name = body` | `/help tf macros`, `/help tf /def` |
| react to text from the game | triggers: `/def -t'pattern' name = body` | `/help tf triggers` |
| react to events (connect, disconnect, GMCP ...) | hooks: `/def -hEVENT name = body` | `/help tf hooks` |
| do something later or repeatedly | `/repeat`, `/at`, `/tick` | `/help tf /repeat` |
| remember something | variables: `/set name=value`, used as `%name` | `/help tf variables` |
| calculate | expressions: `/test`, `$[expr]` and the built-in functions | `/help tf expressions`, `/help tf functions` |
| make decisions and loops | `/if`, `/while`, `/for`, `/let` | `/help tf /if` |
| change what you see | `/gag`, `/hilite`, `/substitute`, attributes | `/help tf attributes` |
| match patterns | glob, regular expressions (PCRE-style), simple matching | `/help tf patterns`, `/help tf pcre` |
| talk to the game | `/send`, `/quote`, `/prompt` | `/help tf /send` |
| talk to the system | `/sh`, `/sys`, `/quote !command` | `/help tf /sh` |
| bind keys | `/bind`, `/dokey` | [[Key bindings]] |

The commands behind these are implemented by gtf's scripting engine, [gtfparser](https://github.com/itsnotyoutoday/gtfparser), which gtf shares with other programs. The client supplies the screen, the keyboard and the network.

## A first script

Save this as `hello.tf`:

```
/def greet = /echo Hello, %{1-world}!
/def -t'* tells you *' notify = /echo -aBCred >> %{*}
/def -hCONNECT hi = /echo Connected to %{1}
/set score=5
/def addone = /set score=$[score+1]
```

Run it with `/load hello.tf`. Now `/greet Rick` prints a greeting, any line from the game that matches `* tells you *` is echoed in bold red, every connection announces itself, and `/addone` adds one to `%score`. `/list` shows your macros, `/undef name` removes one, and `/save file` writes your macros to a file you can `/load` later.

## Loading files

* `/load file` runs a script. gtf looks for it in the current folder and in the library folder; `/help tf /load` describes the search rules. `/require file` loads a library file only if it is not loaded already.
* Your startup file is loaded for you: see [[Configuration and startup files]].
* A file may contain anything you can type: one command per line, a trailing backslash continues the line, and `%;` separates several commands on one line.

## The library

gtf ships the standard TinyFugue library, unmodified, and loads `stdlib.tf` at startup. That is where many everyday commands come from (`/addtelnet`, the status line fields, the key bindings). The library folder also holds optional files you can load when you want them, such as `lisp.tf`, `tools.tf`, `textutil.tf` and `tick.tf`; `/help tf library` describes them. The variable `TFLIBDIR` names the folder, and `GTFLIBDIR` names the folder with gtf's own additions — [[Files and folders]].

The library is TinyFugue's, with its authors' copyright and the GNU General Public License. It is shown as such by `/help`: see [[License and credits]].

## Where to get help while scripting

* `/help tf <topic>` looks only in the bundled TinyFugue manual. That is the reference for every command, function and variable of the language.
* `/listdef`, `/listvar`, `/listbind` and `/listhook` show what is defined right now.
* `/help tf debugging` explains how to trace what macros and triggers do.
* [[Commands and variables index]] lists what gtf itself adds.

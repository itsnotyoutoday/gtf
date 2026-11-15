---
title: License and credits
topics: license, credits, licence, authors
section: About
order: 20
---
# License and credits

## gtf

gtf's own code is Copyright (C) 2026 Rick Bird, and is free software: you may use, study, change and share it under the terms of the **GNU General Public License, version 3 or (at your option) any later version**. The full text is the file `LICENSE` that comes with gtf. It comes with no warranty, to the extent the law allows.

## TinyFugue

[TinyFugue](https://tf.tcp.com/~hawkeye/tf/) was written by Ken Keys, with contributors, and is released under the GNU General Public License, version 2 or later. gtf is not TinyFugue, is not made by its authors and is not supported by them. It is inspired by TinyFugue and aims to stay compatible with it: that is where its scripting language, commands, macros, library and many of its key bindings come from, and TinyFugue's authors deserve the credit for the language that so many players and game builders still use.

gtf also ships two things that belong to TinyFugue's authors, unmodified:

* the **standard library** (`stdlib.tf` and the other `.tf` files), which gtf loads at startup;
* the **manual** (`tf-help`), which `/help` consults after gtf's own topics, and always says so when it shows text from it.

Both are Copyright (C) 1993-2007 Ken Keys and contributors and are distributed under the GNU General Public License, version 2 or later, the same family of license as gtf. `/help copyright` shows TinyFugue's own copyright notice. In the source tree the file `gtflib/TF-NOTICE.md` records the same.

## Other people's work in gtf

* **gtfparser**, the scripting engine that runs the language, is a separate project by the same author, shared with other programs. It follows the regular-expression behaviour of the [PCRE2](https://www.pcre.org/) project.
* **The CMCP core**, a shared library that implements the console protocol, is also a separate project; gtf only adds the terminal drawing and the keys.
* **The squatter** is the remote-access server inside gtf that [[Squatter]] describes. It speaks the protocol of the SquatClient apps.
* gtf includes a copy of Go's standard `crypto/tls` package with a small patch that lets a TLS connection be carried across a restart. That code is Copyright The Go Authors and is under the BSD license that comes with Go; its license, notice and patent grant files are kept with it. gtf also uses the Go packages `golang.org/x/crypto`, `golang.org/x/image`, `golang.org/x/sys` and `golang.org/x/term` and the `github.com/coder/websocket` library, under their own licenses.

If you believe something here is credited wrongly or not at all, please say so: [[Reporting a problem]].

See also: [[gtf and TinyFugue]].

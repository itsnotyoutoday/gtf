---
title: Network protocols
topics: gtf-protocols, gtf-gmcp
section: Reference
order: 40
---
# Network protocols

gtf connects over plain TCP, or over TLS when a world has the `x` flag or you use `/connect -x`. Over either it speaks telnet, with these extensions. Games choose what they use; you do not have to configure any of it.

| extension | what gtf does |
|---|---|
| terminal type (TTYPE) and MTTS | answers with `TINYFUGUE` first (games recognise TinyFugue by that name), then your terminal type (for example `XTERM-256COLOR`), then an `MTTS` number listing what your terminal can do (ANSI, VT100, UTF-8, 256 colours, true colour, TLS), then `GTF`. The last answer repeats |
| window size (NAWS) | tells the game your terminal's size when it asks and again whenever you resize the window |
| character set (CHARSET) | accepts UTF-8, otherwise ISO-8859-1, and switches both directions to it. Text is decoded as UTF-8 when that is the character set (bytes that are not valid UTF-8 are read as Latin-1) |
| end-of-record and go-ahead (EOR, GA) | treats the text since the last newline as a prompt, so prompts show where the cursor is |
| suppress go-ahead and echo | when the game says it will echo, gtf hides what you type (a password prompt) |
| MCCP2 compression | decompresses the stream when the game starts it, and goes back to plain text if the compressed stream ends |
| GMCP | on start gtf announces itself and the packages it wants (`Char`, `Room`, `Comm`, `Core`, `Group`, `Client.Media`, and `Cobra` for the console protocol while `%cmcp` is on); scripts and panels read what arrives — [[What gtf adds to scripting]] |
| MSDP | reads variables, lists and tables from the game and lets scripts send `REPORT`, `LIST` and other commands |
| MSSP | reads the game's status report |
| CMCP | consoles inside GMCP — [[CMCP console protocol]] |

Not implemented: MXP, MCCP version 1, ATCP, and any option not listed (gtf refuses them politely, so the game falls back).

## TLS

A world with the `x` flag is connected over TLS 1.2 or 1.3 using the host name you gave. **gtf does not verify the server's certificate.** The connection is encrypted, which protects against eavesdropping, but it does not protect against someone impersonating the game. TLS connections can be carried across a [[Restarting without dropping connections]] (TLS 1.3, and TLS 1.2 with an AEAD cipher).

## Looking at what the game sends

The variables and functions that reveal received data: `gmcp_json("")` is everything GMCP has sent on the world in front, `msdp_get` and `mssp_get` read one value. If a game's data looks wrong, look at that first.

See also: [[Connecting to a world]], [[gtf variables]].

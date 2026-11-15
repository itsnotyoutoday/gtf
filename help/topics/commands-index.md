---
title: Commands and variables index
topics: gtf-commands, gtf-index, command-index
section: Reference
order: 10
---
# Commands and variables index

What gtf itself provides, with a pointer to the topic that explains it. Everything else you type with a `/` (`/def`, `/connect`, `/set`, `/recall` ...) is the TinyFugue language: look it up with `/help <command>` or `/help tf <command>` — [[gtf and TinyFugue]].

## Commands added by gtf

| command | does | topic |
|---|---|---|
| `/panel` | define panels around the output window | [[Panels and layouts]] |
| `/layout` | named sets of panels | [[Panels and layouts]] |
| `/canvas` | drawing surfaces | [[Canvases and sprites]] |
| `/sprite` | vector pictures (experimental) | [[Canvases and sprites]] |
| `/cmcp` | console protocol screens and settings | [[CMCP keys and commands]] |
| `/restart` | replace gtf, keep connections | [[Restarting without dropping connections]] |
| `/update` | check for and install a new version | [[Updating gtf]] |
| `/version` | gtf's version and copyright | [[Versions and update checks]] |
| `/squatter` | the remote-access server | [[Squatter]] |
| `/pair` | pair a device with the squatter | [[Pairing devices]] |

## Macros that come with gtf

| macro | does | topic |
|---|---|---|
| `/sidebar [off]` | show or hide the stock sidebar | [[Panels and layouts]] |
| `/panelhp name HPVAR MAXVAR` | a one-line health gauge on top | [[Panels and layouts]] |
| `/ver` | the TinyFugue release gtf is compatible with | [[What gtf adds to scripting]] |

## Functions added by gtf

| function | topic |
|---|---|
| `gmcp_get`, `gmcp_json`, `gmcp_has`, `gmcp_send` | [[What gtf adds to scripting]] |
| `msdp_get`, `msdp_send`, `mssp_get` | [[What gtf adds to scripting]] |
| `term_width`, `term_height`, `worldfile`, `gtf_startup` | [[What gtf adds to scripting]] |
| `gmcp`, `msdp` (send, as in TinyFugue) | [[What gtf adds to scripting]] |

## Hooks that gtf fires

`GMCP` and `MSDP` — [[What gtf adds to scripting]]; `SQUAT_CONNECT`, `SQUAT_DISCONNECT` and `SQUAT_PAIR` — [[Squatter hooks]]. gtf also fires TinyFugue's ordinary hooks, such as `CONNECT`, `DISCONNECT`, `LOGIN`, `ACTIVITY`, `BGTEXT`, `MORE`, `PROMPT` and `RESIZE`.

## Variables

| variables | topic |
|---|---|
| `squatter`, `squatter_*` | [[Squatter settings]] |
| `cmcp`, `cmcp_*` | [[CMCP settings]] |
| `version`, `TFVERSION`, `TFLIBDIR`, `GTFLIBDIR`, `TFHELP`, `HOME`, `TERM` | [[gtf variables]] |
| `more`, `isize`, `wrap`, `wrapsize`, `visual`, `alert_attr`, `alert_time`, `textdiv`, `textdiv_str` | [[gtf variables]] |

## Options and files

[[Command line and environment]] lists the command-line options and environment variables. [[Files and folders]] says where everything lives. [[Network protocols]] lists the telnet extensions gtf speaks.

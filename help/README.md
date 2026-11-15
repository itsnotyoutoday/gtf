# gtf documentation: help topics and the wiki

Everything a user reads about gtf lives here, once, as Markdown: one file per topic in `topics/`. The client shows the files with `/help`
(embedded in the binary), and `go run ./cmd/helpwiki` turns the same files into the GitHub wiki. Do not edit the wiki by hand; edit these.

## A topic file

```
---
title: Squatter address and port     # the page title, the wiki page name and a /help name
topics: squatter-address, /bind      # more names /help answers to (optional); the file name and the title always work
section: Remote access               # the group in /help topics and the wiki sidebar
order: 30                            # position in the section (then by title)
help: no                             # optional: wiki only (not shown by /help)
wiki: no                             # optional: /help only (not published to the wiki)
---
# Squatter address and port

Text...
```

* The file starts with the front matter above and then Markdown. The first line of the body is the `# Title` heading (the wiki drops it: it
  shows the title itself).
* **Links** between topics are `[[Squatter pairing]]` or `[[how to pair|Squatter pairing]]` (text first, then the target). The target is a
  title, a file name or any `topics:` name. `/help` prints the text followed by `(/help name)`; the wiki gets a real link. A link that does
  not resolve fails the tests (`go run ./cmd/helpwiki -check` checks them too).
* Use only what both renderers handle: `#` / `##` / `###` headings, paragraphs, `*` and `1.` lists, `|` tables, fenced code blocks,
  `> ` quotes, `**bold**`, `` `code` `` and `[text](https://...)` links. No HTML, no images, no italics with `*` or `_`, no nested tables.
  Keep lines in code blocks under about 70 columns: they are not re-wrapped.
* Write for the user, in gtf's own words. gtf is TinyFugue-**compatible**; it is not TinyFugue. Say "gtf", not "TF", for what gtf does, and
  name TinyFugue only when you mean TinyFugue (its language, its library, its manual). Do not copy text from the TinyFugue manual into a topic.
* Add the section to `SectionOrder` in `help.go` if it is new.

## Sections

Start here, Remote access, Using gtf, Scripting, Layouts and graphics, Console protocol, Reference, About.

## Building the wiki

```
go run ./cmd/helpwiki -check            validate topics and links
go run ./cmd/helpwiki -out build/wiki   write the pages into build/wiki (including _Sidebar.md, _Footer.md and Home.md from the wiki-only Home topic)
```

The command only writes files, and it overwrites a page of the same name, so build into a fresh directory and compare with the published wiki before copying. Publishing is a separate step: copy `build/wiki/*` into a clone of `https://github.com/itsnotyoutoday/gtf.wiki.git`,
review the diff, commit and push.

## The bundled TinyFugue manual

`gtflib/tf/tf-help` is an unmodified copy of TinyFugue's manual (see `gtflib/TF-NOTICE.md`). `/help` consults it after these topics and says so.
It is not part of these topics and its text must not be edited or copied here.

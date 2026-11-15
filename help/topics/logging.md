---
title: Logging
topics: gtf-logging, logging
section: Using gtf
order: 40
---
# Logging

`/log` writes text to a file as it arrives, so you can keep a record of a session.

```
/log session.txt             everything (all worlds, gtf too)
/log -w session.txt          only the world in front
/log -wmymud mymud.txt       only the world named mymud
/log -i input.txt            only what you type
/log                         list the logs that are open
/log off                     close all logs
```

A relative file name is relative to the folder gtf was started in (`/pwd` shows it, `/lcd` changes it). Logging starts from the moment you give the command; it does not write out what was already on screen (use `/recall` for that, see [[History and recall]]).

Lines in the log file carry a time stamp. Its format is the variable `log_time_format`, and `wraplog` decides whether long lines are wrapped in the file the way they are on screen. Set them with `/set`; the details are in `/help tf /log`.

A log holds whatever the game printed, including anything you would rather not share, so look through a log before you pass it on.

See also: [[History and recall]].

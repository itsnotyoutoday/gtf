---
title: Several connections
topics: connections, multiple-connections, sockets-gtf
section: Using gtf
order: 20
---
# Several connections

You can be connected to many worlds at once. Each connection is a **socket**, and each world has its own output window with its own history of what it sent.

## Switching

| | |
|---|---|
| `/connect name` | open another connection; it comes to the front when you start it |
| `/connect -b name` | open it in the background |
| `/fg name` | bring a world to the front |
| `/fg -n` | go to "no world" |
| `/bg` (Ctrl-]) | send the current world to the background |
| `/listsockets` | list every socket |
| `/dc` | disconnect the world in front (`/dc name` for another) |
| Esc `{` and Esc `}` | previous and next world |

Text typed in the input line goes to the world in front. The status line shows its name; when you are connected to nothing it shows `(no world)`.

## Text from worlds you are not looking at

Lines from a background world are kept in that world's window. When you come back, gtf puts a divider line in front of the text that arrived while you were away. The status line raises an alert for activity in the background (the alert text, its colour and how long it stays come from `%alert_attr` and `%alert_time`), and clears it when you bring that world to the front. Triggers keep running in every world, in front or not. See [[Scripting in gtf]].

## When a connection ends

A connection that ends (you typed `/dc`, the server closed it, or it never succeeded) leaves a **dead socket**. It stays in `/listsockets`, marked `!`, and the status line shows `!name` while it is in front. Its window keeps its text, so you can read what happened. gtf forgets the dead socket after you have looked at it and moved to another world. If you connect to the same world again you get the old window back with the old text above the new.

A world that was only a temporary `/connect host port` goes away completely with its connection.

## One shared window

gtf always keeps one window per world ("virtual screens", the `virtscreen` variable). The older TinyFugue arrangement of a single shared window is not implemented.

See also: [[Windows and the status line]], [[Restarting without dropping connections]].

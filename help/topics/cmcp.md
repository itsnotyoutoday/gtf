---
title: CMCP console protocol
topics: cmcp, console-protocol, consoles
section: Console protocol
order: 10
---
# CMCP console protocol

CMCP is the client protocol of CobraMUSH, a space-game MUSH. It lets a game describe **consoles** (a helm, sensors, power and damage control, tactical and so on) with gauges, radar, tables and buttons, and send live data for them, all inside the GMCP channel that games already use. The game stays a text game: you can still read and type everything. A client that does not understand CMCP simply ignores it.

gtf is a **cell client**: it draws consoles in the terminal as a grid of coloured text cells, and it lets you operate every control from the keyboard.

## Do I have to do anything?

No. gtf offers CMCP to any game that speaks GMCP, and nothing CMCP is sent before gtf has declared it. A game that does not use CMCP looks exactly as before. To turn it off, `/cmcp off` (for connections opened from then on; it also ends CMCP on the connection in front) or `/set cmcp=off` in your startup file.

## What you see

When you take a console in the game (for example with `man opera`), the output area becomes the console screen:

* the game's own **text pane** stays on screen, is never covered and always has at least 4 rows and 30 columns; on a very small terminal the console steps aside and the text takes the whole area;
* the **status line and input line are untouched**, so you can always type;
* a row at the top shows the screen's title, the ship, whether it is read-only, a key hint and short notices;
* the game's data is shown in widgets: bars, gauges, lists, tables, a radar dish, a schematic of the ship with damaged parts tinted, a viewscreen with a star field and contacts, buttons, toggles and sliders.

Built-in screens cover the usual consoles: operations, power, damage, sensors, tactical and helm. A game can also send its own **layout pack** with extra or different screens, which gtf fetches, verifies and shows once you have allowed it — [[CMCP layout packs]]. Until a pack is usable the built-in screen of the same console is shown.

A screen has a **screen menu** on the left listing the screens your current console may show. `/cmcp screens` lists all screens, including those that another console provides (it tells you which one to `man`).

## Layout

This is a text game first: **the output stays big**. A console screen is a frame with a band of pictures along the top, the framed game output on the left and a column of data and buttons on the right. `%cmcp_theme plain` removes the frame. The output pane is kept at at least 45% of the height or 60% of the width, never under 8 rows; rows that do not fit wrap or stack, and long lists scroll. F3 gives a log view with the output at about 80% of the height, and F4 hides the screen altogether while data keeps flowing. More in [[CMCP keys and commands]] and [[CMCP settings]].

## A click is a command

Pressing a button sends the same command the game would get if you typed it, and gtf shows it as a line like `cmcp> dmg assign 3=Port_Nacelle` in your output. A refused command shows the game's explanation and the command it suggests instead. Buttons that your console may not use are dimmed; on a read-only console (the ship is on automatic, or someone else has the console) nothing is offered.

## Limits

* gtf has **no mouse support** for controls; the keyboard does everything. (When a console screen is up, the mouse wheel scrolls the output and lists; see [[CMCP settings]].)
* gtf plays **no sound** and does not claim to, so a game sends it no sound events. With `%cmcp_bell on` the terminal bell rings for alerts instead.
* The helm has no joystick; its heading, speed, warp, engage, stop, course, dock and undock controls each send an ordinary command.
* After a [[Restarting without dropping connections]] the new process starts the CMCP exchange again with each game, which answers with a full set of data.
* gtf takes a direct connection. Serving CMCP to a remote client through the [[Squatter]] is not implemented.

See also: [[CMCP keys and commands]], [[CMCP settings]], [[CMCP layout packs]].

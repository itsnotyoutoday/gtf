---
title: CMCP settings
topics: cmcp-settings, cmcp-themes, themes, cmcp-variables
section: Console protocol
order: 30
---
# CMCP settings

CMCP settings are ordinary variables: `/set name=value`. Put the ones you want in your startup file — [[Configuration and startup files]]. gtf reads them again as it draws, so most changes show at once.

## Themes

A game can suggest a look for its consoles (a theme), and you can fix your own. `/cmcp themes` lists what gtf has and which one is in use. Which one is used is decided by `%cmcp_theme_mode`:

* `ship` (the default): gtf follows the game, and uses its built-in default look when the game suggests nothing. One theme is used for a whole ship, so it does not flip between consoles.
* `fixed`: keep `%cmcp_theme` whatever the game suggests. (Setting `%cmcp_theme` to a name while leaving the mode unset also fixes the theme.)

A game may announce a theme of its own as a pack; gtf fetches it under the same rules as layout packs: [[CMCP layout packs]]. Writing themes is described in the CMCP protocol documentation, not here.

## The settings

| variable | default | meaning |
|---|---|---|
| `cmcp` | on | offer CMCP to games (applies to new connections) |
| `cmcp_screens` | on | draw console screens; `off` keeps the protocol and the data for scripts and draws nothing |
| `cmcp_theme` | unset | a theme name from the table above |
| `cmcp_theme_mode` | ship | `ship` or `fixed` |
| `cmcp_room_screens` | off | `on` draws a screen in the ship's rooms; off keeps the text view with a coloured alert border; a manned console always draws |
| `cmcp_level` | 3 | the most gtf draws: 3 full console look, 2 plain widgets, 1 text only; the game can cap a screen too and the lower wins |
| `cmcp_layout` | auto | `side` puts the game output on the left and the widgets stacked on the right |
| `cmcp_textport` | balanced | the output's size: `compact` (about 20%), `balanced` (about 35%), `large` (about 50%); F3 expands it to about 80% |
| `cmcp_text_width` | auto | width of the game output in the side layout: `auto` (80 columns) or a number; `/cmcp width` and Alt-, Alt-. change it and remember it in `~/.gtf/cmcp/prefs.json` |
| `cmcp_start_collapsed` | off | start with the screen hidden (like F4) |
| `cmcp_ascii` | auto | draw with ASCII only; `auto` turns it on when the locale is not UTF-8 |
| `cmcp_motion` | on | `off` makes animation instant (and keeps alerts steady) |
| `cmcp_mouse` | on | while a console is up, ask the terminal for mouse reports so the wheel scrolls the output and lists. Your terminal's own text selection then needs Shift (Option in iTerm and Terminal.app) |
| `cmcp_bell` | off | ring the terminal bell for alerts: red and yellow alert, proximity, comm beep, hail |
| `cmcp_alert_recolour` | theme | how much a red alert recolours the screen: `full`, `edge`, `off` |
| `cmcp_alert_pulse` | theme | pulse the red-alert colours (`on`, `off`) |
| `cmcp_alert_ambient` | theme | show the ship's alert edge on every screen aboard, rooms included (`on`, `off`) |
| `cmcp_hit_shake` | theme | jitter the frame when your own ship is hit |
| `cmcp_pack_consent` | always | `always`, `ask` or `never`: see [[CMCP layout packs]] |
| `cmcp_origins` | unset | extra places packs may be fetched from (`https://host[:port]`, space separated); the game's own host is always allowed |
| `cmcp_key_focus`, `cmcp_key_log`, `cmcp_key_hide`, `cmcp_key_close`, `cmcp_key_narrow`, `cmcp_key_widen` | Tab, F3, F4, F6, Alt-, and Alt-. | the keys — [[CMCP keys and commands]] |
| `cmcp_debug` | unset | any text: show protocol diagnostics as `% cmcp:` lines |
| `cmcp_era`, `cmcp_sounds_dir` | tng, unset | stored but unused: gtf plays no sound |

"theme" in the default column means the chosen theme's own value is used.

Two settings are for developers of a game's CMCP support and are not for normal use: `cmcp_dev_loopback_http` and `cmcp_dev_tls`.

## Reduced motion and 16 colours

With `%cmcp_motion off` the log-view animation is instant, red alerts do not pulse and a hit does not shake the frame. On a terminal with fewer than 24-bit colours gtf maps colours to the nearest it has. `%cmcp_ascii on` replaces every drawing character with a plain ASCII one.

See also: [[CMCP console protocol]].

# Command status

tf's commands are implemented by the shared engine; the client supplies the
sockets, screen and keyboard they act on. Status is "client side wired" unless
noted. Real tf is the reference: `scripts/compare_tf.py` runs both side by side
in ptys and diffs the rendered screens (the scenarios cover startup, typing, input
editing keys, wrapping, the input window, `/help`, `/listworlds`, `/limit`, the more
pager, ...).

| command / area | status | notes |
|---|---|---|
| `/connect /dc /fg /listsockets` | working | engine commands over `WorldHost/ConnectHost/FgHost/SocketHost`; real TCP/TLS (`-x`), telnet, MCCP, GMCP/MSDP |
| `/addworld /unworld /renameworld /listworlds /saveworld /loadworld /world_info` | working (engine) | `~/.tfrc` and world files are read like tf (`~/.tfrc`, `~/tfrc`, `./.tfrc`, `./tfrc`; `/loadworld` via WORLDFILE) |
| `/log /recall /recordline /limit /relimit /unlimit` | working (engine) | client feeds `RecordWorldLine`; `/limit` redraws through `LimitHost.Redraw` |
| `/more /visual /scroll`, wrapping (`wrap wrapsize wrapspace wrappunct`), `isize`, `status_height` | working | variables are read from the engine each repaint; wrapping reproduces tf's Unicode line breaking |
| `/status_add /status_rm /status_edit /status_clear /status_defaults` | working (engine) | client draws `StatusRows` (field attributes on values only) |
| `/dokey /bind /unbind /input /kb*()` | working (engine) | keys come from `lib/tf/kbbind.tf` unmodified |
| `/beep /localecho /suspend` | working | `/suspend` stops the process (SIGTSTP) and re-enters raw mode; server `WILL ECHO` hides input via `SetLocalEcho` |
| `/help` | working | `tf-help` from the shared tf library, shown through `HelpHost` |
| `/sh /quote /lcd /load /save /edit /def /hook /trigger ...` | engine | not client specific |
| `/quit /exit /restrict` | working | |
| `/watchdog /watchname` | engine | |
| ACTIVITY / BGTEXT / BGTRIG / PREACTIVITY / MORE / CONNECT / DISCONNECT / CONFAIL / PENDING / LOGIN / PROMPT / WORLD / RESIZE | fired by the client | default messages rendered via `HookDisplayHost` |
| `/panel /layout`, `gmcp_get()` ... | gtf only | see LAYOUTS.md, PROTOCOLS.md |
| `/reboot` | not wired | needs a `RebootHost` |
| `/liststreams /features` | engine | at the time of writing the stdlib prints `% features: no such function` at startup (engine gap) |

## Known visual/behavioural differences from real tf

* Non-ASCII typed input: this build of real tf echoes UTF-8 input as mojibake in the pty
  comparison; gtf shows the characters.
* `HookAlert` messages (status-line alerts) are shown on the foreground screen instead.
* No termcap: the key table is xterm's (the library's `kbbind.tf` also lists the VT220 /
  application-mode sequences).
* The status-line `@more` field shows `   0+ N` instead of `More  N` in one case
  (`compare_tf.py more-off-wrapped-list`); it comes from the stock `status_int_more`
  expression evaluating `/elseif (old)` with `old=0` and is an engine question.
* Differences remaining in `compare_tf.py` that are engine message formats (`% RECALL:` vs
  `% /recall:`) or macro numbers (`/list` numbers differ because the engine defines a different number of
  library macros).

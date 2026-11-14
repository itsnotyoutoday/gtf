# Network protocols

`internal/net` is a self-contained transport + protocol stack. It knows nothing
about the interpreter or the UI.

```go
t := net.TCP{Timeout: 15 * time.Second}            // or net.TLS{Verify: true, ServerName: "..."}
c, _ := net.Dial(ctx, t, "host", "4000", net.Options{Env: env})
for ev := range c.Events() { switch e := ev.(type) {
    case net.Text:         // decoded UTF-8 bytes; may end mid-line
    case net.Prompt:       // IAC GA / IAC EOR: the text since the last newline is a prompt
    case net.OptionChange: // an option turned on/off for a side
    case net.Message:      // structured data from GMCP / MSDP / MSSP
    case net.Subneg:       // a subnegotiation no plugin consumed
    case net.Closed:       // last event
}}
c.SendLine("look"); c.Resize(120, 40); c.SendProto("gmcp", "Core.Ping", nil)
```

* IAC escaping both ways; `Text` is always UTF-8: input is decoded as UTF-8 when the
  session charset is UTF-8 (invalid bytes are read as Latin-1, a multi-byte character
  split across reads is carried over) or as Latin-1; output is encoded the same way.
* Option negotiation follows the RFC 1143 Q method (no loops); unknown options are
  refused (`WONT`/`DONT`).

## Plugins (`internal/net/proto/<name>`)

A protocol is one package implementing `telnet.Handler` and registering itself in
`init()`; adding one is the new package plus one blank-import line in
`internal/net/conn.go`.

| package | option | behaviour |
|---------|--------|-----------|
| `ttype`   | 24  | TERMINAL-TYPE cycle: `TINYFUGUE` (gtf presents itself as TinyFugue first: it is a compatible client and games key on that name), the terminal name (e.g. `XTERM-256COLOR`), `MTTS <bits>`, then `GTF` for servers that read the whole cycle (e.g. `XTERM-256COLOR`), `MTTS <bits>` (ANSI, VT100, UTF-8, 256 colours, truecolor, SSL as available); the last answer repeats. |
| `naws`    | 31  | window size on enable and on every `Conn.Resize` (SIGWINCH). |
| `charset` | 42  | answers `REQUEST` accepting `UTF-8` (else `ISO-8859-1`) and switches the session charset. |
| `eor`     | 25  | negotiates; `IAC EOR` and `IAC GA` become `Prompt` events. |
| `sga`     | 3, 1 | SUPPRESS-GO-AHEAD and ECHO. `WILL ECHO` from the server is reported as `OptionChange{Opt: OptEcho}`; the client reacts by hiding the input line (password mode). |
| `mccp2`   | 86  | `IAC SB MCCP2 IAC SE` switches the inbound stream to zlib mid-stream; when the compressed stream ends the raw stream resumes. |
| `gmcp`    | 201 | `Package.Name <json>` packets -> `Message{Proto:"gmcp"}`. On enable: `Core.Hello` and `Core.Supports.Set` (`Char 1`, `Room 1`, `Comm 1`, `Core 1`, `Group 1`, `Client.Media 1`). `SendProto("gmcp", name, json)`. |
| `msdp`    | 69  | VAR/VAL/TABLE/ARRAY parsing (nested) -> `Message{Proto:"msdp"}` per top-level variable; `SendProto("msdp", "REPORT", []any{"HEALTH","MANA"})` encodes arrays as repeated VALs. |
| `mssp`    | 70  | VAR/VAL pairs -> `Message{Proto:"mssp"}`. |

MXP is not implemented.

## Testing

`internal/net/nettest` pairs a `Conn` with a scripted server on a loopback socket:

```go
rw, srv := nettest.Pair(t)
c := net.NewConn(rw, net.Options{})
srv.Send(nettest.IAC(telnet.DO, telnet.OptNAWS))
srv.Expect(nettest.IAC(telnet.WILL, telnet.OptNAWS), nettest.SB(telnet.OptNAWS, 0, 80, 0, 24))
```

`internal/net/conn_test.go` covers IAC escaping, NAWS (and resize), TTYPE/MTTS cycling,
ECHO on/off, GA/EOR prompts, unhandled subnegotiations, CHARSET, Latin-1/invalid UTF-8,
split multibyte sequences, MCCP2, GMCP (both directions), MSDP/MSSP and close.

## Data in scripts

GMCP/MSDP/MSSP data is stored per session (`internal/client/data`) and exposed to
scripts and layouts:

| | |
|---|---|
| `gmcp_get("Char.Vitals.hp"[, world])` | value as text (arrays indexed from 0: `Room.Info.exits.0`) |
| `gmcp_json("Char.Vitals")`, `gmcp_json("")` | JSON of a package / of everything received |
| `gmcp_has(path)`, `msdp_get(var)`, `mssp_get(var)` | |
| `gmcp_send("Char.Vitals.Get"[, json])`, `msdp_send("REPORT", "HEALTH", "MANA")` | send |
| `gmcp("Package json")`, `msdp("LIST=COMMANDS")` | send, exactly as in real tf (existing scripts keep working) |
| hooks `/def -hGMCP`, `/def -hMSDP` | `GMCP` args: `Package {json}`; `MSDP` args: `VAR=value` (arrays `{ a b }`, tables `[ k=v ]`) |

Divergence from the original brief: `gmcp()`/`msdp()` are real tf functions that *send*,
so the readers have the `_get`/`_json` names.

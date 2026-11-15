---
title: Squatter address and port
topics: squatter-address, squatter-bind, squatter-port
section: Remote access
order: 30
---
# Squatter address and port

By default the squatter listens on every interface (`0.0.0.0`), port 10328. That is what lets SquatClient reach the machine's public address, its Tailscale address or its LAN address with no setup. `/squatter on` lists the addresses it found. Only the machine's own network interfaces are read; nothing is looked up on the internet. A machine behind NAT shows its LAN address: use the router's public address with a port forward, or Tailscale.

| you want | do |
|---|---|
| reachable from anywhere (the default) | nothing |
| only through Tailscale | `/squatter bind <the 100.x Tailscale address>` |
| only on this machine (ssh tunnel or VPN) | `/squatter bind 127.0.0.1` |
| another port | `/squatter port 4444` |
| remember it | `/set squatter_bind=...` and `/set squatter_port=...` in your rc file |

Changes apply at once: the listener restarts, connected clients reconnect and replay, and sessions and paired devices are kept.

With `squatter_tls=off` (plain `ws://`, for ssh tunnels only) the default bind is `127.0.0.1`, and a non-loopback bind is refused.

## Reaching a loopback-only squatter from another machine

```
ssh -L 10328:localhost:10328 user@this-machine
```

then add `127.0.0.1`, port 10328 in SquatClient. See also [[Squatter security]] and [[Squatter settings]].

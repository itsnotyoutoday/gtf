---
title: Squatter troubleshooting
topics: squatter-trouble, squatter-troubleshooting
section: Remote access
order: 100
---
# Squatter troubleshooting

`/squatter` shows the state. Then:

* **State `error` with "address already in use"**: another program uses the port. `/squatter port <other>`, then `/squatter on`.
* **It listens but SquatClient cannot connect**: check the address (`/squatter on` lists them), a firewall or router, and that you use the right port. On a machine behind NAT use Tailscale or a port forward ([[Squatter address and port]]).
* **"plain ws:// is only allowed with a loopback bind"**: `squatter_tls=off` needs `/squatter bind 127.0.0.1`.
* **The fingerprint SquatClient shows is not the one gtf printed**: do not pair; check you reached the right machine ([[Pairing devices]]).
* **The pairing code is rejected**: it works once and expires after `squatter_pair_ttl` seconds; run `/pair` again.
* **SquatClient shows text twice or with stamps in the text**: those lines are from your client's own output; see [[What remote clients see]].
* **Nothing happens at all after a restart**: `/set squatter=on` may be missing from your rc file; `/squatter on` starts it now.

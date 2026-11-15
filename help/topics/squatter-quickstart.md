---
title: Squatter quick start
topics: squatter-quickstart
section: Remote access
order: 20
---
# Squatter quick start

1. `/squatter on`

   gtf creates `~/.gtf/squatter/` (a TLS certificate and the list of paired devices) the first time and starts listening. It prints the address it listens on, for example: `squatter: listening on every interface, port 10328 (public 203.0.113.9, tailscale 100.73.155.123, lan 192.168.1.20). Next: /pair`
2. `/pair`

   Prints a one-time code, valid for 10 minutes, and the TLS fingerprint.
3. In SquatClient add a squatter of type "gtf (GoTinyFugue)": the address from step 1, port 10328 and the code. SquatClient shows the certificate's fingerprint; confirm it matches the one printed in step 2. Later connections need no code.

To start it every time, put `/set squatter=on` in your rc file. To keep it from being reachable from other machines, see [[Squatter address and port]]. Roles and managing devices: [[Pairing devices]].

---
title: Squatter security
topics: squatter-security
section: Remote access
order: 80
---
# Squatter security

* **Reachable from other machines by default.** Anyone who can route to the port can try to connect. They need the TLS handshake and a paired device key, and a pairing code that is valid for ten minutes and works once. To keep it on this machine: `/squatter bind 127.0.0.1` and use an ssh tunnel or a VPN ([[Squatter address and port]]).
* **A device paired as admin can run commands as you.** Pair your own devices only; use `/pair viewer` or `/pair operator` for anyone else, or set `squatter_commands=off` ([[Pairing devices]]).
* **TLS** is on unless `squatter_tls=off`, which is refused for anything but a loopback bind. The certificate is self-signed; trust is by fingerprint. Use `squatter_cert` and `squatter_key` for your own.
* The state directory is mode 0700, keys and device lists 0600. Pairing codes and passwords are never written to a log.
* The process holds your game passwords. Treat the machine like a bastion.

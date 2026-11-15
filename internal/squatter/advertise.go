// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package squatter

import (
	"fmt"
	"net"
	"sort"
	"strings"
)

// Addr is one address of this machine a remote client could try, with what kind it is.
type Addr struct {
	IP   string
	Kind string // "public", "tailscale", "lan", "ipv6"
}

// InterfaceAddrs is where the addresses come from (a variable so tests can supply their own).
var InterfaceAddrs = net.InterfaceAddrs

var cgnat = mustCIDR("100.64.0.0/10") // Tailscale (and other carrier-grade NAT) addresses

func mustCIDR(s string) *net.IPNet {
	_, n, err := net.ParseCIDR(s)
	if err != nil {
		panic(err)
	}
	return n
}

// ReachableAddrs lists this machine's non-loopback unicast addresses, the best first: a public IPv4 address, then a Tailscale one, then
// private LAN addresses, then global IPv6. It only reads the local interfaces (nothing is looked up on the internet).
func ReachableAddrs() []Addr {
	as, err := InterfaceAddrs()
	if err != nil {
		return nil
	}
	var out []Addr
	for _, a := range as {
		ipn, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		ip := ipn.IP
		if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
			continue
		}
		if v4 := ip.To4(); v4 != nil {
			switch {
			case cgnat.Contains(v4):
				out = append(out, Addr{v4.String(), "tailscale"})
			case v4.IsPrivate():
				out = append(out, Addr{v4.String(), "lan"})
			default:
				out = append(out, Addr{v4.String(), "public"})
			}
			continue
		}
		if ip.IsGlobalUnicast() && !ip.IsPrivate() && !(len(ip) == net.IPv6len && ip[0] == 0xfd && ip[1] == 0x7a) { // fd7a: Tailscale's IPv6 range
			out = append(out, Addr{ip.String(), "ipv6"})
		}
	}
	rank := map[string]int{"public": 0, "tailscale": 1, "lan": 2, "ipv6": 3}
	sort.SliceStable(out, func(i, j int) bool { return rank[out[i].Kind] < rank[out[j].Kind] })
	return out
}

// bestAddr is the address to give a remote client when the squatter listens on every interface ("" when the machine has none).
func bestAddr() string {
	if a := ReachableAddrs(); len(a) > 0 {
		return a[0].IP
	}
	return ""
}

func anyBind(b string) bool { return b == "0.0.0.0" || b == "::" || b == "" }

// ListeningNotice is the one notice shown when the squatter starts listening beyond this machine: where it can be reached and what to do next.
// "" for a loopback bind.
func (c Config) ListeningNotice(port string) string {
	if IsLoopback(c.Bind) {
		return ""
	}
	var b strings.Builder
	if anyBind(c.Bind) {
		fmt.Fprintf(&b, "squatter: listening on every interface, port %s", port)
		if as := ReachableAddrs(); len(as) > 0 {
			parts := make([]string, 0, len(as))
			for _, a := range as {
				parts = append(parts, a.Kind+" "+a.IP)
			}
			b.WriteString(" (" + strings.Join(parts, ", ") + ")")
		}
	} else {
		fmt.Fprintf(&b, "squatter: listening on %s:%s", c.Bind, port)
	}
	b.WriteString(". Next: /pair for a pairing code. Connections need TLS and a paired device key; a device paired as admin can run commands in this client, so pair only your own devices. ")
	b.WriteString("To keep it on this machine: /squatter bind 127.0.0.1 (see /help squatter).")
	return b.String()
}

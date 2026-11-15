package squatter

import (
	"net"
	"strings"
	"testing"
)

func ipn(s string) net.Addr {
	ip, n, _ := net.ParseCIDR(s)
	n.IP = ip
	return n
}

func TestReachableAddrsOrderAndKinds(t *testing.T) {
	old := InterfaceAddrs
	defer func() { InterfaceAddrs = old }()
	InterfaceAddrs = func() ([]net.Addr, error) {
		return []net.Addr{ipn("127.0.0.1/8"), ipn("192.168.1.20/24"), ipn("100.73.155.123/32"), ipn("203.0.113.9/24"), ipn("fe80::1/64"),
			ipn("fd7a:115c:a1e0::5001:9bb5/48"), ipn("2001:db8::7/64")}, nil
	}
	as := ReachableAddrs()
	want := []Addr{{"203.0.113.9", "public"}, {"100.73.155.123", "tailscale"}, {"192.168.1.20", "lan"}, {"2001:db8::7", "ipv6"}}
	if len(as) != len(want) {
		t.Fatalf("%+v", as)
	}
	for i := range want {
		if as[i] != want[i] {
			t.Fatalf("%d: %+v want %+v", i, as[i], want[i])
		}
	}
	if bestAddr() != "203.0.113.9" {
		t.Fatal(bestAddr())
	}
	n := DefaultConfig("/h").ListeningNotice("10328")
	if !strings.Contains(n, "public 203.0.113.9") || !strings.Contains(n, "tailscale 100.73.155.123") || !strings.Contains(n, "/pair") {
		t.Fatalf("%q", n)
	}
}

func TestPairURLUsesTheReachableAddressWhenListeningEverywhere(t *testing.T) {
	old := InterfaceAddrs
	defer func() { InterfaceAddrs = old }()
	InterfaceAddrs = func() ([]net.Addr, error) { return []net.Addr{ipn("198.51.100.7/24")}, nil }
	r := newRig(t, func(c *Config) { c.Bind = "0.0.0.0" })
	var info PairInfo
	var err error
	r.h.Do(func() { info, err = r.sq.Pair("") })
	if err != nil || !strings.HasPrefix(info.URL, "wss://198.51.100.7:") {
		t.Fatalf("%q %v", info.URL, err)
	}
}

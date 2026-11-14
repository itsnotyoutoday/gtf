// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package client

import (
	"context"
	"errors"
	"net"
	"strings"
	"syscall"
	"time"
	"unicode"
	"unicode/utf8"

	gnet "github.com/itsnotyoutoday/gtf/internal/net"
	"github.com/itsnotyoutoday/gtf/internal/net/telnet"
)

// lookupWait is how long a name lookup may take before tf says
// "Hostname lookup for X in progress." (tf's CONN_WAIT).
const lookupWait = 400 * time.Millisecond

// Dialer opens a connection to a world. It reports progress through tr (which
// may be nil) the way tf's connection code does, and returns a *DialError when
// the connection cannot be made.
type Dialer interface {
	Dial(ctx context.Context, w World, tr *DialTrace) (*gnet.Conn, error)
}

// DialTrace receives the connection's progress; every field is optional.
type DialTrace struct {
	Lookup func()                 // the name lookup is taking a while
	Try    func(addr string)      // about to connect to "ip port"
	Failed func(addr, why string) // connecting to that address failed, another will be tried
}

func (t *DialTrace) lookup() {
	if t != nil && t.Lookup != nil {
		t.Lookup()
	}
}

func (t *DialTrace) try(a string) {
	if t != nil && t.Try != nil {
		t.Try(a)
	}
}

func (t *DialTrace) failed(a, why string) {
	if t != nil && t.Failed != nil {
		t.Failed(a, why)
	}
}

// DialError says why a connection could not be made.
type DialError struct {
	Lookup bool   // the host name could not be resolved
	Addr   string // "ip port" of the last address tried (not for lookup errors)
	Why    string // strerror-style text, e.g. "Connection refused"
}

func (e *DialError) Error() string {
	if e.Lookup {
		return e.Why
	}
	return e.Addr + ": " + e.Why
}

// NetDialer dials over TCP/TLS.
type NetDialer struct{ Env func() *telnet.Env }

// Dial implements Dialer. Like the reference tf (built without IPv6) it tries
// a name's IPv4 addresses one after the other; IPv6 addresses are used only
// when the name has no IPv4 address.
func (d NetDialer) Dial(ctx context.Context, w World, tr *DialTrace) (*gnet.Conn, error) {
	addrs, err := resolve(ctx, w.Host, tr)
	if err != nil {
		return nil, &DialError{Lookup: true, Why: lookupWhy(err)}
	}
	var tp gnet.Transport = gnet.TCP{Timeout: 15 * time.Second}
	if w.SSL {
		tp = gnet.TLS{Timeout: 15 * time.Second, ServerName: w.Host}
	}
	env := telnet.NewEnv()
	if d.Env != nil {
		env = d.Env()
	}
	var last *DialError
	for i, ip := range addrs {
		addr := ip + " " + w.Port
		tr.try(addr)
		conn, err := gnet.Dial(ctx, tp, ip, w.Port, gnet.Options{Env: env})
		if err == nil {
			return conn, nil
		}
		last = &DialError{Addr: addr, Why: strerror(err)}
		if i+1 < len(addrs) {
			tr.failed(addr, last.Why)
		}
	}
	return nil, last
}

// resolve returns the addresses of host (a literal address resolves to itself).
func resolve(ctx context.Context, host string, tr *DialTrace) ([]string, error) {
	if ip := net.ParseIP(host); ip != nil {
		return []string{ip.String()}, nil
	}
	type result struct {
		ips []net.IPAddr
		err error
	}
	ch := make(chan result, 1)
	go func() {
		ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		ch <- result{ips, err}
	}()
	var r result
	select {
	case r = <-ch:
	case <-time.After(lookupWait):
		tr.lookup()
		r = <-ch
	}
	if r.err != nil {
		return nil, r.err
	}
	var v4, v6 []string
	for _, a := range r.ips {
		s := a.IP.String()
		list := &v6
		if a.IP.To4() != nil {
			list = &v4
		}
		dup := false
		for _, x := range *list {
			dup = dup || x == s
		}
		if !dup {
			*list = append(*list, s)
		}
	}
	if len(v4) > 0 {
		return v4, nil
	}
	if len(v6) == 0 {
		return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
	}
	return v6, nil
}

// lookupWhy is tf's wording (hstrerror) for a failed name lookup.
func lookupWhy(err error) string {
	var de *net.DNSError
	if errors.As(err, &de) {
		switch {
		case de.IsNotFound:
			return "Unknown host"
		case de.IsTimeout || de.IsTemporary:
			return "Host name lookup failure"
		}
	}
	return capitalize(err.Error())
}

// strerror renders a connection error the way libc's strerror does.
func strerror(err error) string {
	var en syscall.Errno
	if errors.As(err, &en) {
		return capitalize(en.Error())
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "Operation timed out"
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return "Operation timed out"
	}
	msg := err.Error()
	if i := strings.LastIndex(msg, ": "); i >= 0 {
		msg = msg[i+2:]
	}
	return capitalize(msg)
}

func capitalize(s string) string {
	r, n := utf8.DecodeRuneInString(s)
	if n == 0 {
		return s
	}
	return string(unicode.ToUpper(r)) + s[n:]
}

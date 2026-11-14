package client

import (
	"strings"

	"github.com/itsnotyoutoday/gtf/internal/layout"
	"github.com/itsnotyoutoday/gtfparser"
)

type (
	cmdf = func(*gtfparser.Interp, string) gtfparser.Value
	fnf  = func(*gtfparser.Interp, []gtfparser.Value) (gtfparser.Value, error)
)

// install registers the gtf-only commands and functions. Everything that is
// part of tf itself (connections, history, limits, status, keys, ...) is the
// engine's; only panels/layouts and the protocol-data functions are added
// here, through the engine's public registration points.
func (c *Client) install() {
	in := c.in
	for n, f := range map[string]cmdf{"panel": c.cmdPanel, "layout": c.cmdLayout} {
		in.RegisterCommand(n, f)
	}
	funcs := map[string]fnf{
		// TODO(core): these tf functions belong in the engine (limit() is used by the
		// stock @more status field); remove when it provides them.
		"limit": func(*gtfparser.Interp, []gtfparser.Value) (gtfparser.Value, error) { return b2i(c.in.LimitActive()), nil },
		"nread": func(*gtfparser.Interp, []gtfparser.Value) (gtfparser.Value, error) { return gtfparser.Int(0), nil },
		"nmail": func(*gtfparser.Interp, []gtfparser.Value) (gtfparser.Value, error) { return gtfparser.Int(0), nil },
		// gmcp()/msdp() keep real tf's meaning (send); the readers are new names.
		"gmcp": c.fnGmcpSend, "msdp": c.fnMsdpSend,
		"gmcp_get": c.fnGmcpGet, "gmcp_json": c.fnGmcpJSON, "gmcp_has": c.fnGmcpHas,
		"msdp_get": c.fnMsdpGet, "mssp_get": c.fnMsspGet,
		"gmcp_send": c.fnGmcpSend2, "msdp_send": c.fnMsdpSend2,
	}
	for n, f := range funcs {
		in.RegisterFunction(n, f)
	}
}

func arg(a []gtfparser.Value, i int) string {
	if i < len(a) {
		return a[i].String()
	}
	return ""
}

func b2i(b bool) gtfparser.Value {
	if b {
		return gtfparser.Int(1)
	}
	return gtfparser.Int(0)
}

func (c *Client) sessionOf(world string) *Session { return c.pick(world) }

// gmcp("Package json" [, world]) sends, as in real tf.
func (c *Client) fnGmcpSend(_ *gtfparser.Interp, a []gtfparser.Value) (gtfparser.Value, error) {
	s := c.sessionOf(arg(a, 1))
	if s == nil || s.conn == nil {
		return gtfparser.Int(0), nil
	}
	name, js := splitFirstWord(arg(a, 0))
	return b2i(s.conn.SendProto("gmcp", name, js) == nil), nil
}

// msdp("VAR=value" [, world]) sends, as in real tf (simple "VAR=val" or
// "VAR={ a b }" forms).
func (c *Client) fnMsdpSend(_ *gtfparser.Interp, a []gtfparser.Value) (gtfparser.Value, error) {
	s := c.sessionOf(arg(a, 1))
	if s == nil || s.conn == nil {
		return gtfparser.Int(0), nil
	}
	txt := arg(a, 0)
	name, val, _ := strings.Cut(txt, "=")
	var v any = strings.TrimSpace(val)
	if t := strings.TrimSpace(val); strings.HasPrefix(t, "{") && strings.HasSuffix(t, "}") {
		var list []any
		for _, w := range strings.Fields(t[1 : len(t)-1]) {
			list = append(list, w)
		}
		v = list
	}
	return b2i(s.conn.SendProto("msdp", strings.TrimSpace(name), v) == nil), nil
}

func splitFirstWord(s string) (string, string) {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, " \t"); i >= 0 {
		return s[:i], strings.TrimSpace(s[i+1:])
	}
	return s, ""
}

func (c *Client) fnGmcpSend2(in *gtfparser.Interp, a []gtfparser.Value) (gtfparser.Value, error) {
	s := c.sessionOf(arg(a, 2))
	if s == nil || s.conn == nil {
		return gtfparser.Int(0), nil
	}
	var v any
	if len(a) > 1 && a[1].String() != "" {
		v = a[1].String()
	}
	return b2i(s.conn.SendProto("gmcp", arg(a, 0), v) == nil), nil
}

func (c *Client) fnMsdpSend2(in *gtfparser.Interp, a []gtfparser.Value) (gtfparser.Value, error) {
	s := c.sessionOf("")
	if s == nil || s.conn == nil {
		return gtfparser.Int(0), nil
	}
	var v any
	if len(a) == 2 {
		v = a[1].String()
	} else if len(a) > 2 {
		var l []any
		for _, x := range a[1:] {
			l = append(l, x.String())
		}
		v = l
	}
	return b2i(s.conn.SendProto("msdp", arg(a, 0), v) == nil), nil
}

func (c *Client) lookupData(scheme string, a []gtfparser.Value) (any, bool) {
	s := c.sessionOf(arg(a, 1))
	if s == nil {
		return nil, false
	}
	return s.store.Resolve(scheme + ":" + arg(a, 0))
}

func (c *Client) fnGmcpGet(_ *gtfparser.Interp, a []gtfparser.Value) (gtfparser.Value, error) {
	v, _ := c.lookupData("gmcp", a)
	return gtfparser.Str(layout.ToString(v)), nil
}
func (c *Client) fnGmcpHas(_ *gtfparser.Interp, a []gtfparser.Value) (gtfparser.Value, error) {
	_, ok := c.lookupData("gmcp", a)
	return b2i(ok), nil
}
func (c *Client) fnGmcpJSON(_ *gtfparser.Interp, a []gtfparser.Value) (gtfparser.Value, error) {
	s := c.sessionOf(arg(a, 1))
	if s == nil {
		return gtfparser.Str(""), nil
	}
	return gtfparser.Str(s.store.JSON("gmcp:" + arg(a, 0))), nil
}
func (c *Client) fnMsdpGet(_ *gtfparser.Interp, a []gtfparser.Value) (gtfparser.Value, error) {
	v, _ := c.lookupData("msdp", a)
	return gtfparser.Str(flatten(v)), nil
}
func (c *Client) fnMsspGet(_ *gtfparser.Interp, a []gtfparser.Value) (gtfparser.Value, error) {
	v, _ := c.lookupData("mssp", a)
	return gtfparser.Str(flatten(v)), nil
}

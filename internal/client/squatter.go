// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package client

// squatter.go connects internal/squatter (the squatter daemon and tf bridge that SquatClient
// pairs with) to the client: it reads the squatter_* variables, implements squatter.Host on
// top of the client's Sessions, mirrors every world screen to the squatter through
// ui.Screen.Tap, and defines /squatter and /pair. Everything here runs on the engine
// goroutine, except Host.Do/Post which are the doors the network goroutines use.

import (
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/itsnotyoutoday/gtf/internal/layout"
	"github.com/itsnotyoutoday/gtf/internal/squatter"
	"github.com/itsnotyoutoday/gtf/internal/squatter/proto"
	"github.com/itsnotyoutoday/gtf/internal/ui"
	"github.com/itsnotyoutoday/gtfparser"
)

// squatQueue holds the work the squatter's network goroutines have asked the engine goroutine to do
// (Host.Do and Host.Post). It is run by tasks posted to the engine, and by /restart's quiesce, which
// occupies the engine goroutine while a client's request may be waiting for it.
type squatQueue struct {
	mu sync.Mutex
	q  []func()
}

func (q *squatQueue) push(f func()) {
	q.mu.Lock()
	q.q = append(q.q, f)
	q.mu.Unlock()
}

// drain runs what is queued, in order, on the calling goroutine (the engine goroutine).
func (q *squatQueue) drain() {
	for {
		q.mu.Lock()
		if len(q.q) == 0 {
			q.mu.Unlock()
			return
		}
		f := q.q[0]
		q.q[0] = nil
		q.q = q.q[1:]
		q.mu.Unlock()
		f()
	}
}

type squatState struct {
	q        squatQueue
	quiesced bool // a /restart has quiesced the squatter's connections and not yet resumed or exec'd
	c        *Client
	sq       *squatter.Squatter
	sig      string
	warned   map[string]bool
	lastView string // the foreground session last reported to the squatter
	worldSum string // content last written to the world file by a peer's edit (see saveWorlds)
}

func newSquatState(c *Client) *squatState {
	st := &squatState{c: c, warned: map[string]bool{}}
	st.sq = squatter.New(&squatHost{c: c}, squatter.Options{Version: strings.TrimPrefix(Version, "v"), Home: c.cfg.HomeDir, TLS: squatTLS{}})
	return st
}

func (st *squatState) stop() { st.sq.Stop() }

func (st *squatState) active() bool { return st.sq.Active() }

// squatCheck applies the squatter_* variables when any of them changed (and once at startup). It
// is cheap: a dozen map lookups.
func (c *Client) squatCheck() {
	if c.sq == nil || c.closed.Load() {
		return
	}
	c.squatLocalView()
	var b strings.Builder
	for _, n := range squatter.ConfigVars {
		b.WriteString(c.in.Var(n))
		b.WriteByte(0)
	}
	sig := b.String()
	if sig == c.sq.sig {
		return
	}
	c.sq.sig = sig
	cfg, warns := squatter.ConfigFromVars(c.in.Var, c.cfg.HomeDir)
	for _, w := range warns {
		if !c.sq.warned[w] {
			c.sq.warned[w] = true
			c.privateLine("% squatter: " + w)
		}
	}
	c.sq.sq.Apply(cfg, c.squatSeed)
}

// squatSeed registers the connections that exist when the squatter is switched on, with the text
// their screens still hold.
func (c *Client) squatSeed() {
	for _, s := range c.order {
		if s.state == gtfparser.SockResolving || s.dead() && s.state == gtfparser.SockDead {
			continue
		}
		c.squatOpen(s)
		if s.squatID == "" {
			continue
		}
		sb := s.scr.SB
		for i := max(0, sb.Logical()-2000); i < sb.Logical(); i++ {
			c.sq.sq.Line(s.squatID, proto.KindGame, cellsToSpans(sb.Cells(i)), false, "")
		}
		if s.open() {
			c.sq.sq.SessionConnected(s.squatID)
		}
		if s.dead() {
			c.squatEnded(s)
		}
	}
}

func (c *Client) squatOpen(s *Session) {
	if !c.sq.active() {
		return
	}
	port, _ := strconv.Atoi(s.World.Port)
	s.squatID = c.sq.sq.SessionOpened(squatter.SessionMeta{World: s.World.Name, Host: s.World.Host, Port: port, TLS: s.World.SSL,
		StartedAt: c.cfg.Now().UnixMilli()})
	c.lastSquatID = s.squatID
}

func (c *Client) squatConnected(s *Session) {
	if s.squatID != "" {
		c.sq.sq.SessionConnected(s.squatID)
	}
}

func (c *Client) squatEnded(s *Session) {
	if s.squatID == "" {
		return
	}
	r := s.endReason
	if r == "" {
		r = "client_quit"
	}
	c.sq.sq.SessionEnded(s.squatID, r, s.endDetail)
}

func (st *squatState) setRemoteEcho(s *Session, on bool) {
	if s.squatID != "" {
		st.sq.SetRemoteEcho(s.squatID, on)
	}
}

func (st *squatState) gmcp(s *Session, name, raw string) {
	if s.squatID != "" {
		st.sq.Gmcp(s.squatID, name, raw)
	}
}

func (st *squatState) msdp(s *Session, name string, v any) {
	if s.squatID != "" {
		st.sq.Msdp(s.squatID, name, v)
	}
}

// squatTap mirrors a line shown on a world's screen to the squatter.
func (c *Client) squatTap(s *Session, cells []layout.Cell) {
	if s.squatID == "" || c.noTap {
		return
	}
	kind := proto.KindSystem
	if c.tapGame {
		kind = proto.KindGame
	}
	c.sq.sq.Line(s.squatID, kind, cellsToSpans(cells), false, "")
}

// squatPrompt mirrors a prompt (a partial line flushed by GA/EOR/timeout).
func (c *Client) squatPrompt(s *Session, cells []layout.Cell) {
	if s.squatID != "" && len(cells) > 0 {
		c.sq.sq.Line(s.squatID, proto.KindGame, cellsToSpans(cells), true, "")
	}
}

// squatLocalEcho mirrors a line the local side sent to the game (typed, or sent by a script);
// lines sent by a remote client are echoed by the squatter itself.
func (c *Client) squatLocalEcho(s *Session, text string) {
	if s.squatID == "" || c.remoteSend {
		return
	}
	c.sq.sq.Line(s.squatID, proto.KindEcho, []proto.Span{{S: text}}, false, "tf")
}

// addLine puts a line on a screen. While a remote tf.exec captures output it is diverted into
// the capture instead and addLine returns true (nothing is displayed or recorded).
func (c *Client) addLine(scr *ui.Screen, cells []layout.Cell) bool {
	if c.capture != nil {
		*c.capture = append(*c.capture, ui.StripCells(cells))
		return true
	}
	c.ui.AddLine(scr, cells)
	return false
}

// privateLine shows text to whoever ran the command, without recording it in history, the log or
// the squatter's scrollback (a pairing code is a secret).
func (c *Client) privateLine(text string) {
	if c.capture != nil {
		*c.capture = append(*c.capture, text)
		return
	}
	c.noTap = true
	defer func() { c.noTap = false }()
	c.ui.AddLine(c.screenFor(), ui.PlainCells(text, layout.Attr{}))
	c.scheduleRender()
}

func colorOfCell(k layout.Color) *proto.Color {
	var out proto.Color
	switch k.Kind {
	case layout.ANSI, layout.Indexed:
		out = proto.Palette(k.N)
	case layout.RGB:
		out = proto.RGB(k.R, k.G, k.B)
	default:
		return nil
	}
	return &out
}

// cellsToSpans converts display cells to wire spans (adjacent cells of the same style merge).
func cellsToSpans(cells []layout.Cell) []proto.Span {
	var out []proto.Span
	for _, cl := range cells {
		if cl.R == 0 {
			continue
		}
		sp := proto.Span{S: string(cl.R), Fg: colorOfCell(cl.A.FG), Bg: colorOfCell(cl.A.BG), B: cl.A.Bold, Dim: cl.A.Dim,
			I: cl.A.Italic, U: cl.A.Underline, Rev: cl.A.Reverse}
		if n := len(out); n > 0 && out[n-1].SameStyle(sp) {
			out[n-1].S += sp.S
		} else {
			out = append(out, sp)
		}
	}
	return out
}

// ---------------------------------------------------------------- squatter.Host

type squatHost struct{ c *Client }

func (h *squatHost) Do(f func()) {
	c := h.c
	d := make(chan struct{})
	c.sq.q.push(func() { f(); close(d) })
	c.Post(func(*gtfparser.Interp) { c.sq.q.drain() })
	select {
	case <-d:
	case <-c.done:
	}
}

func (h *squatHost) Post(f func()) {
	c := h.c
	c.sq.q.push(func() { f(); c.afterTask() })
	c.Post(func(*gtfparser.Interp) { c.sq.q.drain() })
}

func (c *Client) squatSession(id string) *Session {
	for _, s := range c.order {
		if s.squatID == id {
			return s
		}
	}
	return nil
}

func (h *squatHost) SendLine(id, text string, o squatter.SendOpts) error {
	c := h.c
	s := c.squatSession(id)
	if s == nil || !s.open() {
		return fmt.Errorf("not connected")
	}
	c.remoteSend = true
	defer func() { c.remoteSend = false }()
	if o.Command {
		// like a line typed in that world: the world is the context, the output lands on its screen
		origin := c.peerOrigin(o.Origin)
		c.withWorld(s, func() { c.in.DoAs(origin, func() { c.in.ExecFull(text) }) })
		c.afterTask()
		return nil
	}
	return s.sendLine(text)
}

func (h *squatHost) Connect(t squatter.ConnectTarget) (string, error) {
	c := h.c
	line := "/connect -b "
	switch {
	case t.Ref != "":
		if !safeWord(t.Ref) {
			return "", fmt.Errorf("bad world name")
		}
		line += t.Ref
	default:
		if !safeWord(t.Host) {
			return "", fmt.Errorf("bad host name")
		}
		if name := c.adhocWorldName(t); name != "" {
			// honour the name the peer sent: a temporary world of that name (dropped when the
			// connection ends, like "/connect host port" makes)
			c.in.DefineWorld(gtfparser.WorldInfo{Name: name, Host: t.Host, Port: strconv.Itoa(t.Port), SSL: t.TLS, Temp: true})
			line += name
			break
		}
		if t.TLS {
			line = "/connect -b -x "
		}
		line += t.Host + " " + strconv.Itoa(t.Port)
	}
	c.lastSquatID = ""
	var out []string
	c.capture, c.captureWarn = &out, false
	c.in.ExecFull(line)
	c.capture = nil
	ok := !c.captureWarn
	c.afterTask()
	if !ok || c.lastSquatID == "" {
		if len(out) == 0 {
			out = []string{"the connection could not be started"}
		}
		return "", fmt.Errorf("%s", strings.TrimPrefix(out[len(out)-1], "% "))
	}
	return c.lastSquatID, nil
}

// adhocWorldName turns the name a peer gave an ad hoc connection into a world name that is free
// ("" = none usable: fall back to the engine's "(unnamedN)").
func (c *Client) adhocWorldName(t squatter.ConnectTarget) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(t.Name) {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("-_.:[]", r) {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	base := b.String()
	if len(base) > 48 {
		base = base[:48]
	}
	if base == "" || strings.Trim(base, "_") == "" || strings.EqualFold(base, "default") || base[0] == '/' {
		return ""
	}
	name := base
	for i := 2; ; i++ {
		if _, taken := c.in.World(name); !taken {
			return name
		}
		name = fmt.Sprintf("%s-%d", base, i)
	}
}

// safeWord allows only what can be a world name or host name inside a command line.
func safeWord(s string) bool {
	if s == "" || len(s) > 255 {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("-_.:[]", r)) {
			return false
		}
	}
	return true
}

func (h *squatHost) Disconnect(id string) error {
	c := h.c
	s := c.squatSession(id)
	if s == nil || s.dead() {
		return fmt.Errorf("not connected")
	}
	err := c.Disconnect(s.World.Name)
	c.afterTask()
	return err
}

func (h *squatHost) SendGMCP(id, pkg, data string) error {
	s := h.c.squatSession(id)
	if s == nil || s.conn == nil || !s.open() {
		return fmt.Errorf("not connected")
	}
	var v any
	if data != "" {
		v = data
	}
	if err := s.conn.SendProto("gmcp", pkg, v); err != nil {
		return fmt.Errorf("the game did not negotiate GMCP")
	}
	return nil
}

func (h *squatHost) SendMSDP(id, name string, value any) error {
	s := h.c.squatSession(id)
	if s == nil || s.conn == nil || !s.open() {
		return fmt.Errorf("not connected")
	}
	if err := s.conn.SendProto("msdp", name, value); err != nil {
		return fmt.Errorf("the game did not negotiate MSDP")
	}
	return nil
}

func (h *squatHost) Exec(line, device string) (bool, []string) {
	c := h.c
	out := []string{}
	c.capture, c.captureWarn = &out, false
	origin := c.peerOrigin(device)
	c.in.DoAs(origin, func() { c.in.ExecFull(line) })
	c.capture = nil
	ok := !c.captureWarn
	c.afterTask()
	return ok, out
}

// peerOrigin is the engine origin of a paired device's code and sets its restriction level from
// squatter_restrict (none | shell | file | world; default file: no shell and no file access). Everything the
// peer runs, and every macro, trigger, hook or timer it defines, carries this origin and stays restricted
// when it fires later; the owner at the terminal is never restricted. The level is applied before the code
// runs (an origin with no level would be unrestricted).
func (c *Client) peerOrigin(device string) string {
	origin := "peer:" + device
	level := restrictLevel(c.in.Var("squatter_restrict"))
	if c.in.OriginRestriction(origin) != level {
		c.in.SetOriginRestriction(origin, level)
	}
	return origin
}

// restrictLevel maps the squatter_restrict setting to the engine /restrict levels; an unknown or empty
// value is the safe default (file).
func restrictLevel(v string) int {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "none", "off", "0":
		return gtfparser.RestrictNone
	case "shell", "1":
		return gtfparser.RestrictShell
	case "world", "3":
		return gtfparser.RestrictWorld
	}
	return gtfparser.RestrictFile
}

func (h *squatHost) Worlds() []proto.TFWorld {
	var out []proto.TFWorld
	for _, w := range h.c.in.Worlds() {
		out = append(out, tfWorld(w))
	}
	return out
}

// tfWorld is the wire form of a world: never the password (PassSet only) nor the macro file.
func tfWorld(w gtfparser.WorldInfo) proto.TFWorld {
	return proto.TFWorld{Name: w.Name, Host: w.Host, Port: w.Port, Type: w.Type, Char: w.Character,
		PassSet: w.Password != "", TLS: w.SSL, Echo: w.Echo, Charset: w.Charset, Temp: w.Temp, Rev: w.Rev}
}

func (h *squatHost) ApplyNAWS(id string, cols int) {
	if s := h.c.squatSession(id); s != nil && s.conn != nil && s.open() {
		s.resize(cols, h.c.ui.H)
	}
}

func (h *squatHost) Hook(event string, args ...string) { h.c.in.FireHookArgs(event, args...) }

func (h *squatHost) SetVars(vars map[string]string) {
	for k, v := range vars {
		h.c.in.SetVar(k, v)
	}
}

// Notice shows a line to the local user only: what the squatter says (state, paths, device names,
// audit lines) is never added to a session's scrollback, which every paired device can replay.
func (h *squatHost) Notice(text string) { h.c.privateLine(text) }

// ---------------------------------------------------------------- commands

const squatUsage = `usage: /squatter [status | on | off | list | who | kick <name> | unpair <name> | fingerprint | port <n> | bind <addr> | name <name> | pair [role]]
  /squatter       show the state, listen address and fingerprint
  on, off         set %squatter (the daemon starts and stops at once)
  list            connected clients, paired devices and the sessions held
  who             who is attached, which world each views, who is typing
  kick <name>     disconnect a client by device name (the whole rest of the line) or id prefix
  unpair <name>   forget a paired device (name or id prefix of 4+ characters) and disconnect it
  fingerprint     the TLS fingerprint a client pins when it pairs
  port, bind, name  set %squatter_port, %squatter_bind, %squatter_name (applies at once)
  pair [role]     same as /pair
Settings are ordinary variables: %squatter %squatter_port %squatter_bind %squatter_name %squatter_dir
%squatter_kind %squatter_tls %squatter_cert %squatter_key %squatter_scrollback %squatter_max_clients
%squatter_max_sessions %squatter_pair_ttl %squatter_pair_role %squatter_commands %squatter_restrict %squatter_gtf %squatter_naws
%squatter_world_save (see docs/SQUATTER.md).`

// out prints the squatter's own output privately (see privateLine): it names the fingerprint, the
// state directory and the paired devices, so it must not enter any scrollback.
func (c *Client) out(lines ...string) {
	for _, l := range lines {
		c.privateLine(l)
	}
}

func (c *Client) cmdSquatter(in *gtfparser.Interp, args string) gtfparser.Value {
	f := strings.Fields(args)
	sub := ""
	rest := "" // the rest of the line after the subcommand (device names contain spaces)
	if len(f) > 0 {
		sub = strings.ToLower(f[0])
		f = f[1:]
		rest = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(args), strings.Fields(args)[0]))
	}
	sq := c.sq.sq
	set := func(name, val string) gtfparser.Value {
		in.SetVar(name, val)
		c.sq.sq.Retry() // "/squatter on" after an error tries again
		c.sq.sig = ""   // re-read the variables even if none changed
		c.squatCheck()
		c.out(sq.StatusLines()...)
		return gtfparser.Int(1)
	}
	switch sub {
	case "", "status":
		c.out(sq.StatusLines()...)
		if st, _ := sq.State(); st == squatter.StateOff {
			c.out("  (set squatter=on, for example in your rc file, to start it)")
		}
		return gtfparser.Int(1)
	case "on", "off":
		return set(squatter.VarEnabled, sub)
	case "port":
		if len(f) != 1 {
			c.out("% usage: /squatter port <number>")
			return gtfparser.Int(0)
		}
		if n, err := strconv.Atoi(f[0]); err != nil || n < 0 || n > 65535 {
			c.out("% /squatter: " + f[0] + " is not a port number")
			return gtfparser.Int(0)
		}
		return set(squatter.VarPort, f[0])
	case "bind", "name":
		if len(f) != 1 {
			c.out("% usage: /squatter " + sub + " <value>")
			return gtfparser.Int(0)
		}
		return set("squatter_"+sub, f[0])
	case "fingerprint":
		fp := sq.Fingerprint()
		if fp == "" {
			c.out("% /squatter: no TLS fingerprint (the squatter is off, or runs without TLS)")
			return gtfparser.Int(0)
		}
		c.out("TLS fingerprint: " + fp)
		return gtfparser.Int(1)
	case "kick":
		if rest == "" {
			c.out("% usage: /squatter kick <device name or id prefix>")
			return gtfparser.Int(0)
		}
		n, err := sq.Kick(rest)
		if err != nil {
			c.out("% /squatter: " + err.Error())
			return gtfparser.Int(0)
		}
		c.out(fmt.Sprintf("%% squatter: %d client(s) disconnected", n))
		return gtfparser.Int(int64(n))
	case "unpair":
		if rest == "" {
			c.out("% usage: /squatter unpair <device name or id prefix>")
			return gtfparser.Int(0)
		}
		names, err := sq.Unpair(rest)
		if err != nil {
			c.out("% /squatter: " + err.Error())
			return gtfparser.Int(0)
		}
		if len(names) == 0 {
			c.out("% /squatter: no paired device " + rest)
			return gtfparser.Int(0)
		}
		c.out("% squatter: unpaired " + strings.Join(names, ", "))
		return gtfparser.Int(int64(len(names)))
	case "list":
		c.squatList()
		return gtfparser.Int(1)
	case "who":
		c.squatWho()
		return gtfparser.Int(1)
	case "pair":
		return c.cmdPair(in, strings.Join(f, " "))
	case "help", "?":
		c.out(strings.Split(squatUsage, "\n")...)
		return gtfparser.Int(1)
	}
	c.out("% /squatter: unknown subcommand " + sub)
	c.out(strings.Split(squatUsage, "\n")[0])
	return gtfparser.Int(0)
}

func (c *Client) squatList() {
	sq := c.sq.sq
	c.out(sq.StatusLines()...)
	peers := sq.Peers()
	c.out(fmt.Sprintf("Connected clients (%d):", len(peers)))
	for _, p := range peers {
		c.out(fmt.Sprintf("  %-20s %-9s %s  since %s", p.Name, p.Role, p.DeviceID[:min(8, len(p.DeviceID))], p.Since.Format("15:04:05")))
	}
	if devs, err := sq.Devices(); err == nil {
		c.out(fmt.Sprintf("Paired devices (%d):", len(devs)))
		for _, d := range devs {
			on := ""
			if d.Online {
				on = " (connected)"
			}
			seen := "never"
			if d.LastSeen.UnixMilli() > 0 {
				seen = d.LastSeen.Format("2006-01-02 15:04")
			}
			c.out(fmt.Sprintf("  %-20s %-9s %s  last seen %s%s", d.Name, d.Role, d.DeviceID[:min(8, len(d.DeviceID))], seen, on))
		}
	}
	c.out(fmt.Sprintf("Sessions held: %d", sq.NumSessions()))
}

// squatWho shows who is attached (peers and this terminal), what each views and who is typing.
func (c *Client) squatWho() {
	pres := c.sq.sq.Presence()
	c.out(fmt.Sprintf("Attached (%d):", len(pres)))
	name := func(id string) string {
		if id == "" {
			return "-"
		}
		if s := c.squatSession(id); s != nil {
			return s.World.Name
		}
		return id
	}
	for _, p := range pres {
		typing := ""
		if p.Typing != "" {
			typing = "  typing in " + name(p.Typing)
		}
		ext := ""
		if p.Ext && !p.Local {
			ext = " [gtf]"
		}
		c.out(fmt.Sprintf("  %-20s %-9s views %-16s%s%s", p.Name, p.Role, name(p.View), ext, typing))
	}
	if cols := c.sq.sq.Config().NAWS; cols != "" {
		c.out("  width reported to games: policy " + cols)
	}
}

// cmdPair creates a one-time pairing code and prints it the way `squatterd pair` does. The code is a
// secret: it is shown only to whoever ran the command (not recorded in history, logs or scrollback).
func (c *Client) cmdPair(_ *gtfparser.Interp, args string) gtfparser.Value {
	role := strings.ToLower(strings.TrimSpace(args))
	info, err := c.sq.sq.Pair(role)
	if err != nil {
		c.out("% /pair: " + err.Error())
		return gtfparser.Int(0)
	}
	for _, l := range info.Lines() {
		c.privateLine(l)
	}
	return gtfparser.Int(1)
}

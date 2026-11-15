// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package client

// cmcpscreen.go draws the console screen of the foreground world's CMCP session (the std.* screens
// and layout-pack screens, bound to protocol data by the core's pack package) with the cell
// renderer of internal/cmcpui, and operates it from the keyboard.

import (
	"fmt"
	"strings"
	"time"

	"github.com/itsnotyoutoday/cmcp/pack"
	"github.com/itsnotyoutoday/cmcp/raster"
	"github.com/itsnotyoutoday/cmcp/session"
	"github.com/itsnotyoutoday/gtf/internal/cmcpui"
	"github.com/itsnotyoutoday/gtf/internal/layout"
	"github.com/itsnotyoutoday/gtf/internal/ui"
	"github.com/itsnotyoutoday/gtfparser"
)

// cmcpScr is the per-connection screen state.
type cmcpScr struct {
	st        *cmcpui.State
	states    map[string]*cmcpui.State // $ui, focus and cursors per screen: leaving a context keeps them (spec 3.3, 16.6)
	gen       int
	frame     *cmcpui.Frame
	override  string // a client-local {"show":...} screen, until the server names another
	lastSrv   string
	notice    string
	noticeEnd time.Time
	effects   []cmcpui.Effect
	sprites   *raster.Cache
	packs     map[string]*pack.Pack // layout packs verified, validated and consented (cmcppacks.go)
	zonesFor  map[string][]string
	jobs      map[string]*packJob
	auto      map[string]bool // group pickers that already got their first group selected
}

func (s *Session) cmScr() *cmcpScr {
	if s.cs == nil {
		s.cs = &cmcpScr{st: cmcpui.NewState(), sprites: raster.NewCache(32), packs: map[string]*pack.Pack{}, zonesFor: map[string][]string{}}
	}
	return s.cs
}

// cmcpCaps: the default capabilities of the core: every core widget, none of the optional ones
// (the joystick resolves to helm-controls: gtf steers with commands, the owner's decision; scope
// falls back to radar; lights are not drawn).
var cmcpCaps = pack.Caps{Class: "cell"} // also applies the pack's layout.cell hints (spec 6.13)

func (c *Client) cmcpScreensOn() bool {
	switch strings.ToLower(strings.TrimSpace(c.in.Var("cmcp_screens"))) {
	case "off", "0", "no", "false":
		return false
	}
	return true
}

func onVar(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "on", "1", "yes", "true":
		return true
	}
	return false
}

// cmcpThemeFor resolves the theme by spec 6.6: the player's explicit choice; else, with "use the
// ship's theme" on, the console's, the ship's, then the screen's suggestion; else plain.
func (c *Client) cmcpThemeFor(u *session.UI, suggest string) string {
	if t := strings.TrimSpace(c.in.Var("cmcp_theme")); t != "" && t != "auto" {
		return t
	}
	if onVar(c.in.Var("cmcp_ship_theme")) {
		for _, t := range []string{u.ConsoleTheme, u.ShipTheme, suggest} {
			if t != "" {
				return t
			}
		}
	}
	return "plain"
}

// cmcpScreenFor finds the screen to draw (a pack screen, a built-in one, or a client-local override).
func (c *Client) cmcpScreenFor(s *Session, u *session.UI) (*pack.Screen, *pack.Pack, string) {
	cs := s.cmScr()
	id := u.Screen.Resolved
	if u.Screen.ID != cs.lastSrv {
		cs.lastSrv, cs.override = u.Screen.ID, ""
	}
	if cs.override != "" {
		id = cs.override
	}
	if id == "" {
		return nil, nil, ""
	}
	if i := strings.IndexByte(id, '/'); i > 0 {
		if p := cs.packs[id[:i]]; p != nil {
			if sc := p.Screens[id[i+1:]]; sc != nil {
				return sc, p, id
			}
		}
		if u.Console != nil {
			id = "std." + u.Console.ID
		}
	}
	return pack.Builtin(id), pack.BuiltinPack(), id
}

type cmAssets struct {
	c   *Client
	s   *Session
	p   *pack.Pack
	env *pack.Env
	th  cmcpui.Theme
}

func (a *cmAssets) sprite(id string) *pack.Sprite {
	if a.p == nil {
		return nil
	}
	return a.p.Sprites[id]
}

func viewOf(sp *pack.Sprite) string {
	if sp.DefaultView != "" {
		return sp.DefaultView
	}
	if len(sp.ViewNames) > 0 {
		return sp.ViewNames[0]
	}
	return ""
}

func (a *cmAssets) Zones(id string) []string {
	sp := a.sprite(id)
	if sp == nil {
		return nil
	}
	b := sp.Body(viewOf(sp))
	if b == nil {
		return nil
	}
	var z []string
	for _, zz := range b.Zones {
		z = append(z, zz.ID)
	}
	return z
}

func (a *cmAssets) Sprite(id string, w, h int) *cmcpui.Block {
	sp := a.sprite(id)
	if sp == nil || w < 4 || h < 2 {
		return nil
	}
	view := viewOf(sp)
	spr, err := sp.Resolve(view, a.p.Resolver(a.th.TokenMap()))
	if err != nil {
		return nil
	}
	par := sp.Evaluate(view, a.env).Params()
	cells, err := a.s.cmScr().sprites.Cells(spr, w, h, par, raster.Options{Mode: raster.HalfBlock, Depth: raster.TrueColor})
	if err != nil || cells == nil {
		return nil
	}
	blk := &cmcpui.Block{W: cells.W, H: cells.H, Cells: make([]layout.Cell, len(cells.C))}
	for i, ce := range cells.C {
		blk.Cells[i] = layout.Cell{R: ce.R, A: layout.Attr{FG: layoutColorC(ce.FG), BG: layoutColorC(ce.BG)}}
	}
	return blk
}

func layoutColorC(t raster.TermColor) layout.Color {
	switch t.Kind {
	case raster.KindANSI:
		return layout.Color{Kind: layout.ANSI, N: t.N}
	case raster.KindIndexed:
		return layout.Color{Kind: layout.Indexed, N: t.N}
	case raster.KindRGB:
		return layout.Color{Kind: layout.RGB, R: t.R, G: t.G, B: t.B}
	}
	return layout.Color{}
}

// cmcpSyncSurface draws the screen for the foreground world into the UI (called from syncUI).
func (c *Client) cmcpSyncSurface() {
	c.ui.Surf = nil
	s := c.fg
	if s == nil || s.cm == nil || !c.cmcpScreensOn() || !c.ui.Visual {
		return
	}
	u := s.cm.UI()
	if u.State != session.StateReady {
		return
	}
	cs := s.cmScr()
	if u.Generation != cs.gen {
		cs.gen = u.Generation
		cs.st.Reset()
		cs.states, cs.auto = nil, nil
	}
	scr, p, id := c.cmcpScreenFor(s, u)
	if scr == nil {
		return
	}
	if cs.states == nil {
		cs.states = map[string]*cmcpui.State{}
	}
	if st := cs.states[id]; st != nil {
		cs.st = st
	} else {
		st := cmcpui.NewState()
		cs.states[id], cs.st = st, st
	}
	w, h := c.ui.W, c.ui.OutRows()
	now := c.cfg.Now()
	env := pack.EnvFromUI(u, cs.st.UI)
	budget := 4096
	env.Budget = &budget
	tree := scr.Bind(env, cmcpCaps)
	plan := c.cmcpPlan(cs, tree)
	th := cmcpui.ThemeByName(c.cmcpThemeFor(u, scr.SuggestTheme))
	hdr := scr.Title
	if hdr == "" {
		hdr = id
	}
	if u.Ship.Name != "" {
		hdr += "  -  " + u.Ship.Name
	}
	if n := len(u.Stack); n > 1 {
		hdr += fmt.Sprintf("  [%d contexts]", n)
	}
	if u.Readonly {
		hdr += "  [read-only]"
		if u.AI {
			hdr += " AI"
		}
	}
	notice := ""
	if now.Before(cs.noticeEnd) {
		notice = cs.notice
	}
	var live []cmcpui.Effect
	for _, e := range cs.effects {
		if now.Sub(e.At) < 3*time.Second {
			live = append(live, e)
		}
	}
	cs.effects = live
	ce := cmcpui.Env{Layout: strings.ToLower(strings.TrimSpace(c.in.Var("cmcp_layout"))), Mnemonics: plan.Mnemonics, Theme: th, Now: now, Alert: u.Alert, Header: hdr, Notice: notice, Effects: live,
		Assets: &cmAssets{c: c, s: s, p: p, env: env, th: th}}
	f := cmcpui.Draw(tree, w, h, ce, cs.st)
	if c.cmcpAutoSelect(s, f) {
		// a picker had nothing selected: its first row was chosen (a $ui write only), draw again
		env = pack.EnvFromUI(u, cs.st.UI)
		budget = 4096
		env.Budget = &budget
		tree = scr.Bind(env, cmcpCaps)
		ce.Mnemonics = c.cmcpPlan(cs, tree).Mnemonics
		ce.Assets = &cmAssets{c: c, s: s, p: p, env: env, th: th}
		f = cmcpui.Draw(tree, w, h, ce, cs.st)
	}
	cs.frame = f
	c.ui.Surf = &ui.Surface{Grid: f.Grid, Text: layout.Rect{X: f.Text.X, Y: f.Text.Y, W: f.Text.W, H: f.Text.H}}
}

// cmcpNoticef shows a short message in the screen's header.
func (c *Client) cmcpNotice(s *Session, text string) {
	cs := s.cmScr()
	cs.notice, cs.noticeEnd = text, c.cfg.Now().Add(6*time.Second)
	c.scheduleRender()
}

// ---- keys ----

// cmcpPlan builds the focus plan of the bound screen (core: focusable widgets in the cell class's
// order, automatic mnemonics) and gives it to the screen's shared focus model.
func (c *Client) cmcpPlan(cs *cmcpScr, tree *pack.BoundNode) cmcpui.Plan {
	panels, mn, _ := tree.FocusPlan(nil)
	p := cmcpui.Plan{Panels: panels, Mnemonics: mn}
	cs.st.SetupModel(p, cmcpui.FocusKeyName(c.in.Var("cmcp_key_focus")))
	return p
}

// cmcpKeys offers raw terminal bytes to the screen and returns true when it took them. The
// keyboard model is the shared one (cmcp/go/focus, spec 6.12): Tab on an empty input line enters
// the screen (a rebound key, %cmcp_key_focus, always does); inside, Tab and Shift-Tab move
// between panels (wrapping), arrows work inside lists and on sliders, Enter activates, a mnemonic
// letter presses its button (a hold_ms one three times in two seconds), Esc or any other printable
// key returns to the input line (and is typed there). The input line always works.
func (c *Client) cmcpKeys(raw string) bool {
	s := c.fg
	if s == nil || s.cm == nil || s.cs == nil || s.cs.frame == nil || c.ui.Surf == nil || s.cs.st.Model == nil {
		return false
	}
	cs := s.cs
	name, ok := cmcpui.KeyName(raw)
	if !ok {
		return false
	}
	empty := c.in.Input().Text() == ""
	if cs.st.Active && !empty {
		cmcpui.Dispatch(cs.frame, cs.st, "esc", true, c.cfg.Now()) // the player is typing
		c.scheduleRender()
		return false
	}
	if cs.st.Active && (raw == "y" || raw == "n") && c.packDecide(s, "", raw == "y") {
		c.cmcpNotice(s, "")
		return true
	}
	r := cmcpui.Dispatch(cs.frame, cs.st, name, empty, c.cfg.Now())
	for _, ev := range r.Events {
		c.cmcpPerform(s, ev)
	}
	if r.Hold != "" {
		_, n, _ := strings.Cut(r.Hold, ":")
		c.cmcpNotice(s, "press again to confirm ("+n+"/3)")
	}
	c.scheduleRender()
	return !r.Input
}

// cmcpToggleFocus is /cmcp focus: enter the screen's focus mode, or leave it.
func (c *Client) cmcpToggleFocus(s *Session) {
	if s == nil || s.cs == nil || s.cs.frame == nil || s.cs.st.Model == nil {
		return
	}
	cs := s.cs
	if cs.st.Active {
		cmcpui.Dispatch(cs.frame, cs.st, "esc", true, c.cfg.Now())
	} else {
		cmcpui.Dispatch(cs.frame, cs.st, cmcpui.FocusKeyName(c.in.Var("cmcp_key_focus")), true, c.cfg.Now())
	}
	c.scheduleRender()
}

// cmcpPerform runs a widget event through the core: actions become commands (with the echo of the
// equivalent typed text), $ui writes, local screen switches and UI sounds.
func (c *Client) cmcpPerform(s *Session, ev cmcpui.Event) {
	cs := s.cmScr()
	u := s.cm.UI()
	env := pack.EnvFromUI(u, cs.st.UI)
	res := ev.Node.Perform(ev.Name, ev.Value, env, cs.st.UI, s.cm, c.cfg.Now())
	for _, t := range res.Tickets {
		if t.Mutating && t.Echo != "" {
			c.cmcpEcho(s, t.Echo)
		}
	}
	for _, err := range res.Errs {
		c.cmcpNotice(s, err.Error())
	}
	if res.Show != "" {
		cs.override = res.Show
	}
	for _, sc := range res.Sounds {
		if bellEvents[sc.Event] {
			c.cmcpBell()
		}
	}
	c.cmcpPump(s)
}

// cmcpEcho prints the typed-text equivalent of a command as a client-generated line (spec 4.5).
func (c *Client) cmcpEcho(s *Session, text string) {
	c.echoTo(s.scr, "Ccyan", "cmcp> "+text)
}

var _ = gtfparser.Int

// cmcpAutoSelect gives a group picker whose selection is empty its first group, when selecting
// only writes screen state (so a screen that shows "the selected group" has something to show).
func (c *Client) cmcpAutoSelect(s *Session, f *cmcpui.Frame) bool {
	cs := s.cmScr()
	if cs.auto == nil {
		cs.auto = map[string]bool{}
	}
	did := false
	for _, it := range f.Items {
		n := it.Node
		if n.Widget != "group-picker" || cs.auto[fmt.Sprintf("%p|%s", cs.st, n.Node.Path)] || n.Props["selected"] != nil {
			continue
		}
		acts := n.Node.On["select"]
		onlySet := len(acts) > 0
		for _, a := range acts {
			if a.Kind != pack.ActSet {
				onlySet = false
			}
		}
		groups, _ := n.Props["groups"].(map[string]any)
		if !onlySet || len(groups) == 0 {
			continue
		}
		cs.auto[fmt.Sprintf("%p|%s", cs.st, n.Node.Path)] = true
		first := cmcpui.FirstKey(groups)
		n.Node.Perform("select", first, pack.EnvFromUI(s.cm.UI(), cs.st.UI), cs.st.UI, nil, c.cfg.Now())
		did = true
	}
	return did
}

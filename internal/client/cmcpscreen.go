// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package client

// cmcpscreen.go draws the console screen of the foreground world's CMCP session (the std.* screens
// and layout-pack screens, bound to protocol data by the core's pack package) with the cell
// renderer of internal/cmcpui, and operates it from the keyboard.

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/itsnotyoutoday/cmcp/go/ambient"
	"github.com/itsnotyoutoday/cmcp/go/focus"
	"github.com/itsnotyoutoday/cmcp/go/textport"
	"github.com/itsnotyoutoday/cmcp/go/theme"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/itsnotyoutoday/cmcp/go/notice"
	"github.com/itsnotyoutoday/cmcp/go/pack"
	"github.com/itsnotyoutoday/cmcp/go/pulse"
	"github.com/itsnotyoutoday/cmcp/go/raster"
	"github.com/itsnotyoutoday/cmcp/go/session"
	"github.com/itsnotyoutoday/gtf/internal/cmcpui"
	"github.com/itsnotyoutoday/gtf/internal/layout"
	"github.com/itsnotyoutoday/gtf/internal/ui"
	"github.com/itsnotyoutoday/gtfparser"
)

// cmcpScr is the per-connection screen state.
type cmcpScr struct {
	st           *cmcpui.State
	states       map[string]*cmcpui.State // $ui, focus and cursors per screen: leaving a context keeps them (spec 3.3, 16.6)
	gen          int
	frame        *cmcpui.Frame
	override     string // a client-local {"show":...} screen, until the server names another
	lastSrv      string
	effects      []cmcpui.Effect
	sprites      *raster.Cache
	packs        map[string]*pack.Pack // layout packs verified, validated and consented (cmcppacks.go)
	zonesFor     map[string][]string
	jobs         map[string]*packJob
	view         *textport.View         // the shared LOG / screen-off / textport_default state machine (cmcp/go/textport)
	tween        tweenState             // the F3 animation in progress
	themes       map[string]*theme.Pack // downloaded theme packs (verified, consented)
	themeAsked   map[string]bool
	shipTheme    string // the one theme of this ship (theme_mode ship), until the next ship hop
	shipThemeGen int
	told         bool   // the one-line CMCP notice was shown
	lockNote     string // "MENU x - locked: hint", shown until the next key
	reenter      bool   // a screen switch from the menu keeps the keyboard focus on the new screen
	alertPick    string // the level the alert chooser has highlighted; empty when it is closed
	fxKey        string // the ambience/shake settings last given to the core
	alertLevel   string // the alert level the pulse counts from
	alertSince   time.Time
	pulseAt      time.Time       // the flip a redraw is already scheduled for
	reg          *theme.Registry // downloaded theme versions, the override rule and the dev force flag (spec 19.9)
	auto         map[string]bool // group pickers that already got their first group selected
}

func (s *Session) cmScr() *cmcpScr {
	if s.cs == nil {
		s.cs = &cmcpScr{view: textport.NewViewWith(onVar(s.c.in.Var("cmcp_start_collapsed")), textport.State(strings.ToLower(strings.TrimSpace(s.c.in.Var("cmcp_textport"))))), st: cmcpui.NewState(), reg: theme.NewRegistry(theme.ClassCell), sprites: raster.NewCache(32), packs: map[string]*pack.Pack{}, zonesFor: map[string][]string{}}
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
func (c *Client) cmcpThemeFor(s *Session, u *session.UI, suggest string) string {
	// theme_mode: "ship" (the default) follows ONE theme per ship: the ship's suggestion (Cobra.Console
	// ship.theme), then the console's, then the loaded pack screen's; it is chosen once per ship (a hop
	// is a new Generation) so a ship never mixes themes when some consoles fall back to built-in
	// screens. "fixed" keeps the player's %cmcp_theme; %cmcp_theme alone also fixes it.
	fixed := strings.TrimSpace(c.in.Var("cmcp_theme"))
	mode := strings.ToLower(strings.TrimSpace(c.in.Var("cmcp_theme_mode")))
	if mode == "" && onVar(c.in.Var("cmcp_ship_theme")) {
		mode = "ship" // the old setting
	}
	if mode == "fixed" || (mode == "" && fixed != "" && fixed != "auto") {
		if fixed != "" && fixed != "auto" {
			return fixed
		}
		return "tng"
	}
	cs := s.cmScr()
	if cs.shipThemeGen == u.Generation && cs.shipTheme != "" {
		return cs.shipTheme
	}
	pick := ""
	for _, t := range []string{u.ShipTheme, u.ConsoleTheme, suggest} {
		if t != "" {
			pick = t // an announced theme pack need not be embedded: cmcpResolveTheme falls back
			break
		}
	}
	if pick == "" {
		if fixed != "" && fixed != "auto" {
			pick = fixed
		} else {
			pick = "tng" // nothing suggested: the default
		}
	}
	if u.ShipTheme != "" || u.ConsoleTheme != "" || suggest != "" {
		cs.shipTheme, cs.shipThemeGen = pick, u.Generation
	}
	return pick
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
	sc := pack.Builtin(id)
	if sc == nil {
		// the core resolves a missing pack to "std.<console id>", but a console id is not always a
		// built-in screen id (navigation is the helm, science the sensors)
		if alt, ok := consoleScreens[strings.TrimPrefix(id, "std.")]; ok {
			id, sc = "std."+alt, pack.Builtin("std."+alt)
		}
	}
	return sc, pack.BuiltinPack(), id
}

var consoleScreens = map[string]string{"navigation": "helm", "science": "sensors", "comms": "communications", "ops": "operations"}

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
	c.cmcpSyncSurfaceInner()
	c.setMouse(c.ui.Surf != nil)
}

func (c *Client) cmcpSyncSurfaceInner() {
	c.ui.Surf = nil
	s := c.fg
	if s == nil || s.cm == nil || !c.cmcpScreensOn() || !c.ui.Visual {
		return
	}
	u := s.cm.UI()
	if u.State != session.StateReady || s.cmScr().view.State() == textport.Off {
		return // the screen is off: data keeps streaming, the text view has the whole area
	}
	cs := s.cmScr()
	if u.Generation != cs.gen {
		cs.gen = u.Generation
		cs.st.Reset()
		cs.states, cs.auto = nil, nil
	}
	scr, p, id := c.cmcpScreenFor(s, u)
	if scr == nil {
		// a room, a dock or an away scene: no console screen, but the ship's alert still shows (6.17.1)
		now := c.cfg.Now()
		th := c.cmcpResolveTheme(s, u, c.cmcpThemeFor(s, u, ""))
		c.cmcpFxConfig(s, cs, th)
		c.cmcpAmbientFrame(s, u, cs, now)
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
	c.cmcpDropStale(cs, u.Ship.ID)
	w, h := c.ui.W, c.ui.OutRows()
	fullH := h
	if h >= 12 {
		h -= 2 // the top row is the notice line (theme.NoticeRows) and the bottom row gtf's own key-hint line
	}
	now := c.cfg.Now()
	env := pack.EnvFromUI(u, cs.st.UI)
	env.Ctx = u.CtxLocal() // $ctx: the active context (name, kind, key)
	budget := 4096
	env.Budget = &budget
	tree := scr.Bind(env, cmcpCaps)
	plan := c.cmcpPlan(cs, tree)
	if cs.reenter {
		cs.reenter = false
		if cs.st.Model != nil {
			cmcpui.Dispatch(&cmcpui.Frame{}, cs.st, cmcpui.FocusKeyName(c.in.Var("cmcp_key_focus")), true, now)
		}
	}
	th := c.cmcpResolveTheme(s, u, c.cmcpThemeFor(s, u, scr.SuggestTheme))
	c.cmcpFxConfig(s, cs, th)
	look := s.cm.Ambient(now)
	title := scr.Title
	if n := len(u.Stack); n > 0 && u.Stack[n-1].Kind == "room" && u.Stack[n-1].Name != "" {
		title = u.Stack[n-1].Name // a ship room: the frame's title is the room's name ($ctx.name, spec 3.3.3)
	}
	hdr := title
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
	pulse := c.cmcpPulse(s, cs, th, u.Alert, now)
	noteText, noteKind := "", cmcpui.NoticeInfo
	if n := s.cm.Notice(now); n != nil {
		noteText = n.Line()
		noteKind = noteKindOf(n)
		time.AfterFunc(250*time.Millisecond, c.scheduleRender) // the line lingers, fades and expires on the clock
	}
	var live []cmcpui.Effect
	for _, e := range cs.effects {
		if now.Sub(e.At) < 3*time.Second {
			live = append(live, e)
		}
	}
	cs.effects = live
	textRows := c.cmcpTextRows(s, cs, w, h, th, now)
	tags := []string{}
	if len(u.Stack) > 1 {
		tags = append(tags, fmt.Sprintf("%d contexts", len(u.Stack)))
	}
	if u.Readonly {
		tags = append(tags, "read-only")
	}
	var nav []cmcpui.NavEntry
	for _, o := range session.MenuScreens(u.Screens) { // only the screens this console may show (6.14)
		nav = append(nav, cmcpui.NavEntry{ID: o.ID, Title: o.Title, Active: o.Active, Available: o.Available})
	}
	ce := cmcpui.Env{Nav: nav, Consent: c.cmcpConsentLine(cs), Status: c.cmcpStatusStrip(u), Expanded: cs.view.State() == textport.Expanded && !cs.tween.active(now), TextRows: textRows, ScreenID: id, Vars: c.cmcpRoomVars(u, c.cmcpVars(u, id, title), title), Builtin: p != nil && p.Builtin, Title: title, Ship: u.Ship.Name, Tags: tags, ASCII: c.cmcpASCII(), Layout: strings.ToLower(strings.TrimSpace(c.in.Var("cmcp_layout"))), Mnemonics: plan.Mnemonics, Theme: th, Now: now, Alert: u.Alert, TextCols: c.cmcpTextWidth(), AlertRecolour: c.cmcpRecolourWith(th, look), PulseDim: pulse.Dim, PulseDepth: pulse.Depth, Header: hdr, Effects: live,
		Assets: &cmAssets{c: c, s: s, p: p, env: env, th: th}}
	f := cmcpui.Draw(tree, w, h, ce, cs.st)
	if c.cmcpAutoSelect(s, f) {
		// a picker had nothing selected: its first row was chosen (a $ui write only), draw again
		env = pack.EnvFromUI(u, cs.st.UI)
		env.Ctx = u.CtxLocal() // $ctx: the active context (name, kind, key)
		budget = 4096
		env.Budget = &budget
		tree = scr.Bind(env, cmcpCaps)
		ce.Mnemonics = c.cmcpPlan(cs, tree).Mnemonics
		ce.Assets = &cmAssets{c: c, s: s, p: p, env: env, th: th}
		f = cmcpui.Draw(tree, w, h, ce, cs.st)
	}
	c.cmcpOrderPanels(cs, f, plan)
	f.Grid = c.cmcpWithHint(f, cs, noteText, noteKind, u.Alert, w, fullH)
	if dx, dy := c.cmcpShake(s, now); dx != 0 || dy != 0 {
		f.Grid = shiftGrid(f.Grid, f.Text, dx, dy)
	}
	cs.frame = f
	c.ui.Surf = &ui.Surface{Grid: f.Grid, Text: layout.Rect{X: f.Text.X, Y: f.Text.Y, W: f.Text.W, H: f.Text.H}}
}

// cmcpNoticef shows a short message in the screen's header.
func (c *Client) cmcpNotice(s *Session, text string) {
	if text != "" {
		c.cmcpNoteKey(s, "", text, cmcpui.NoticeInfo)
	}
}

// cmcpNoteKey posts to the notice line; a notice with the key of a visible one replaces it in place.
func (c *Client) cmcpNoteKey(s *Session, key, text string, kind cmcpui.NoticeKind) {
	n := notice.Notice{ID: key, Kind: notice.Info, Text: text}
	switch kind {
	case cmcpui.NoticeProgress:
		n.Kind, n.State = notice.Download, notice.Active
	case cmcpui.NoticeDone:
		n.Kind, n.State = notice.Download, notice.Done
	case cmcpui.NoticeFail:
		n.Kind, n.State = notice.Download, notice.Failed
	}
	if n.ID == "" {
		n.ID = fmt.Sprintf("n%d", c.cfg.Now().UnixNano())
	}
	s.cm.Notify(c.cfg.Now(), n)
	c.scheduleRender()
}

// noteKindOf maps a core notice to the look of its line.
func noteKindOf(n *notice.Notice) cmcpui.NoticeKind {
	switch {
	case n.State == notice.Active:
		return cmcpui.NoticeProgress
	case n.State == notice.Done:
		return cmcpui.NoticeDone
	case n.State == notice.Failed || n.Kind == notice.Error || n.Kind == notice.Warning:
		return cmcpui.NoticeFail
	}
	return cmcpui.NoticeInfo
}

// ---- keys ----

// cmcpPlan builds the focus plan of the bound screen (core: focusable widgets in the cell class's
// order, automatic mnemonics) and gives it to the screen's shared focus model.
func (c *Client) cmcpPlan(cs *cmcpScr, tree *pack.BoundNode) cmcpui.Plan {
	panels, mn, _ := tree.FocusPlan(nil)
	if cs.st.Model != nil && len(cs.st.Order) > 0 {
		// keep the order of the last draw and only the panels that were actually drawn (a clipped
		// widget must not be a focus stop the player cannot see); the screen menu is a panel of
		// its own and stays
		byID := map[string]focus.Panel{}
		for _, mp := range panels {
			byID[mp.ID] = mp
		}
		var kept []focus.Panel
		for _, id := range cs.st.Order {
			if id == "~nav" {
				kept = append(kept, focus.Panel{ID: id, Kind: focus.List})
			} else if mp, ok := byID[id]; ok {
				kept = append(kept, mp)
			}
		}
		panels = kept
	}
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
	if s == nil || s.cm == nil {
		return false
	}
	name, ok := cmcpui.KeyName(raw)
	if !ok {
		return false
	}
	s.cmScr().lockNote = "" // the locked-screen note lasts until the next key
	if c.cmcpScreensOn() && c.cmcpAlertKey(s, name, raw) {
		return true
	}
	if c.cmcpScreensOn() && s.cs != nil {
		switch name {
		case cmcpui.FocusKeyName(orStr(c.in.Var("cmcp_key_narrow"), "^[,")):
			c.cmcpSetWidth(s, fmt.Sprintf("%d", -widthStep))
			return true
		case cmcpui.FocusKeyName(orStr(c.in.Var("cmcp_key_widen"), "^[.")):
			c.cmcpSetWidth(s, fmt.Sprintf("+%d", widthStep))
			return true
		}
	}
	if c.cmcpScreensOn() && name == cmcpui.FocusKeyName(orStr(c.in.Var("cmcp_key_close"), "F6")) && c.cmcpConsentLine(s.cmScr()) == "" {
		c.cmcpClose(s) // while a pack question is pending F6 is its REFUSE key instead
		return true
	}
	// two screen-wide keys that work from the input line too: the log view (F3) and the screen off (F4)
	if c.cmcpScreensOn() {
		switch name {
		case cmcpui.FocusKeyName(orStr(c.in.Var("cmcp_key_log"), "F3")):
			c.cmcpToggleLog(s)
			return true
		case cmcpui.FocusKeyName(orStr(c.in.Var("cmcp_key_hide"), "F4")):
			c.cmcpToggleHide(s)
			return true
		}
	}
	if s.cs == nil || s.cs.frame == nil || c.ui.Surf == nil || s.cs.st.Model == nil {
		return false
	}
	cs := s.cs
	empty := c.in.Input().Text() == ""
	// the consent question answers on F5 / F6 only while a question is pending (F5 is the alert control otherwise)
	if (name == "f5" || name == "f6") && c.cmcpConsentLine(cs) != "" {
		if c.packDecide(s, "", name == "f5") {
			return true
		}
	}
	if name == "shift_tab" && !cs.st.Active {
		if !empty || len(cs.frame.Items) == 0 {
			return empty // never leak a recognised navigation key to the undefined-key error
		}
		// Shift+Tab enters the screen on the LAST panel: Tab to enter, Shift+Tab to wrap back to the end
		cmcpui.Dispatch(cs.frame, cs.st, cmcpui.FocusKeyName(c.in.Var("cmcp_key_focus")), true, c.cfg.Now())
		cmcpui.Dispatch(cs.frame, cs.st, "shift_tab", true, c.cfg.Now())
		c.scheduleRender()
		return true
	}
	if cs.st.Active && !empty {
		cmcpui.Dispatch(cs.frame, cs.st, "esc", true, c.cfg.Now()) // the player is typing
		c.scheduleRender()
		return false
	}
	if cs.st.Active {
		// the screen menu: [ and ] step through the screens the console may show, a digit picks the nth
		switch {
		case raw == "[":
			c.cmcpNavStep(s, -1)
			return true
		case raw == "]":
			c.cmcpNavStep(s, 1)
			return true
		case len(raw) == 1 && raw[0] >= '1' && raw[0] <= '9':
			if c.cmcpNavTo(s, int(raw[0]-'1')) {
				return true
			}
		}
	}
	r := cmcpui.Dispatch(cs.frame, cs.st, name, empty, c.cfg.Now())
	for _, ev := range r.Events {
		if ev.Nav != "" {
			err := s.cm.ShowScreen(c.cfg.Now(), ev.Nav)
			var se *session.ScreenError
			if errors.As(err, &se) {
				title := ev.Nav
				for _, o := range s.cm.UI().Screens {
					if o.ID == ev.Nav {
						title = o.Title
					}
				}
				cs.lockNote = fmt.Sprintf("MENU %s - locked: %s to use", title, se.HintCmd)
				c.cmcpPump(s)
				continue
			}
			c.cmcpNavErr(s, err)
			continue
		}
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
	env.Ctx = u.CtxLocal() // $ctx: the active context (name, kind, key)
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

// ContactArt gives a ship contact the art its scene names for its empire and class, in the view
// that faces the observer (pack.SelectView), tinted from the contact's own data (never the own
// ship's damage, spec 7.2.3).
func (a *cmAssets) ContactArt(scene string, contact map[string]any, w, h int) *cmcpui.Block {
	if a.p == nil || w < 3 || h < 2 {
		return nil
	}
	sc := a.p.Scenes[scene]
	if sc == nil {
		return nil
	}
	var def struct {
		Contacts struct {
			Art     map[string]string `json:"art"`
			Default string            `json:"default"`
		} `json:"contacts"`
	}
	if json.Unmarshal(sc.Raw, &def) != nil {
		return nil
	}
	id := pack.LookupArt(def.Contacts.Art, str1(contact["empire"]), str1(contact["class"]), str1(contact["type"]), def.Contacts.Default)
	sp := a.p.Sprites[id]
	if sp == nil {
		return nil
	}
	az, el := 0.0, 0.0
	if hd, ok := contact["heading"].(map[string]any); ok {
		if v, ok := hd["az"].(float64); ok {
			az = v
		}
	}
	view, _ := pack.SelectView(sp.ViewNames, az, el)
	if view == "" {
		view = viewOf(sp)
	}
	spr, err := sp.Resolve(view, a.p.Resolver(a.th.TokenMap()))
	if err != nil {
		return nil
	}
	par := sp.EvaluateContact(view, contact).Params()
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

func str1(v any) string { s, _ := v.(string); return s }

// cmcpASCII is %cmcp_ascii: on, off, or auto (default): ASCII when the locale is not UTF-8.
func (c *Client) cmcpASCII() bool {
	switch strings.ToLower(strings.TrimSpace(c.in.Var("cmcp_ascii"))) {
	case "on", "1", "yes", "true":
		return true
	case "off", "0", "no", "false":
		return false
	}
	for _, k := range []string{"LC_ALL", "LC_CTYPE", "LANG"} {
		if v := os.Getenv(k); v != "" {
			u := strings.ToUpper(v)
			return !(strings.Contains(u, "UTF-8") || strings.Contains(u, "UTF8"))
		}
	}
	return false
}

func orStr(s, d string) string {
	if strings.TrimSpace(s) == "" {
		return d
	}
	return s
}

// tweenState is the F3 animation: the text pane's rows step from one value to another in whole
// cells (textport.PlanTween: 4 linear steps over 200 ms, instant with %cmcp_motion off).
type tweenState struct {
	from, to int
	start    time.Time
	plan     textport.Plan
	last     int // the rows last drawn
}

func (t *tweenState) active(now time.Time) bool {
	return t.plan.Steps > 1 && now.Sub(t.start) < time.Duration(t.plan.DurationMS)*time.Millisecond
}

// rows is the text pane's rows at time now.
func (t *tweenState) rows(now time.Time) int {
	if !t.active(now) {
		return t.to
	}
	step := int(now.Sub(t.start) / (time.Duration(t.plan.DurationMS) * time.Millisecond / time.Duration(t.plan.Steps)))
	fr := textport.CellFrames(textport.Rect{0, 0, 0, float64(t.from)}, textport.Rect{0, 0, 0, float64(t.to)}, t.plan.Steps)
	return int(fr[min(step, len(fr)-1)][3])
}

func (c *Client) cmcpMotionOn() bool {
	switch strings.ToLower(strings.TrimSpace(c.in.Var("cmcp_motion"))) {
	case "off", "0", "no", "false":
		return false
	}
	return true
}

// cmcpTextRows gives the text pane's rows for this frame from the shared view state (the player's
// %cmcp_textport size, the LOG and screen-off toggles) and runs the toggle's animation.
func (c *Client) cmcpTextRows(s *Session, cs *cmcpScr, w, h int, th cmcpui.Theme, now time.Time) int {
	top, bottom := th.Margins(h)
	cs.view.SetResting(textport.State(strings.ToLower(strings.TrimSpace(c.in.Var("cmcp_textport")))))
	ih := h - top - bottom
	iw := w
	target := int(textport.MinExtent(float64(iw), float64(ih), textport.Stacked, cs.view.State(), true))
	if cs.tween.last == 0 {
		cs.tween = tweenState{from: target, to: target, last: target}
		return target
	}
	if target != cs.tween.to {
		cs.tween.from = cs.tween.rows(now)
		cs.tween.to = target
		cs.tween.start = now
		cs.tween.plan = textport.PlanTween(true, false, !c.cmcpMotionOn(), false, cs.tween.from == target)
		if cs.tween.plan.Steps > 1 {
			step := time.Duration(cs.tween.plan.DurationMS/cs.tween.plan.Steps) * time.Millisecond
			for i := 1; i <= cs.tween.plan.Steps; i++ {
				time.AfterFunc(time.Duration(i)*step+5*time.Millisecond, func() {
					defer func() { recover() }()
					c.Post(func(*gtfparser.Interp) { c.scheduleRender() })
				})
			}
		}
	}
	r := cs.tween.rows(now)
	cs.tween.last = r
	return r
}

// cmcpVars fills the closed theme variables of spec 19.5 from the subscribed documents.
func (c *Client) cmcpVars(u *session.UI, id, title string) map[string]any {
	v := map[string]any{"mode": strings.TrimPrefix(id, "std."), "title": title, "ship": u.Ship.Name, "class": u.Ship.Class, "alert": u.Alert}
	if m, _ := u.Docs["Space.Damage.Stat"].(map[string]any); m != nil {
		if h, _ := m["hull"].(map[string]any); h != nil {
			cur, ok1 := h["cur"].(float64)
			mx, ok2 := h["max"].(float64)
			if ok1 && ok2 && mx > 0 {
				v["hull"] = cur / mx * 100
			}
		}
	}
	if m, _ := u.Docs["Space.Power"].(map[string]any); m != nil {
		if cm, _ := m["components"].(map[string]any); cm != nil {
			if p, ok := cm["pct"].(float64); ok {
				v["core"] = p
			}
		}
	}
	if m, _ := u.Docs["Ship.Target"].(map[string]any); m != nil {
		if cid, ok := m["cid"].(float64); ok {
			v["lock"] = cid
		}
	}
	return v
}

// cmcpToggleLog is the LOG toggle (F3): the shared state machine moves between the resting size and
// expanded; the text pane then steps there over four frames.
func (c *Client) cmcpToggleLog(s *Session) {
	cs := s.cmScr()
	cs.view.Event(textport.EvLog)
	if cs.st.Active && cs.st.Model != nil && cs.frame != nil && cs.view.State() == textport.Expanded {
		cmcpui.Dispatch(cs.frame, cs.st, "esc", true, c.cfg.Now())
	}
	c.scheduleRender()
}

// cmcpToggleHide is the screen-off toggle (F4).
func (c *Client) cmcpToggleHide(s *Session) {
	cs := s.cmScr()
	cs.view.Event(textport.EvScreen)
	if cs.view.State() == textport.Off && cs.st.Active && cs.st.Model != nil && cs.frame != nil {
		cmcpui.Dispatch(cs.frame, cs.st, "esc", true, c.cfg.Now())
	}
	c.scheduleRender()
}

// cmcpStatusStrip is the frame's bottom strip: hull, shields, core, lock and the alert state,
// from the documents the console already subscribes to (a missing one is left out).
func (c *Client) cmcpStatusStrip(u *session.UI) []string {
	var out []string
	if m, _ := u.Docs["Space.Damage.Stat"].(map[string]any); m != nil {
		if h, _ := m["hull"].(map[string]any); h != nil {
			if cur, ok1 := h["cur"].(float64); ok1 {
				if mx, ok2 := h["max"].(float64); ok2 && mx > 0 {
					out = append(out, fmt.Sprintf("HULL %.0f%%", cur/mx*100))
				}
			}
		}
	}
	if m, _ := u.Docs["Space.Shields"].(map[string]any); m != nil {
		if fc, _ := m["facings"].(map[string]any); len(fc) > 0 {
			sum, n := 0.0, 0.0
			for _, v := range fc {
				if f, _ := v.(map[string]any); f != nil {
					if p, ok := f["pct"].(float64); ok {
						sum, n = sum+p, n+1
					}
				}
			}
			if n > 0 {
				out = append(out, fmt.Sprintf("SHIELDS %.0f%%", sum/n))
			}
		}
	}
	if m, _ := u.Docs["Space.Power"].(map[string]any); m != nil {
		if cm, _ := m["components"].(map[string]any); cm != nil {
			if p, ok := cm["pct"].(float64); ok {
				out = append(out, fmt.Sprintf("CORE %.0f%%", p))
			}
		}
	}
	lock := "LOCK -"
	if m, _ := u.Docs["Ship.Target"].(map[string]any); m != nil {
		if cid, ok := m["cid"].(float64); ok {
			lock = fmt.Sprintf("LOCK %.0f", cid)
		}
	}
	out = append(out, lock)
	if u.Alert != "" {
		out = append(out, strings.ToUpper(u.Alert)+" ALERT")
	}
	return out
}

// cmcpNavStep shows the previous or next screen of the menu (wrapping).
func (c *Client) cmcpNavStep(s *Session, d int) {
	var err error
	if d > 0 {
		_, err = s.cm.NextScreen(c.cfg.Now())
	} else {
		_, err = s.cm.PrevScreen(c.cfg.Now())
	}
	c.cmcpNavErr(s, err)
}

// cmcpNavTo shows the n-th (0-based) screen of the menu; it reports whether there is one.
func (c *Client) cmcpNavTo(s *Session, n int) bool {
	menu := session.MenuScreens(s.cm.UI().Screens)
	if n < 0 || n >= len(menu) {
		return false
	}
	c.cmcpNavErr(s, s.cm.ShowScreen(c.cfg.Now(), menu[n].ID))
	return true
}

func (c *Client) cmcpNavErr(s *Session, err error) {
	if err == nil {
		s.cmScr().reenter = true
	}
	var se *session.ScreenError
	switch {
	case err == nil:
	case errors.As(err, &se):
		c.cmcpNotice(s, se.Reason+": type "+se.HintCmd)
	default:
		c.cmcpNotice(s, err.Error())
	}
	c.cmcpPump(s)
	c.scheduleRender()
}

// cmcpConsentLine is the question of a pending pack or theme consent, on its own prominent line.
func (c *Client) cmcpConsentLine(cs *cmcpScr) string {
	for _, j := range cs.jobs {
		if j.state == "asking" {
			kind := "LAYOUT PACK"
			if j.ref.Kind == "theme" {
				kind = "THEME PACK"
			}
			return fmt.Sprintf("%s %s %s: F5 = ALLOW   F6 = REFUSE", kind, strings.ToUpper(j.ref.ID), j.ref.Version)
		}
	}
	return ""
}

// cmcpOrderPanels puts the focus model's panels in reading order: the screen menu first, then
// everything else top to bottom, left to right, by where it was drawn.
func (c *Client) cmcpOrderPanels(cs *cmcpScr, f *cmcpui.Frame, plan cmcpui.Plan) {
	if cs.st.Model == nil {
		return
	}
	pos := map[string]cmcpui.Rect{}
	for _, it := range f.Items {
		pos[it.Node.Node.Path] = it.Rect
	}
	var ps []focus.Panel
	if _, ok := pos["~nav"]; ok {
		ps = append(ps, focus.Panel{ID: "~nav", Kind: focus.List})
	}
	var rest []focus.Panel
	for _, p := range plan.Panels {
		if p.ID != "~nav" {
			rest = append(rest, p)
		}
	}
	sort.SliceStable(rest, func(i, j int) bool {
		a, b := pos[rest[i].ID], pos[rest[j].ID]
		if a.Y != b.Y {
			return a.Y < b.Y
		}
		return a.X < b.X
	})
	for _, p := range rest {
		if _, ok := pos[p.ID]; ok {
			ps = append(ps, p)
		}
	}
	cs.st.Order = nil
	for _, p := range ps {
		cs.st.Order = append(cs.st.Order, p.ID)
	}
	cs.st.Model.SetPanels(ps, plan.Mnemonics)
}

// cmcpHintText is the key-hint line of a CMCP screen: gtf's own, so it shows in every theme. Not in
// focus: the keys that start it; in focus: the area and the keys valid there; a pending pack consent
// or a notice takes the line while it lasts.
func (c *Client) cmcpHintText(f *cmcpui.Frame, cs *cmcpScr) (string, string) {
	t, k := c.cmcpHintBase(f, cs)
	if n := c.scrolledBack(); n > 0 {
		t = fmt.Sprintf("SCROLLED BACK %d lines (wheel down, PgDn or End: live)   ", n) + t
	}
	return t, k
}

func (c *Client) cmcpHintBase(f *cmcpui.Frame, cs *cmcpScr) (string, string) {
	switch {
	case cs.alertPick != "":
		return c.cmcpAlertLine(cs, cs.alertLevel), "consent"
	case c.cmcpConsentLine(cs) != "":
		return c.cmcpConsentLine(cs), "consent"
	case cs.lockNote != "":
		return cs.lockNote, "notice"
	case cs.st.Active:
		return f.Header(), "focus"
	}
	return f.Header() + "   F3 log   F4 screen off   F5 alert   F6 close   /cmcp keys", "idle"
}

// cmcpWithHint returns the frame's grid with one more row at the bottom holding the hint line.
func (c *Client) cmcpWithHint(f *cmcpui.Frame, cs *cmcpScr, note string, nk cmcpui.NoticeKind, alert string, w, h int) *layout.Grid {
	src := f.Grid
	if src.H >= h {
		return src
	}
	// the screen is drawn one row down: row 0 belongs to the notice line (theme.NoticeRows = 1)
	dy := 0
	if h-src.H >= 2 {
		dy = 1
	}
	g := layout.NewGrid(w, h)
	copy(g.Cells[dy*w:], src.Cells)
	if dy > 0 {
		f.Text.Y += dy
		for i := range f.Items {
			f.Items[i].Rect.Y += dy
		}
		for i := range f.Boxes {
			f.Boxes[i].Y += dy
		}
	}
	txt, kind := c.cmcpHintText(f, cs)
	a := layout.Attr{FG: layout.Color{R: 200, G: 200, B: 205, Kind: layout.RGB}, BG: layout.Color{R: 30, G: 30, B: 38, Kind: layout.RGB}}
	switch kind {
	case "focus":
		a = layout.Attr{FG: layout.Color{R: 0, G: 0, B: 0, Kind: layout.RGB}, BG: layout.Color{R: 255, G: 255, B: 255, Kind: layout.RGB}, Bold: true}
	case "consent":
		a = layout.Attr{FG: layout.Color{R: 0, G: 0, B: 0, Kind: layout.RGB}, BG: layout.Color{R: 255, G: 200, B: 40, Kind: layout.RGB}, Bold: true}
	case "notice":
		a.Bold = true
	}
	if note != "" {
		// the notice line: the top row of the screen, the same in every theme
		na := layout.Attr{FG: layout.Color{R: 0, G: 0, B: 0, Kind: layout.RGB}, BG: layout.Color{R: 170, G: 200, B: 235, Kind: layout.RGB}, Bold: true}
		switch nk {
		case cmcpui.NoticeProgress:
			na.BG = layout.Color{R: 120, G: 170, B: 255, Kind: layout.RGB}
		case cmcpui.NoticeDone:
			na.BG = layout.Color{R: 110, G: 215, B: 130, Kind: layout.RGB}
		case cmcpui.NoticeFail:
			na.BG = layout.Color{R: 255, G: 90, B: 80, Kind: layout.RGB}
		}
		for x := 0; x < w; x++ {
			g.Set(x, 0, layout.Cell{R: ' ', A: na})
		}
		for i, r := range []rune(" " + note) {
			if i >= w {
				break
			}
			g.Set(i, 0, layout.Cell{R: r, A: na})
		}
	}
	y := h - 1
	for x := 0; x < w; x++ {
		g.Set(x, y, layout.Cell{R: ' ', A: a})
	}
	for i, r := range []rune(" " + txt) {
		if i >= w {
			break
		}
		g.Set(i, y, layout.Cell{R: r, A: a})
	}
	if al := strings.ToLower(alert); al == "red" || al == "yellow" {
		// the alert is always said in words on gtf's own line, whatever the theme does with colour
		lab := " " + strings.ToUpper(al) + " ALERT "
		bg := layout.Color{R: 255, G: 60, B: 50, Kind: layout.RGB}
		if al == "yellow" {
			bg = layout.Color{R: 255, G: 200, B: 40, Kind: layout.RGB}
		}
		aa := layout.Attr{FG: layout.Color{R: 0, G: 0, B: 0, Kind: layout.RGB}, BG: bg, Bold: true}
		for i, r := range []rune(lab) {
			if x := w - len(lab) + i; x >= 0 {
				g.Set(x, y, layout.Cell{R: r, A: aa})
			}
		}
	}
	return g
}

// cmcpAlertRecolour is %cmcp_alert_recolour: theme (default: the theme's own, edge for romulan and
// ferengi until the reference themes carry it), full, edge or off.
func (c *Client) cmcpAlertRecolour(th cmcpui.Theme) string {
	switch v := strings.ToLower(strings.TrimSpace(c.in.Var("cmcp_alert_recolour"))); v {
	case "full", "edge", "off":
		return v
	}
	if th.Pack != nil { // the theme pack's own alert_recolour field (spec 19.3)
		if v, _ := th.Pack.Doc["alert_recolour"].(string); v == "full" || v == "edge" || v == "off" {
			return v
		}
	}
	switch th.Style {
	case "romulan", "ferengi":
		return "edge"
	}
	return "full"
}

// cmcpDropStale forgets pack and theme questions that belong to another ship: the consent question
// is only ever about the current ship's packs (a question left from the previous ship must not
// survive the hop). A dropped one is asked again if the new ship announces the pack.
func (c *Client) cmcpDropStale(cs *cmcpScr, ship string) {
	for k, j := range cs.jobs {
		if j.state == "asking" && j.ship != "" && ship != "" && j.ship != ship {
			delete(cs.jobs, k)
		}
	}
}

// cmcpPulse is the red-alert pulse now (spec 19.3), from the core's pulse package: the phase counts
// from when the alert level started and a redraw is scheduled only for the next flip, so nothing
// repaints between flips. Steady when the alert is green, recolouring or motion is off, or the pulse is
// switched off (%cmcp_alert_pulse theme|on|off; a theme's alert_pulse field decides under `theme`).
func (c *Client) cmcpPulse(s *Session, cs *cmcpScr, th cmcpui.Theme, alert string, now time.Time) cmcpui.PulseSpec {
	lvl := strings.ToLower(alert)
	if lvl != cs.alertLevel {
		cs.alertLevel, cs.alertSince, cs.pulseAt = lvl, now, time.Time{}
	}
	tuning := pulse.Default()
	if th.Pack != nil {
		tuning = th.Pack.AlertPulse
	}
	reduced := strings.TrimSpace(c.in.Var("cmcp_motion")) != "" && !onVar(c.in.Var("cmcp_motion"))
	player := strings.ToLower(strings.TrimSpace(c.in.Var("cmcp_alert_pulse")))
	t := now.Sub(cs.alertSince)
	st := pulse.Compute(lvl, float64(t/time.Millisecond), "cell", tuning, player, false, reduced, c.cmcpAlertRecolour(th))
	if !st.Active {
		return cmcpui.PulseSpec{}
	}
	half := time.Duration(st.PeriodMS) * time.Millisecond / 2
	next := cs.alertSince.Add((t/half + 1) * half)
	if !next.Equal(cs.pulseAt) {
		cs.pulseAt = next
		time.AfterFunc(next.Sub(now)+5*time.Millisecond, c.scheduleRender)
	}
	return cmcpui.PulseSpec{Dim: st.Phase == "dim", Depth: 1 - st.Level}
}

// cmcpAlertKey is the alert control (spec 6.15): F5 or Alt+A opens the chooser on the next level
// (green > yellow > red > green) unless a pack question is pending (then F5 answers it); inside it
// Left/Right/Tab/F5 move the choice, Enter confirms (alert.set, echoed as `alert red`), Esc cancels.
// It reports whether it took the key.
func (c *Client) cmcpAlertKey(s *Session, name, raw string) bool {
	cs := s.cmScr()
	u := s.cm.UI()
	if cs.alertPick != "" {
		i := 0
		for j, l := range session.AlertLevels {
			if l == cs.alertPick {
				i = j
			}
		}
		n := len(session.AlertLevels)
		switch name {
		case "left", "up", "shift_tab":
			cs.alertPick = session.AlertLevels[(i-1+n)%n]
		case "right", "down", "tab", "f5":
			cs.alertPick = session.AlertLevels[(i+1)%n]
		case "enter":
			level := cs.alertPick
			cs.alertPick = ""
			cmd, args, echo := session.AlertCommand(u.Catalogue, level)
			if t, err := s.cm.Command(c.cfg.Now(), cmd, args); err != nil {
				c.cmcpNotice(s, err.Error())
			} else if t.Mutating && echo != "" {
				c.cmcpEcho(s, echo)
			}
			c.cmcpPump(s)
		default: // esc, or anything else, cancels; nothing was sent
			cs.alertPick = ""
		}
		c.scheduleRender()
		return true
	}
	if name != "f5" && name != "alt_a" || u.Console == nil {
		return false
	}
	if name == "f5" && c.cmcpConsentLine(cs) != "" {
		return false
	}
	st := session.AlertControl(u.Catalogue, u.Console.ID, strings.ToLower(u.Alert), u.AI, u.Readonly)
	if !st.Enabled {
		hint := ""
		if st.Hint != nil {
			hint = ": " + *st.Hint
		}
		c.cmcpNotice(s, "Alert control not available"+hint)
		return true
	}
	cs.alertPick = session.AlertNext(strings.ToLower(u.Alert))
	c.scheduleRender()
	return true
}

// cmcpAlertLine is the chooser as the hint line shows it.
func (c *Client) cmcpAlertLine(cs *cmcpScr, current string) string {
	var b strings.Builder
	b.WriteString("ALERT (now " + strings.ToUpper(current) + ")  ")
	for _, l := range session.AlertLevels {
		if l == cs.alertPick {
			b.WriteString("[" + strings.ToUpper(l) + "] ")
		} else {
			b.WriteString(" " + strings.ToUpper(l) + "  ")
		}
	}
	b.WriteString("  Left/Right choose   Enter confirm   Esc cancel")
	return b.String()
}

// cmcpRecolourWith is the recolour mode for this frame: the ambience draws the alert edge as in edge
// mode whatever alert_recolour says (spec 6.17.1), so a theme that does not recolour still shows it.
func (c *Client) cmcpRecolourWith(th cmcpui.Theme, look ambient.Look) string {
	mode := c.cmcpAlertRecolour(th)
	if mode == "off" && look.Edge != "" {
		return "edge"
	}
	return mode
}

// cmcpRoomVars lets a ship room's name stand where the console's mode name goes in a theme's band.
func (c *Client) cmcpRoomVars(u *session.UI, v map[string]any, title string) map[string]any {
	if n := len(u.Stack); n > 0 && u.Stack[n-1].Kind == "room" && v != nil {
		v["mode"] = title
	}
	return v
}

// cmcpClose is the close key and /cmcp close (spec 3.3.4): tell the game the player closed the screen
// and let it decide. gtf never hides the screen itself; the update that follows (a pop, or the console
// unmanned) does, and a refusal or silence shows an error notice (the core posts it).
func (c *Client) cmcpClose(s *Session) {
	if s == nil || s.cm == nil {
		return
	}
	now := c.cfg.Now()
	_, err := s.cm.CloseContext(now, session.CloseUser)
	switch {
	case err == nil:
	case errors.Is(err, session.ErrContextlessClient):
		// a game that does not speak contexts: the old way of leaving a console
		if t, e := s.cm.Command(now, "console.unman", nil); e != nil {
			c.cmcpNotice(s, "Close: "+e.Error())
		} else if t.Mutating && t.Echo != "" {
			c.cmcpEcho(s, t.Echo)
		}
	case errors.Is(err, session.ErrNoActiveContext):
		c.cmcpNotice(s, "Nothing to close")
	default:
		c.cmcpNotice(s, "Close: "+err.Error())
	}
	c.cmcpPump(s)
	c.scheduleRender()
}

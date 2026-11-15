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
	"github.com/itsnotyoutoday/cmcp/textport"
	"github.com/itsnotyoutoday/cmcp/theme"
	"os"
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
	st           *cmcpui.State
	states       map[string]*cmcpui.State // $ui, focus and cursors per screen: leaving a context keeps them (spec 3.3, 16.6)
	gen          int
	frame        *cmcpui.Frame
	override     string // a client-local {"show":...} screen, until the server names another
	lastSrv      string
	notice       string
	noticeEnd    time.Time
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
	told         bool            // the one-line CMCP notice was shown
	reenter      bool            // a screen switch from the menu keeps the keyboard focus on the new screen
	auto         map[string]bool // group pickers that already got their first group selected
}

func (s *Session) cmScr() *cmcpScr {
	if s.cs == nil {
		s.cs = &cmcpScr{view: textport.NewViewWith(onVar(s.c.in.Var("cmcp_start_collapsed")), textport.State(strings.ToLower(strings.TrimSpace(s.c.in.Var("cmcp_textport"))))), st: cmcpui.NewState(), sprites: raster.NewCache(32), packs: map[string]*pack.Pack{}, zonesFor: map[string][]string{}}
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
	if cs.reenter {
		cs.reenter = false
		if cs.st.Model != nil {
			cmcpui.Dispatch(&cmcpui.Frame{}, cs.st, cmcpui.FocusKeyName(c.in.Var("cmcp_key_focus")), true, now)
		}
	}
	th := c.cmcpResolveTheme(s, u, c.cmcpThemeFor(s, u, scr.SuggestTheme))
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
	textRows := c.cmcpTextRows(s, cs, w, h, th, now)
	tags := []string{}
	if len(u.Stack) > 1 {
		tags = append(tags, fmt.Sprintf("%d contexts", len(u.Stack)))
	}
	if u.Readonly {
		tags = append(tags, "read-only")
	}
	var nav []cmcpui.NavEntry
	for _, o := range u.Screens {
		nav = append(nav, cmcpui.NavEntry{ID: o.ID, Title: o.Title, Active: o.Active, Available: o.Available})
	}
	ce := cmcpui.Env{Nav: nav, Status: c.cmcpStatusStrip(u), Expanded: cs.view.State() == textport.Expanded && !cs.tween.active(now), TextRows: textRows, ScreenID: id, Vars: c.cmcpVars(u, id, scr.Title), Builtin: p != nil && p.Builtin, Title: scr.Title, Ship: u.Ship.Name, Tags: tags, ASCII: c.cmcpASCII(), Layout: strings.ToLower(strings.TrimSpace(c.in.Var("cmcp_layout"))), Mnemonics: plan.Mnemonics, Theme: th, Now: now, Alert: u.Alert, Header: hdr, Notice: notice, Effects: live,
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
	if s == nil || s.cm == nil {
		return false
	}
	name, ok := cmcpui.KeyName(raw)
	if !ok {
		return false
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
	u := s.cm.UI()
	if n < 0 || n >= len(u.Screens) {
		return false
	}
	c.cmcpNavErr(s, s.cm.ShowScreen(c.cfg.Now(), u.Screens[n].ID))
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

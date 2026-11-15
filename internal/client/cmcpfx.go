// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package client

import (
	"fmt"
	"strings"
	"time"

	"github.com/itsnotyoutoday/cmcp/ambient"
	"github.com/itsnotyoutoday/cmcp/session"
	"github.com/itsnotyoutoday/cmcp/shake"
	"github.com/itsnotyoutoday/gtf/internal/cmcpui"
	"github.com/itsnotyoutoday/gtf/internal/layout"
	"github.com/itsnotyoutoday/gtf/internal/ui"
)

// cmcpFxConfig gives the core the settings of the alert ambience (spec 6.17.1) and the hit shake (6.18):
// the theme's alert_ambient and hit_shake, the player's %cmcp_alert_ambient and %cmcp_hit_shake
// (theme|on|off) and reduced motion (%cmcp_motion off). It only calls the core when something changed
// (every call marks the UI dirty).
func (c *Client) cmcpFxConfig(s *Session, cs *cmcpScr, th cmcpui.Theme) {
	player := func(n string) string { return strings.ToLower(strings.TrimSpace(c.in.Var(n))) }
	reduced := strings.TrimSpace(c.in.Var("cmcp_motion")) != "" && !onVar(c.in.Var("cmcp_motion"))
	mode := c.cmcpAlertRecolour(th)
	key := fmt.Sprint(th.Name, "|", player("cmcp_alert_ambient"), "|", player("cmcp_hit_shake"), "|", reduced, "|", mode)
	if key == cs.fxKey {
		return
	}
	cs.fxKey = key
	at, st := ambient.Default(), shake.Default()
	edge, yellow := "", ""
	if th.Pack != nil {
		at, st = th.Pack.AlertAmbient, th.Pack.HitShake
		if r := th.Pack.Pal["red"]; r != nil {
			edge = r["r"]
		}
		if y := th.Pack.Pal["yellow"]; y != nil {
			yellow = y["r"]
		}
	}
	s.cm.SetAmbient(&session.AmbientConfig{Tuning: at, Player: player("cmcp_alert_ambient"), RecolourMode: mode, ThemeEdge: edge, YellowR: yellow})
	s.cm.SetShake(&session.ShakeConfig{Class: "cell", Tuning: st, Player: player("cmcp_hit_shake"), Reduced: reduced})
}

// shiftGrid moves everything but the text pane, the notice row and the hint row by (dx, dy) cells:
// the frame and the widgets jitter, the text and the input line never do.
func shiftGrid(g *layout.Grid, text cmcpui.Rect, dx, dy int) *layout.Grid {
	out := layout.NewGrid(g.W, g.H)
	bg := g.At(g.W/2, 2).A
	bg.Reverse, bg.Bold, bg.Underline = false, false, false
	for y := 1; y < g.H-1; y++ {
		for x := 0; x < g.W; x++ {
			out.Set(x, y, layout.Cell{R: ' ', A: layout.Attr{BG: bg.BG}})
		}
	}
	for y := 0; y < g.H; y++ {
		for x := 0; x < g.W; x++ {
			c := g.At(x, y)
			if y == 0 || y == g.H-1 || (x >= text.X && x < text.X+text.W && y >= text.Y && y < text.Y+text.H) {
				out.Set(x, y, c)
				continue
			}
			nx, ny := x+dx, y+dy
			if nx < 0 || nx >= g.W || ny < 1 || ny >= g.H-1 || (nx >= text.X && nx < text.X+text.W && ny >= text.Y && ny < text.Y+text.H) {
				continue
			}
			out.Set(nx, ny, c)
		}
	}
	return out
}

// cmcpShake is the hit shake of this frame: the one-cell step along the hit axis, with a redraw
// scheduled for the next 100 ms step while it lasts.
func (c *Client) cmcpShake(s *Session, now time.Time) (dx, dy int) {
	st := s.cm.Shake(now)
	if !st.Active {
		return 0, 0
	}
	time.AfterFunc(shake.CellStepMS*time.Millisecond+5*time.Millisecond, c.scheduleRender)
	return st.Step[0], st.Step[1]
}

// cmcpAmbientFrame is what a room, a dock or an away scene shows while the ship is in alert: no console
// screen, only the alert edge, a strip at the top and the bottom of the area in the alert colour, with
// the text between them (spec 6.17.1). It returns false when there is nothing to show.
func (c *Client) cmcpAmbientFrame(s *Session, u *session.UI, cs *cmcpScr, now time.Time) bool {
	look := s.cm.Ambient(now)
	if look.Edge == "" {
		return false
	}
	w, h := c.ui.W, c.ui.OutRows()
	if h < 8 {
		return false
	}
	g := layout.NewGrid(w, h)
	col := hexColor(look.Edge)
	if sp := c.cmcpPulse(s, cs, cmcpui.Theme{}, u.Alert, now); sp.Dim {
		col = layout.Lerp(layout.Color{Kind: layout.RGB}, col, 1-sp.Depth)
	}
	a := layout.Attr{FG: col, BG: layout.Color{Kind: layout.RGB}}
	label := strings.ToUpper(u.Alert) + " ALERT"
	for _, y := range []int{1, h - 2} {
		for x := 0; x < w; x++ {
			g.Set(x, y, layout.Cell{R: '▀', A: a})
		}
	}
	for i, r := range []rune(" " + label + " ") {
		g.Set(2+i, 1, layout.Cell{R: r, A: layout.Attr{FG: layout.Color{Kind: layout.RGB}, BG: col, Bold: true}})
	}
	text := layout.Rect{X: 0, Y: 2, W: w, H: h - 4}
	if dx, dy := c.cmcpShake(s, now); dx != 0 || dy != 0 {
		g = shiftGrid(g, cmcpui.Rect{X: text.X, Y: text.Y, W: text.W, H: text.H}, dx, dy)
	}
	if n := s.cm.Notice(now); n != nil {
		for i, r := range []rune(" " + n.Line()) {
			if i < w {
				g.Set(i, 0, layout.Cell{R: r, A: layout.Attr{FG: layout.Color{Kind: layout.RGB}, BG: layout.Color{Kind: layout.RGB, R: 170, G: 200, B: 235}, Bold: true}})
			}
		}
		time.AfterFunc(250*time.Millisecond, c.scheduleRender)
	}
	c.ui.Surf = &ui.Surface{Grid: g, Text: text}
	return true
}

func hexColor(h string) layout.Color {
	if c, err := layout.ParseColor(h); err == nil {
		return layout.RGBOf(c)
	}
	return layout.Color{Kind: layout.RGB, R: 255, G: 60, B: 50}
}

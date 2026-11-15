// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package cmcpui

import (
	"fmt"
	"math"

	"github.com/itsnotyoutoday/cmcp/pack"
)

// The helm controls are what gtf shows where a pack asks for a joystick: gtf has no stick, the
// ship is steered by ordinary commands. Heading, speed and warp are edited locally and sent with
// Enter as helm.heading, helm.speed and helm.warp; STOP and a course to the selected contact are
// buttons. Every control sends a catalogue verb the console lists (CanSend), nothing else.

const (
	hcAz = iota
	hcMark
	hcSpeed
	hcWarp
	hcEngage
	hcStop
	hcCourse
	hcDock
	hcUndock
	hcN
)

func (d *drawer) helmInit(b *pack.BoundNode, ws *wstate) {
	if ws.inited {
		return
	}
	br := asMap(prop(b, "bearing"))
	if br == nil {
		return
	}
	ws.az, ws.mark = numOr(br["az"], 0), numOr(br["mark"], 0)
	ws.warp = numOr(asMap(prop(b, "warp"))["factor"], 0)
	ws.inited = true
}

func (d *drawer) helmKeys(b *pack.BoundNode) func(Key) ([]Event, bool) {
	return func(k Key) ([]Event, bool) {
		ws := d.st.ws(b.Node.Path)
		d.helmInit(b, ws)
		stepDeg := numOr(prop(b, "step_deg"), 5)
		step := func(big, small float64) float64 {
			switch k {
			case "left":
				return -big
			case "right":
				return big
			case "-":
				return -small
			case "+", "=":
				return small
			}
			return 0
		}
		ev := func(name string, v any) ([]Event, bool) {
			return []Event{{Node: b.Node, Name: name, Value: v}}, true
		}
		enter := k == "enter" || k == "space"
		switch k {
		case "up":
			if ws.cur > 0 {
				ws.cur--
				return nil, true
			}
			return nil, false
		case "down":
			if ws.cur < hcN-1 {
				ws.cur++
				return nil, true
			}
			return nil, false
		}
		sel := prop(b, "selected")
		switch ws.cur {
		case hcAz, hcMark:
			f := &ws.az
			lim := 360.0
			if ws.cur == hcMark {
				f, lim = &ws.mark, 90
			}
			if dv := step(stepDeg, 1); dv != 0 {
				if ws.cur == hcAz {
					*f = math.Mod(*f+dv+360, 360)
				} else {
					*f = math.Max(-lim, math.Min(lim, *f+dv))
				}
				return nil, true
			}
			if enter {
				return ev("heading", map[string]any{"az": ws.az, "mark": ws.mark})
			}
		case hcSpeed:
			if dv := step(10, 1); dv != 0 {
				ws.speed = math.Max(0, math.Min(100, ws.speed+dv))
				return nil, true
			}
			if enter {
				return ev("speed", ws.speed)
			}
		case hcWarp:
			mx := numOr(asMap(prop(b, "warp"))["max"], 10)
			if dv := step(1, 0.1); dv != 0 {
				ws.warp = math.Max(0, math.Min(mx, math.Round((ws.warp+dv)*10)/10))
				return nil, true
			}
			if enter {
				return ev("warp", ws.warp)
			}
		case hcEngage:
			if k == "left" || k == "right" || k == "-" || k == "+" {
				ws.stage = 1 - ws.stage
				return nil, true
			}
			if enter {
				return ev("engage", []string{"impulse", "warp"}[ws.stage])
			}
		case hcStop:
			if enter {
				return ev("stop", nil)
			}
		case hcCourse, hcDock:
			if enter && sel != nil {
				return ev(map[int]string{hcCourse: "course", hcDock: "dock"}[ws.cur], sel)
			}
			if enter {
				return nil, true
			}
		case hcUndock:
			if enter {
				return ev("undock", nil)
			}
		}
		return nil, false
	}
}

func (d *drawer) helmControls(b *pack.BoundNode, r Rect, foc bool) {
	th := d.env.Theme
	ws := d.st.ws(b.Node.Path)
	d.helmInit(b, ws)
	y := r.Y
	hdr := th.accent()
	hdr.Bold = true
	d.c.text(r.X, y, "HELM CONTROLS", hdr, r.W)
	y++
	now := fmt.Sprintf("now: bearing %s  speed %s m/s  warp %s", str(prop(b, "bearing")), fmtNum(round1(numOr(prop(b, "speed_mps"), 0))), fmtNum(numOr(asMap(prop(b, "warp"))["factor"], 0)))
	d.c.text(r.X, y, now, th.dim(), r.W)
	y++
	row := func(i int, ev, label, val, hint string) {
		if y >= r.Y+r.H {
			return
		}
		a := th.text()
		if _, ok := b.Node.On[ev]; !ok {
			a = th.dim() // the pack maps no command to it
		}
		mk := ' '
		if foc && ws.cur == i {
			mk, a = '>', focused(th.accent())
		}
		d.c.put(r.X, y, mk, a)
		d.c.text(r.X+2, y, fmt.Sprintf("%-10s %-8s %s", label, val, hint), a, r.W-2)
		y++
	}
	sel := "none (select a contact)"
	if c := prop(b, "selected"); c != nil {
		sel = "contact " + str(c)
	}
	row(hcAz, "heading", "azimuth", fmtNum(ws.az), fmt.Sprintf("Left/Right %s, +/- 1, Enter sends heading", fmtNum(numOr(prop(b, "step_deg"), 5))))
	row(hcMark, "heading", "mark", fmtNum(ws.mark), "elevation -90..90, Enter sends heading")
	row(hcSpeed, "speed", "speed %", fmtNum(ws.speed), "Left/Right 10, Enter sends speed")
	row(hcWarp, "warp", "warp", fmtNum(ws.warp), "Left/Right 1, +/- 0.1, Enter sends warp")
	row(hcEngage, "engage", "engage", []string{"impulse", "warp"}[ws.stage], "Left/Right chooses, Enter engages")
	row(hcStop, "stop", "[ STOP ]", "", "all stop")
	row(hcCourse, "course", "[ COURSE ]", "", sel)
	row(hcDock, "dock", "[ DOCK ]", "", sel)
	row(hcUndock, "undock", "[ UNDOCK ]", "", "")
}

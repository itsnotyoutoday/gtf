// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package layout

import (
	"math"
	"strconv"
	"strings"
)

// args splits widget arguments into positionals and key=value options.
func widgetArgs(a []string) (pos []string, kw map[string]string) {
	kw = map[string]string{}
	for _, s := range a {
		if i := strings.IndexByte(s, '='); i > 0 && !strings.ContainsAny(s[:i], ":.#\"") {
			kw[s[:i]] = s[i+1:]
		} else {
			pos = append(pos, s)
		}
	}
	return
}

func (c *Ctx) num(ref string) (float64, bool) {
	v, _ := c.lookup(ref)
	return toFloat(v)
}

func intOpt(kw map[string]string, k string, def int) int {
	if v, ok := kw[k]; ok {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

var partials = []rune{' ', '▏', '▎', '▍', '▌', '▋', '▊', '▉'}

func (c *Ctx) widget(w tWidget) {
	pos, kw := widgetArgs(w.args)
	switch w.name {
	case "br":
		c.emit("\n")
	case "hr":
		ch := "─"
		if len(pos) > 0 {
			ch = pos[0]
		}
		n := c.Width
		if n <= 0 {
			n = 10
		}
		if v := intOpt(kw, "width", 0); v > 0 {
			n = v
		}
		c.emit(strings.Repeat(ch, n))
	case "bar":
		cur, max := c.barArgs(pos)
		c.bar(cur, max, kw)
	case "gauge":
		cur, max := c.barArgs(pos)
		c.gauge(cur, max, kw, pos)
	case "spark":
		c.spark(pos, kw)
	}
}

func (c *Ctx) barArgs(pos []string) (cur, max float64) {
	max = 100
	if len(pos) > 0 {
		cur, _ = c.num(pos[0])
	}
	if len(pos) > 1 {
		if m, ok := c.num(pos[1]); ok {
			max = m
		}
	}
	return
}

func autoColor(frac float64) string {
	switch {
	case frac > 0.5:
		return "green"
	case frac > 0.25:
		return "yellow"
	}
	return "red"
}

func (c *Ctx) bar(cur, max float64, kw map[string]string) {
	width := intOpt(kw, "width", 10)
	if width < 1 {
		width = 1
	}
	frac := 0.0
	if max > 0 {
		frac = cur / max
	}
	frac = math.Max(0, math.Min(1, frac))
	col := kw["color"]
	if col == "" || col == "auto" {
		col = autoColor(frac)
	}
	fg, _ := ParseColor(col)
	bg, _ := ParseColor(kw["bg"])
	emptyCol, _ := ParseColor(kw["empty"])
	if kw["empty"] == "" {
		emptyCol = Color{Kind: ANSI, N: 8}
	}
	fill, empty := "█", "░"
	switch kw["chars"] {
	case "ascii":
		fill, empty = "#", "-"
	case "line":
		fill, empty = "━", "─"
	case "block":
		fill, empty = "█", " "
	}
	if v := kw["fill"]; v != "" {
		fill = v
	}
	if v := kw["blank"]; v != "" {
		empty = v
	}
	eighths := int(math.Round(frac * float64(width) * 8))
	full := eighths / 8
	part := eighths % 8
	fa := Attr{FG: fg, BG: bg}
	ea := Attr{FG: emptyCol, BG: bg}
	used := 0
	for i := 0; i < full; i++ {
		c.emitWith(fill, fa)
		used++
	}
	if part > 0 && used < width && fill == "█" && kw["chars"] == "" {
		c.emitWith(string(partials[part]), fa)
		used++
	}
	for ; used < width; used++ {
		c.emitWith(empty, ea)
	}
}

func (c *Ctx) gauge(cur, max float64, kw map[string]string, pos []string) {
	if l := kw["label"]; l != "" {
		c.emit(l + " ")
	}
	if kw["brackets"] != "off" {
		c.emit("[")
	}
	c.bar(cur, max, kw)
	if kw["brackets"] != "off" {
		c.emit("]")
	}
	if kw["nums"] != "off" {
		c.emit(" " + toString(cur) + "/" + toString(max))
	}
	if kw["pct"] == "on" && max > 0 {
		c.emit(" " + strconv.Itoa(int(math.Round(cur/max*100))) + "%")
	}
}

var sparkChars = []rune("▁▂▃▄▅▆▇█")

func (c *Ctx) spark(pos []string, kw map[string]string) {
	if len(pos) == 0 || c.Hist == nil {
		return
	}
	c.lookup(pos[0]) // register the dependency
	h := c.Hist(pos[0])
	width := intOpt(kw, "width", 16)
	if len(h) > width {
		h = h[len(h)-width:]
	}
	if len(h) == 0 {
		return
	}
	lo, hi := h[0], h[0]
	for _, v := range h {
		lo, hi = math.Min(lo, v), math.Max(hi, v)
	}
	if mn, ok := kw["min"]; ok {
		lo, _ = strconv.ParseFloat(mn, 64)
	}
	if mx, ok := kw["max"]; ok {
		hi, _ = strconv.ParseFloat(mx, 64)
	}
	fg, _ := ParseColor(kw["color"])
	a := Attr{FG: fg}
	for _, v := range h {
		idx := 0
		if hi > lo {
			idx = int(math.Round((v - lo) / (hi - lo) * 7))
		}
		idx = max(0, min(7, idx))
		c.emitWith(string(sparkChars[idx]), a)
	}
}

// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

// Package cmcpui is the cell renderer of CMCP (spec 6.10, class "cell"): it draws a bound
// screen tree (cmcp/go/pack) as a grid of 24-bit UTF-8 cells and operates its widgets from the
// keyboard. It knows no protocol: evaluated props go in, cells and widget events come out.
package cmcpui

import (
	"strings"

	"github.com/itsnotyoutoday/gtf/internal/layout"
)

// Theme maps the design tokens of spec 6.7 to colours and says how the screen is framed and how
// its controls and pictures look (docs/CMCP-THEMES.md). The plain theme leaves backgrounds to the
// terminal and draws no frame; the others paint their own ground, frame, pills and radar.
type Theme struct {
	Name   string
	Tokens map[string]string // "color.accent" -> "#rrggbb" (or a colour name)
	// PaintBG makes the screen draw color.bg behind everything but the text pane.
	PaintBG bool
	// Style names the frame and control look ("" = none): tng, klingon, romulan, ferengi, tos, soft, ops.
	Style string
	// Cycle are the colours pills, tabs and bars take in turn (the LCARS rainbow of a console).
	Cycle []string
	// Radar is the sensor picture: dish, hex or rings.
	Radar string
}

var themes = map[string]Theme{
	"plain": {Name: "plain", Radar: "dish", Tokens: map[string]string{
		"color.text": "default", "color.dim": "gray", "color.accent": "cyan", "color.accent2": "blue",
		"color.ok": "green", "color.warn": "yellow", "color.alert": "red", "color.panel": "default", "color.bg": "default"}},
	"tng": {Name: "tng", PaintBG: true, Style: "tng", Radar: "dish",
		Cycle: []string{"#ff9900", "#cc99cc", "#9999ff", "#ffcc66", "#ff99cc", "#cc6666"},
		Tokens: map[string]string{
			"color.bg": "#000000", "color.panel": "#0a0a14", "color.text": "#ffe0b0", "color.dim": "#9a8a78",
			"color.accent": "#ff9900", "color.accent2": "#9999ff", "color.ok": "#66dd88", "color.warn": "#ffcc33", "color.alert": "#ff3030"}},
	"klingon": {Name: "klingon", PaintBG: true, // frame and control skins wait for the theme-pack format (docs/CMCP-THEMES.md)
		Radar: "hex",
		Cycle: []string{"#c01828", "#d4b86a", "#8a1020", "#a89050"},
		Tokens: map[string]string{
			"color.bg": "#060203", "color.panel": "#180a0c", "color.text": "#e8d8c8", "color.dim": "#8a6a6a",
			"color.accent": "#d02030", "color.accent2": "#d4b86a", "color.ok": "#88cc55", "color.warn": "#e0a020", "color.alert": "#ff4040"}},
	"romulan": {Name: "romulan", PaintBG: true, // frame and control skins wait for the theme-pack format (docs/CMCP-THEMES.md)
		Radar: "rings",
		Cycle: []string{"#40c070", "#f0b040", "#2a9a58", "#c89030"},
		Tokens: map[string]string{
			"color.bg": "#020805", "color.panel": "#0a1610", "color.text": "#d0ecd8", "color.dim": "#6a8a74",
			"color.accent": "#40c070", "color.accent2": "#f0b040", "color.ok": "#a0e080", "color.warn": "#d0c040", "color.alert": "#ff6a3a"}},
	"ferengi": {Name: "ferengi", PaintBG: true, // frame and control skins wait for the theme-pack format (docs/CMCP-THEMES.md)
		Radar: "dish",
		Cycle: []string{"#e0b020", "#b8741c", "#8a2a2a", "#f0d060"},
		Tokens: map[string]string{
			"color.bg": "#0a0604", "color.panel": "#1a1008", "color.text": "#f4e8c8", "color.dim": "#9a8a60",
			"color.accent": "#e0b020", "color.accent2": "#b8741c", "color.ok": "#90d060", "color.warn": "#f0c040", "color.alert": "#ff5030"}},
	"tos": {Name: "tos", PaintBG: true, // frame and control skins wait for the theme-pack format (docs/CMCP-THEMES.md)
		Radar: "dish",
		Cycle: []string{"#d8392b", "#f2c230", "#2f6fd0", "#e8e4d8"},
		Tokens: map[string]string{
			"color.bg": "#101214", "color.panel": "#3a3f46", "color.text": "#f0ece0", "color.dim": "#9aa0a8",
			"color.accent": "#f2c230", "color.accent2": "#2f6fd0", "color.ok": "#5cc060", "color.warn": "#f2c230", "color.alert": "#ff2a1f"}},
	"soft": {Name: "soft", PaintBG: true, // frame and control skins wait for the theme-pack format (docs/CMCP-THEMES.md)
		Radar: "dish",
		Cycle: []string{"#8fb8c8", "#c8a0b8", "#a8c090", "#d8c090"},
		Tokens: map[string]string{
			"color.bg": "#12141a", "color.panel": "#1c2028", "color.text": "#d8dce4", "color.dim": "#7a8494",
			"color.accent": "#8fb8c8", "color.accent2": "#c8a0b8", "color.ok": "#90c090", "color.warn": "#d8c090", "color.alert": "#e07070"}},
	"ops": {Name: "ops", PaintBG: true, // frame and control skins wait for the theme-pack format (docs/CMCP-THEMES.md)
		Radar: "dish",
		Cycle: []string{"#33ccff", "#66dd88", "#ffcc33", "#ff6644"},
		Tokens: map[string]string{
			"color.bg": "#05080c", "color.panel": "#0c1218", "color.text": "#d8e4f0", "color.dim": "#6a7a8a",
			"color.accent": "#33ccff", "color.accent2": "#66dd88", "color.ok": "#66dd88", "color.warn": "#ffcc33", "color.alert": "#ff4444"}},
}

// ThemeNames lists the themes in a stable order.
func ThemeNames() []string {
	return []string{"plain", "tng", "klingon", "romulan", "ferengi", "tos", "soft", "ops"}
}

// ThemeByName returns a theme (plain for an unknown name).
func ThemeByName(name string) Theme {
	if t, ok := themes[strings.ToLower(name)]; ok {
		return t
	}
	return themes["plain"]
}

// Token resolves a token name such as "color.accent" to a colour.
func (t Theme) Token(name string) layout.Color {
	v, ok := t.Tokens[name]
	if !ok {
		v = themes["plain"].Tokens[name]
	}
	c, err := layout.ParseColor(v)
	if err != nil {
		return layout.Color{}
	}
	return c
}

// Tokens returns the theme's tokens as the map the pack package resolves against.
func (t Theme) TokenMap() map[string]any {
	m := map[string]any{}
	for k, v := range t.Tokens {
		if v != "default" {
			m[k] = v
		}
	}
	return m
}

// tone names a pack tone ("accent", "alert", ...) as a colour.
func (t Theme) tone(name string) layout.Color {
	switch strings.ToLower(name) {
	case "alert", "red", "danger":
		return t.Token("color.alert")
	case "warn", "warning", "yellow":
		return t.Token("color.warn")
	case "ok", "good", "green":
		return t.Token("color.ok")
	case "dim":
		return t.Token("color.dim")
	case "accent2", "secondary":
		return t.Token("color.accent2")
	case "text":
		return t.Token("color.text")
	}
	return t.Token("color.accent")
}

func (t Theme) attr(c layout.Color) layout.Attr {
	a := layout.Attr{FG: c}
	if t.PaintBG {
		a.BG = t.Token("color.bg")
	}
	return a
}

func (t Theme) text() layout.Attr   { return t.attr(t.Token("color.text")) }
func (t Theme) dim() layout.Attr    { return t.attr(t.Token("color.dim")) }
func (t Theme) accent() layout.Attr { return t.attr(t.Token("color.accent")) }
func (t Theme) blank() layout.Attr {
	if t.PaintBG {
		return layout.Attr{BG: t.Token("color.bg")}
	}
	return layout.Attr{}
}

// focused turns an attribute into its focused look: reverse video keeps working at any depth.
func focused(a layout.Attr) layout.Attr {
	a.Reverse = true
	a.Bold = true
	return a
}

// RGB resolves a token to a truecolor value (ANSI names become their xterm RGB).
func (t Theme) RGB(name string) layout.Color { return layout.RGBOf(t.Token(name)) }

// cycle returns the i-th colour of the pill rainbow (the accent colour for a theme without one).
func (t Theme) cycle(i int) layout.Color {
	if len(t.Cycle) == 0 {
		return t.RGB("color.accent")
	}
	c, err := layout.ParseColor(t.Cycle[((i%len(t.Cycle))+len(t.Cycle))%len(t.Cycle)])
	if err != nil {
		return t.RGB("color.accent")
	}
	return c
}

// dark is the colour of text on a coloured pill: near black.
var dark = layout.Color{Kind: layout.RGB, R: 8, G: 8, B: 16}

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

// Theme maps the design tokens of spec 6.7 to colours. The plain theme leaves backgrounds to the
// terminal; the accent themes paint the panels' own background.
type Theme struct {
	Name   string
	Tokens map[string]string // "color.accent" -> "#rrggbb" (or a colour name)
	// PaintBG makes the screen draw color.bg behind everything but the text pane.
	PaintBG bool
}

var themes = map[string]Theme{
	"plain": {Name: "plain", Tokens: map[string]string{
		"color.text": "default", "color.dim": "gray", "color.accent": "cyan", "color.accent2": "blue",
		"color.ok": "green", "color.warn": "yellow", "color.alert": "red", "color.panel": "default", "color.bg": "default"}},
	"tng": {Name: "tng", PaintBG: true, Tokens: map[string]string{
		"color.bg": "#06080c", "color.panel": "#0e1218", "color.text": "#e8e0d0", "color.dim": "#7a8a9a",
		"color.accent": "#ff9900", "color.accent2": "#cc99cc", "color.ok": "#66dd88", "color.warn": "#ffcc33", "color.alert": "#ff3030"}},
	"klingon": {Name: "klingon", PaintBG: true, Tokens: map[string]string{
		"color.bg": "#0c0405", "color.panel": "#180a0c", "color.text": "#e8d8d0", "color.dim": "#8a6a6a",
		"color.accent": "#d02020", "color.accent2": "#c08030", "color.ok": "#88cc55", "color.warn": "#e0a020", "color.alert": "#ff4040"}},
	"romulan": {Name: "romulan", PaintBG: true, Tokens: map[string]string{
		"color.bg": "#040a06", "color.panel": "#0a1610", "color.text": "#d8e8dc", "color.dim": "#6a8a74",
		"color.accent": "#40c070", "color.accent2": "#80d0a0", "color.ok": "#a0e080", "color.warn": "#d0c040", "color.alert": "#ff5050"}},
	"ferengi": {Name: "ferengi", PaintBG: true, Tokens: map[string]string{
		"color.bg": "#0a0804", "color.panel": "#161208", "color.text": "#f0e8d0", "color.dim": "#9a8a60",
		"color.accent": "#e0b020", "color.accent2": "#c07020", "color.ok": "#90d060", "color.warn": "#f0c040", "color.alert": "#ff5030"}},
}

// ThemeNames lists the themes in a stable order.
func ThemeNames() []string { return []string{"plain", "tng", "klingon", "romulan", "ferengi"} }

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

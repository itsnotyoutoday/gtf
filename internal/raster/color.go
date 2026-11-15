// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package raster

import (
	"encoding/json"
	"strconv"
	"strings"
)

// Color is a straight (non-premultiplied) 8-bit RGBA colour.
type Color struct{ R, G, B, A uint8 }

var namedColors = map[string]Color{
	"black": {0, 0, 0, 255}, "white": {255, 255, 255, 255}, "red": {255, 0, 0, 255},
	"green": {0, 128, 0, 255}, "lime": {0, 255, 0, 255}, "blue": {0, 0, 255, 255},
	"yellow": {255, 255, 0, 255}, "cyan": {0, 255, 255, 255}, "magenta": {255, 0, 255, 255},
	"gray": {128, 128, 128, 255}, "grey": {128, 128, 128, 255}, "orange": {255, 165, 0, 255},
	"purple": {128, 0, 128, 255}, "transparent": {0, 0, 0, 0},
}

// ParseColor reads #rgb, #rgba, #rrggbb, #rrggbbaa or a small set of names.
func ParseColor(s string) (Color, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if c, ok := namedColors[s]; ok {
		return c, nil
	}
	if !strings.HasPrefix(s, "#") {
		return Color{}, invalidErr("bad colour %q", s)
	}
	h := s[1:]
	hex := func(i, n int) (uint8, bool) {
		v, err := strconv.ParseUint(h[i:i+n], 16, 8)
		if err != nil {
			return 0, false
		}
		if n == 1 {
			v *= 17
		}
		return uint8(v), true
	}
	var c Color
	var ok [4]bool
	switch len(h) {
	case 3, 4:
		c.R, ok[0] = hex(0, 1)
		c.G, ok[1] = hex(1, 1)
		c.B, ok[2] = hex(2, 1)
		c.A, ok[3] = 255, true
		if len(h) == 4 {
			c.A, ok[3] = hex(3, 1)
		}
	case 6, 8:
		c.R, ok[0] = hex(0, 2)
		c.G, ok[1] = hex(2, 2)
		c.B, ok[2] = hex(4, 2)
		c.A, ok[3] = 255, true
		if len(h) == 8 {
			c.A, ok[3] = hex(6, 2)
		}
	default:
		return Color{}, invalidErr("bad colour %q", s)
	}
	if !(ok[0] && ok[1] && ok[2] && ok[3]) {
		return Color{}, invalidErr("bad colour %q", s)
	}
	return c, nil
}

// String formats the colour as #rrggbb or #rrggbbaa.
func (c Color) String() string {
	const d = "0123456789abcdef"
	b := []byte{'#', d[c.R>>4], d[c.R&15], d[c.G>>4], d[c.G&15], d[c.B>>4], d[c.B&15]}
	if c.A != 255 {
		b = append(b, d[c.A>>4], d[c.A&15])
	}
	return string(b)
}

// UnmarshalJSON accepts a colour string.
func (c *Color) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return invalidErr("colour must be a string")
	}
	v, err := ParseColor(s)
	if err != nil {
		return err
	}
	*c = v
	return nil
}

// MarshalJSON writes the colour as a string.
func (c Color) MarshalJSON() ([]byte, error) { return json.Marshal(c.String()) }

func mul255(a, b int) int { return (a*b + 127) / 255 }

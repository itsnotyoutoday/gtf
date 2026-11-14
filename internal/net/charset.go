// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package net

import (
	"unicode/utf8"
)

// decode converts bytes from the server's charset to UTF-8. carry holds an
// incomplete trailing multibyte sequence from the previous call. Invalid
// UTF-8 bytes are tolerated: each is read as a Latin-1 character.
func decode(utf8Mode bool, carry, data []byte) (out, newCarry []byte) {
	if len(carry) > 0 {
		data = append(append([]byte(nil), carry...), data...)
	}
	out = make([]byte, 0, len(data)+8)
	if !utf8Mode {
		for _, b := range data {
			out = utf8.AppendRune(out, rune(b))
		}
		return out, nil
	}
	for i := 0; i < len(data); {
		b := data[i]
		if b < utf8.RuneSelf {
			out = append(out, b)
			i++
			continue
		}
		if !utf8.FullRune(data[i:]) {
			return out, append([]byte(nil), data[i:]...)
		}
		r, n := utf8.DecodeRune(data[i:])
		if r == utf8.RuneError && n == 1 {
			out = utf8.AppendRune(out, rune(b)) // Latin-1 fallback
			i++
			continue
		}
		out = append(out, data[i:i+n]...)
		i += n
	}
	return out, nil
}

// encode converts UTF-8 text to the server's charset (Latin-1 replaces
// unrepresentable characters with '?').
func encode(utf8Mode bool, s string) []byte {
	if utf8Mode {
		return []byte(s)
	}
	out := make([]byte, 0, len(s))
	for _, r := range s {
		if r < 256 {
			out = append(out, byte(r))
		} else {
			out = append(out, '?')
		}
	}
	return out
}

package gtfparser

import (
	"regexp"
	"strings"
	"unicode"
)

// rx is the regexp seam: everything in the interpreter that needs a regular
// expression goes through compileRegexp/match, so the engine can be swapped
// (the pcre package replaces this stand-in).
type rx struct{ re *regexp.Regexp }

// compileRegexp compiles with TinyFugue's rules: dot matches newline, and
// matching is caseless unless the pattern has an uppercase letter.
func compileRegexp(pat string) (*rx, error) {
	flags := "(?s"
	caseless := true
	for i := 0; i < len(pat); i++ {
		if pat[i] == '\\' {
			i++
			continue
		}
		if unicode.IsUpper(rune(pat[i])) {
			caseless = false
			break
		}
	}
	if caseless {
		flags += "i"
	}
	re, err := regexp.Compile(flags + ")" + pat)
	if err != nil {
		return nil, err
	}
	return &rx{re}, nil
}

// match returns the capture offsets (pairs, -1 = unset) or nil.
func (r *rx) match(s string, start int) []int {
	loc := r.re.FindStringSubmatchIndex(s[start:])
	if loc == nil {
		return nil
	}
	for i := range loc {
		if loc[i] >= 0 {
			loc[i] += start
		}
	}
	return loc
}

var _ = strings.TrimSpace

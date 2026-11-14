package gtfparser

import (
	"fmt"

	"github.com/itsnotyoutoday/gtf/pcre"
)

// rx is the regexp seam: everything in the interpreter that needs a regular
// expression goes through compileRegexp/match. It is backed by the pcre
// package, a PCRE2-compatible engine configured as TinyFugue configures
// PCRE2 (dot matches newline, "$" matches only at the very end, smart-case,
// UTF-8 with Unicode properties).
type rx struct{ re *pcre.Regexp }

var rxCache = map[string]*rx{}

func compileRegexp(pat string) (*rx, error) {
	if r, ok := rxCache[pat]; ok {
		return r, nil
	}
	re, err := pcre.CompileTF(pat)
	if err != nil {
		if pe, ok := err.(*pcre.Error); ok {
			return nil, fmt.Errorf("regexp error at character %d: %s", pe.Offset, pe.Msg)
		}
		return nil, err
	}
	if len(rxCache) > 2048 {
		rxCache = map[string]*rx{}
	}
	r := &rx{re}
	rxCache[pat] = r
	return r, nil
}

// match returns the capture offsets (byte pairs, -1 = unset) or nil.
func (r *rx) match(s string, start int) []int {
	return r.re.Match(s, start, start > 0)
}

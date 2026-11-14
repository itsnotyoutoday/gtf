package ui

import (
	"strings"
	"unicode"
)

// Input is the line editor behind the input window: a rune buffer with a
// cursor ("point"), an input history, and the editing primitives the kb*()
// functions and /dokey need.
type Input struct {
	buf   []rune
	pt    int
	hist  []string
	hpos  int // index into hist while recalling; len(hist) = the live line
	draft string
	// WordPunct are the characters that are word characters besides
	// alphanumerics (tf's wordpunct variable).
	WordPunct string
	// Overwrite replaces instead of inserting (tf: insert=off).
	Overwrite bool
	// MaxHist bounds the history (0 = 1000).
	MaxHist int
}

// NewInput makes an empty editor.
func NewInput() *Input { return &Input{WordPunct: "_"} }

// Text is the current line (Editor).
func (in *Input) Text() string { return string(in.buf) }

// Cursor is the cursor's byte offset (Editor).
func (in *Input) Cursor() int { return len(string(in.buf[:in.pt])) }

// String is the current line.
func (in *Input) String() string { return string(in.buf) }

// Len is the line length in runes.
func (in *Input) Len() int { return len(in.buf) }

// Point is the cursor position.
func (in *Input) Point() int { return in.pt }

// Head is the text before the cursor.
func (in *Input) Head() string { return string(in.buf[:in.pt]) }

// Tail is the text after the cursor.
func (in *Input) Tail() string { return string(in.buf[in.pt:]) }

// Set replaces the line and puts the cursor at the end.
func (in *Input) Set(s string) {
	in.buf = []rune(s)
	in.pt = len(in.buf)
}

// Clear empties the line.
func (in *Input) Clear() { in.buf, in.pt = nil, 0; in.hpos = len(in.hist) }

// Insert types text at the cursor.
func (in *Input) Insert(s string) {
	r := []rune(s)
	if in.Overwrite && in.pt < len(in.buf) {
		for i := 0; i < len(r) && in.pt+i < len(in.buf); i++ {
			in.buf[in.pt+i] = r[i]
		}
		n := len(in.buf) - in.pt
		if len(r) > n {
			in.buf = append(in.buf, r[n:]...)
		}
		in.pt += len(r)
		return
	}
	nb := make([]rune, 0, len(in.buf)+len(r))
	nb = append(nb, in.buf[:in.pt]...)
	nb = append(nb, r...)
	nb = append(nb, in.buf[in.pt:]...)
	in.buf = nb
	in.pt += len(r)
}

// Goto moves the cursor, clamped to the line; it returns the new position.
func (in *Input) Goto(n int) int {
	if n < 0 {
		n = 0
	}
	if n > len(in.buf) {
		n = len(in.buf)
	}
	in.pt = n
	return n
}

// Delete removes the text between the cursor and position n and returns the
// new cursor position (kbdel).
func (in *Input) Delete(n int) int {
	if n < 0 {
		n = 0
	}
	if n > len(in.buf) {
		n = len(in.buf)
	}
	a, b := in.pt, n
	if a > b {
		a, b = b, a
	}
	in.buf = append(in.buf[:a:a], in.buf[b:]...)
	in.pt = a
	return a
}

// CharBack is the position n characters left of the cursor.
func (in *Input) CharBack(n int) int { return max(in.pt-n, 0) }

// CharFwd is the position n characters right of the cursor.
func (in *Input) CharFwd(n int) int { return min(in.pt+n, len(in.buf)) }

func (in *Input) isWord(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune(in.WordPunct, r)
}

// WordLeft is the start of the word left of i.
func (in *Input) WordLeft(i int) int {
	i = min(max(i, 0), len(in.buf))
	for i > 0 && !in.isWord(in.buf[i-1]) {
		i--
	}
	for i > 0 && in.isWord(in.buf[i-1]) {
		i--
	}
	return i
}

// WordRight is the position just past the end of the word right of i.
func (in *Input) WordRight(i int) int {
	i = min(max(i, 0), len(in.buf))
	for i < len(in.buf) && !in.isWord(in.buf[i]) {
		i++
	}
	for i < len(in.buf) && in.isWord(in.buf[i]) {
		i++
	}
	return i
}

// Match finds one of ()[]{} at or right of i and returns its partner's
// position, or -1.
func (in *Input) Match(i int) int {
	const open, closeS = "([{", ")]}"
	for ; i < len(in.buf); i++ {
		c := in.buf[i]
		if k := strings.IndexRune(open, c); k >= 0 {
			depth := 0
			for j := i; j < len(in.buf); j++ {
				if in.buf[j] == c {
					depth++
				} else if in.buf[j] == rune(closeS[k]) {
					if depth--; depth == 0 {
						return j
					}
				}
			}
			return -1
		}
		if k := strings.IndexRune(closeS, c); k >= 0 {
			depth := 0
			for j := i; j >= 0; j-- {
				if in.buf[j] == c {
					depth++
				} else if in.buf[j] == rune(open[k]) {
					if depth--; depth == 0 {
						return j
					}
				}
			}
			return -1
		}
	}
	return -1
}

// History returns the input history, oldest first.
func (in *Input) History() []string { return append([]string(nil), in.hist...) }

// AddHistory records a line as if it had been entered.
func (in *Input) AddHistory(s string) {
	if s == "" {
		return
	}
	if n := len(in.hist); n == 0 || in.hist[n-1] != s {
		in.hist = append(in.hist, s)
	}
	m := in.MaxHist
	if m <= 0 {
		m = 1000
	}
	if len(in.hist) > m {
		in.hist = in.hist[len(in.hist)-m:]
	}
	in.hpos = len(in.hist)
}

// Submit returns the line, records it in the history and clears the editor.
func (in *Input) Submit() string {
	s := in.String()
	in.AddHistory(s)
	in.buf, in.pt = nil, 0
	in.hpos = len(in.hist)
	in.draft = ""
	return s
}

func (in *Input) recallTo(i int) bool {
	if in.hpos == len(in.hist) {
		in.draft = in.String()
	}
	if i < 0 || i > len(in.hist) {
		return false
	}
	in.hpos = i
	if i == len(in.hist) {
		in.Set(in.draft)
	} else {
		in.Set(in.hist[i])
	}
	return true
}

// RecallB recalls the previous history line.
func (in *Input) RecallB() bool { return in.recallTo(in.hpos - 1) }

// RecallF recalls the next history line.
func (in *Input) RecallF() bool { return in.recallTo(in.hpos + 1) }

// RecallBeg recalls the oldest line.
func (in *Input) RecallBeg() bool { return in.recallTo(0) }

// RecallEnd returns to the line being typed.
func (in *Input) RecallEnd() bool { return in.recallTo(len(in.hist)) }

// SearchB recalls the previous history line starting with the text before
// the cursor.
func (in *Input) SearchB() bool {
	pre := in.Head()
	if in.hpos == len(in.hist) {
		in.draft = in.String()
	}
	for i := in.hpos - 1; i >= 0; i-- {
		if strings.HasPrefix(in.hist[i], pre) {
			pt := in.pt
			in.hpos = i
			in.buf = []rune(in.hist[i])
			in.pt = min(pt, len(in.buf))
			return true
		}
	}
	return false
}

// SearchF is SearchB forward.
func (in *Input) SearchF() bool {
	pre := in.Head()
	for i := in.hpos + 1; i < len(in.hist); i++ {
		if strings.HasPrefix(in.hist[i], pre) {
			pt := in.pt
			in.hpos = i
			in.buf = []rune(in.hist[i])
			in.pt = min(pt, len(in.buf))
			return true
		}
	}
	if in.hpos < len(in.hist) {
		pt := in.pt
		in.hpos = len(in.hist)
		in.buf = []rune(in.draft)
		in.pt = min(pt, len(in.buf))
		return true
	}
	return false
}

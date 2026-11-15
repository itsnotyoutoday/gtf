// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package help

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

var (
	wikiLink = regexp.MustCompile(`\[\[([^\[\]|]+)(?:\|([^\[\]]+))?\]\]`)
	mdLink   = regexp.MustCompile(`\[([^\]]+)\]\(([^)\s]+)\)`)
	boldRe   = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	codeRe   = regexp.MustCompile("`([^`]+)`")
)

// Link is one [[...]] reference in a topic body.
type Link struct {
	Text   string // what the reader sees ("" = the target)
	Target string // a title, file name or topics: name
}

// Links lists the [[...]] references of a body in order, skipping code blocks and inline code.
func Links(body string) []Link {
	var out []Link
	inCode := false
	for _, l := range strings.Split(body, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "```") {
			inCode = !inCode
			continue
		}
		if inCode {
			continue
		}
		l = codeRe.ReplaceAllString(l, "")
		for _, m := range wikiLink.FindAllStringSubmatch(l, -1) {
			a, b := strings.TrimSpace(m[1]), strings.TrimSpace(m[2])
			if b == "" {
				out = append(out, Link{Target: a})
			} else {
				out = append(out, Link{Text: a, Target: b})
			}
		}
	}
	return out
}

// inline renders the inline Markdown of one line as plain text: links, bold and code marks.
func (ix *Index) inline(s string) string {
	s = wikiLink.ReplaceAllStringFunc(s, func(m string) string {
		sm := wikiLink.FindStringSubmatch(m)
		text, target := strings.TrimSpace(sm[1]), strings.TrimSpace(sm[2])
		if target == "" {
			target, text = text, ""
		}
		t, ok := ix.Lookup(target)
		if !ok {
			if text == "" {
				return target
			}
			return text
		}
		name := "/help " + t.CanonicalName()
		if text == "" || strings.EqualFold(text, t.Title) {
			return t.Title + " (" + name + ")"
		}
		return text + " (" + name + ")"
	})
	s = mdLink.ReplaceAllStringFunc(s, func(m string) string {
		sm := mdLink.FindStringSubmatch(m)
		if strings.HasPrefix(sm[2], "http://") || strings.HasPrefix(sm[2], "https://") {
			return sm[1] + " <" + sm[2] + ">"
		}
		return sm[1]
	})
	s = boldRe.ReplaceAllString(s, "$1")
	s = codeRe.ReplaceAllString(s, "$1")
	return s
}

// RenderText renders a topic's Markdown as plain text lines for a terminal of the given width. Everything is indented two columns; code blocks
// and tables are not re-wrapped.
func (ix *Index) RenderText(t *Topic, width int) []string {
	if width < 40 {
		width = 40
	}
	var out []string
	add := func(s string) { out = append(out, s) }
	inCode := false
	var para []string
	flush := func(indent, hang string) {
		if len(para) == 0 {
			return
		}
		for _, l := range wrap(ix.inline(strings.Join(para, " ")), width-len(indent), indent, hang) {
			add(l)
		}
		para = nil
	}
	lines := strings.Split(t.Body, "\n")
	for i := 0; i < len(lines); i++ {
		l := strings.TrimRight(lines[i], " \t")
		trim := strings.TrimSpace(l)
		if strings.HasPrefix(trim, "```") {
			flush("  ", "  ")
			inCode = !inCode
			continue
		}
		if inCode {
			add("    " + l)
			continue
		}
		switch {
		case trim == "":
			flush("  ", "  ")
			if len(out) > 0 && out[len(out)-1] != "" {
				add("")
			}
		case strings.HasPrefix(trim, "# "):
			flush("  ", "  ")
			add(ix.inline(strings.TrimPrefix(trim, "# ")))
			add("")
		case strings.HasPrefix(trim, "## "), strings.HasPrefix(trim, "### "):
			flush("  ", "  ")
			if len(out) > 0 && out[len(out)-1] != "" {
				add("")
			}
			add("  " + ix.inline(strings.TrimLeft(trim, "# ")))
			add("")
		case strings.HasPrefix(trim, "|"):
			flush("  ", "  ")
			j := i
			var rows [][]string
			for j < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[j]), "|") {
				cells := splitRow(strings.TrimSpace(lines[j]))
				if !isSeparatorRow(cells) {
					for k := range cells {
						cells[k] = ix.inline(cells[k])
					}
					rows = append(rows, cells)
				}
				j++
			}
			for _, r := range renderTable(rows, width-2) {
				add("  " + r)
			}
			i = j - 1
		case strings.HasPrefix(trim, "> "):
			flush("  ", "  ")
			for _, w := range wrap(ix.inline(strings.TrimPrefix(trim, "> ")), width-4, "  | ", "  | ") {
				add(w)
			}
		case isBullet(trim):
			flush("  ", "  ")
			marker, text := splitBullet(trim)
			ind := "  " + strings.Repeat(" ", (len(l)-len(strings.TrimLeft(l, " ")))/2*2)
			para = []string{text}
			flush(ind+marker+" ", ind+strings.Repeat(" ", len(marker)+1))
		default:
			para = append(para, trim)
		}
	}
	flush("  ", "  ")
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return out
}

func isBullet(s string) bool {
	if strings.HasPrefix(s, "- ") || strings.HasPrefix(s, "* ") {
		return true
	}
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	return i > 0 && i+1 < len(s) && s[i] == '.' && s[i+1] == ' '
}

func splitBullet(s string) (marker, text string) {
	if strings.HasPrefix(s, "- ") || strings.HasPrefix(s, "* ") {
		return "-", strings.TrimSpace(s[2:])
	}
	i := strings.Index(s, ". ")
	return s[:i+1], strings.TrimSpace(s[i+2:])
}

func splitRow(s string) []string {
	s = strings.Trim(s, "|")
	parts := strings.Split(s, "|")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

func isSeparatorRow(cells []string) bool {
	if len(cells) == 0 {
		return false
	}
	for _, c := range cells {
		c = strings.Trim(c, ":- ")
		if c != "" {
			return false
		}
	}
	return true
}

// renderTable aligns the columns; a table wider than the terminal is printed as "first column: the rest" lines instead.
func renderTable(rows [][]string, width int) []string {
	if len(rows) == 0 {
		return nil
	}
	n := 0
	for _, r := range rows {
		if len(r) > n {
			n = len(r)
		}
	}
	w := make([]int, n)
	for _, r := range rows {
		for i, c := range r {
			if l := utf8.RuneCountInString(c); l > w[i] {
				w[i] = l
			}
		}
	}
	total := 0
	for _, x := range w {
		total += x + 2
	}
	var out []string
	if total-2 > width && n == 2 {
		// a two-column table too wide for the window: the left cell on its own line, the right one wrapped under it
		for ri, r := range rows {
			if ri == 0 && len(rows) > 1 {
				continue
			}
			out = append(out, r[0])
			if len(r) > 1 && r[1] != "" {
				out = append(out, wrap(r[1], width-4, "    ", "    ")...)
			}
		}
		return out
	}
	if total-2 > width {
		hdr := rows[0]
		for ri, r := range rows {
			if ri == 0 && len(rows) > 1 {
				continue
			}
			first := ""
			if len(r) > 0 {
				first = r[0]
			}
			var rest []string
			for i := 1; i < len(r); i++ {
				if r[i] == "" {
					continue
				}
				if len(rows) > 1 && i < len(hdr) && hdr[i] != "" {
					rest = append(rest, hdr[i]+": "+r[i])
				} else {
					rest = append(rest, r[i])
				}
			}
			out = append(out, wrap(first+" - "+strings.Join(rest, "; "), width, "", "    ")...)
		}
		return out
	}
	for ri, r := range rows {
		var b strings.Builder
		for i := 0; i < n; i++ {
			c := ""
			if i < len(r) {
				c = r[i]
			}
			b.WriteString(c)
			if i < n-1 {
				b.WriteString(strings.Repeat(" ", w[i]-utf8.RuneCountInString(c)+2))
			}
		}
		out = append(out, strings.TrimRight(b.String(), " "))
		if ri == 0 && len(rows) > 1 {
			out = append(out, strings.Repeat("-", min(total-2, width)))
		}
	}
	return out
}

// wrap breaks s at spaces to at most width columns; the first line starts with first, the others with hang.
func wrap(s string, width int, first, hang string) []string {
	if width < 20 {
		width = 20
	}
	words := strings.Fields(s)
	var out []string
	cur := first
	empty := true
	for _, w := range words {
		if !empty && utf8.RuneCountInString(cur)+1+utf8.RuneCountInString(w) > width+len(first) {
			out = append(out, cur)
			cur, empty = hang+w, false
			continue
		}
		if empty {
			cur += w
			empty = false
		} else {
			cur += " " + w
		}
	}
	if !empty || len(out) == 0 {
		out = append(out, cur)
	}
	return out
}

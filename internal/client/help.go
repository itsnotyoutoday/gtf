// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package client

import (
	"bufio"
	"os"
	"path/filepath"
	"sort"
	"strings"

	gtfhelp "github.com/itsnotyoutoday/gtf/help"
)

// helpData holds what /help reads: gtf's own topics (Markdown files under help/topics, embedded in the binary) and the bundled TinyFugue manual
// (tf-help, an unmodified copy in makehelp format: "&topic" header lines, a body, repeat; see gtflib/TF-NOTICE.md). gtf's topics win; a topic only
// the TinyFugue manual has is shown with a line saying where it comes from.
type helpData struct {
	loaded bool
	path   string // the TinyFugue manual
	topics map[string][]string
	order  []string
	own    *gtfhelp.Index
}

// parseHelpFile reads a makehelp-format file; ok is false when it cannot be opened.
func parseHelpFile(path string) (topics map[string][]string, order []string, ok bool) {
	topics = map[string][]string{}
	f, err := os.Open(path)
	if err != nil {
		return topics, nil, false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<16), 1<<20)
	var heads []string
	var body []string
	flush := func() {
		for _, n := range heads {
			topics[n] = body
			order = append(order, n)
		}
		heads, body = nil, nil
	}
	inBody := false
	for sc.Scan() {
		l := sc.Text()
		if strings.HasPrefix(l, "&") {
			if inBody {
				flush()
				inBody = false
			}
			heads = append(heads, strings.ToLower(strings.TrimSpace(l[1:])))
			continue
		}
		inBody = true
		body = append(body, l)
	}
	flush()
	return topics, order, true
}

func (c *Client) loadHelp() *helpData {
	h := c.help
	if h.loaded {
		return h
	}
	h.loaded = true
	h.path = c.in.Var("TFHELP")
	if h.path == "" {
		h.path = filepath.Join(c.libDir, "tf-help")
	}
	h.topics, h.order, _ = parseHelpFile(h.path)
	h.own, _ = gtfhelp.Default()
	return h
}

// helpLookup finds a topic in one help file, trying the forms "/name", "name()" and "name" without a leading slash.
func helpLookup(topics map[string][]string, topic string) ([]string, bool) {
	if b, ok := topics[topic]; ok {
		return b, true
	}
	for _, alt := range []string{"/" + topic, topic + "()", strings.TrimPrefix(topic, "/")} {
		if b, ok := topics[alt]; ok {
			return b, true
		}
	}
	return nil, false
}

// tfHelpNote says where a topic that only the TinyFugue manual has comes from.
const tfHelpNote = "% (from the bundled TinyFugue manual, not gtf's own help; gtf is compatible with TinyFugue but is a separate program, so details can differ: /help tinyfugue)"

// helpWidth is the width gtf's own topics are wrapped to: the window, but never wider than 78 so the text stays readable.
func (c *Client) helpWidth() int {
	w := c.ui.W - 1
	if w > 78 {
		w = 78
	}
	return w
}

// helpTopicsList is the generated "/help topics": every gtf topic by section, with the name to type.
func (c *Client) helpTopicsList(ix *gtfhelp.Index) []string {
	out := []string{"Topics written for gtf  (/help <name>)", ""}
	cur := ""
	for _, t := range ix.Topics {
		if t.NoHelp {
			continue
		}
		if t.Section != cur {
			if cur != "" {
				out = append(out, "")
			}
			cur = t.Section
			out = append(out, "  "+cur)
		}
		out = append(out, "    "+t.CanonicalName()+strings.Repeat(" ", max(1, 26-len(t.CanonicalName())))+t.Title)
	}
	out = append(out, "", "  The commands and the scripting language: /help <command>, from the bundled TinyFugue manual (/help tf <topic> looks only there).")
	return out
}

// showHelp implements /help: gtf's own topics first, then the TinyFugue manual. "/help tf <topic>" looks only in the manual.
func (c *Client) showHelp(topic string) {
	h := c.loadHelp()
	topic = strings.ToLower(strings.TrimSpace(topic))
	if len(h.topics) == 0 && h.own == nil {
		c.echoLine("% Help file " + h.path + " not found.")
		return
	}
	if topic == "" {
		topic = "intro"
	}
	tfOnly := false
	if rest, ok := strings.CutPrefix(topic, "tf "); ok && strings.TrimSpace(rest) != "" {
		topic, tfOnly = strings.TrimSpace(rest), true
	}
	if !tfOnly && h.own != nil && (topic == "topics" || topic == "index") {
		for _, l := range c.helpTopicsList(h.own) {
			c.echoLine(l)
		}
		return
	}
	if !tfOnly && h.own != nil {
		if t, ok := h.own.Lookup(topic); ok && !t.NoHelp {
			for _, l := range h.own.RenderText(t, c.helpWidth()) {
				c.echoLine(l)
			}
			return
		}
	}
	body, ok := helpLookup(h.topics, topic)
	if !ok {
		var near []string
		seen := map[string]bool{}
		consider := func(n string) {
			if (strings.HasPrefix(n, topic) || strings.HasPrefix(n, "/"+topic)) && !seen[n] {
				seen[n] = true
				near = append(near, n)
			}
		}
		if !tfOnly && h.own != nil {
			for _, n := range h.own.Names() {
				consider(n)
			}
		}
		for _, n := range h.order {
			consider(n)
		}
		sort.Strings(near)
		if len(near) > 0 {
			c.echoLine("% Topics matching \"" + topic + "\": " + strings.Join(near, " "))
		} else {
			c.echoLine("% Help on subject " + topic + " not found.")
		}
		return
	}
	if topic == "copyright" {
		for _, l := range ownNotice {
			c.echoLine(l)
		}
	} else {
		c.echoLine(tfHelpNote)
	}
	for _, l := range body {
		c.echoLine(l)
	}
}

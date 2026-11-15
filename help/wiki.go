// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package help

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// WikiPages renders the topics as GitHub wiki pages: file name -> Markdown. Each page is the topic's body with its [[links]] rewritten to wiki page
// names (so a link by file name or topics: alias still works), plus _Sidebar.md (the sections) and _Footer.md. Topics marked wiki: no are left out.
// A page named Home.md comes from a topic titled "Home" if one exists.
func (ix *Index) WikiPages() (map[string]string, error) {
	pages := map[string]string{}
	var problems []string
	for _, t := range ix.Topics {
		if t.NoWiki {
			continue
		}
		body := wikiLink.ReplaceAllStringFunc(t.Body, func(m string) string {
			sm := wikiLink.FindStringSubmatch(m)
			text, target := strings.TrimSpace(sm[1]), strings.TrimSpace(sm[2])
			if target == "" {
				target, text = text, ""
			}
			d, ok := ix.Lookup(target)
			if !ok {
				problems = append(problems, fmt.Sprintf("%s: link to unknown topic %q", t.File, target))
				return m
			}
			if d.NoWiki {
				if text == "" {
					text = d.Title
				}
				return text // a /help-only topic: plain text on the wiki
			}
			if text == "" {
				text = d.Title
			}
			return "[[" + text + "|" + d.WikiName() + "]]"
		})
		// the first heading is the page title the wiki shows already
		body = dropFirstHeading(body, t.Title)
		pages[t.WikiName()+".md"] = strings.TrimRight(body, "\n") + "\n"
	}
	if len(problems) > 0 {
		return nil, fmt.Errorf("%s", strings.Join(problems, "\n"))
	}
	pages["_Sidebar.md"] = ix.sidebar()
	pages["_Footer.md"] = "gtf is free software (GPL-3.0-or-later). It is TinyFugue-compatible; it is not TinyFugue. These pages are generated from the `help/topics` files in the repository: edit those, not the wiki.\n"
	return pages, nil
}

func dropFirstHeading(body, title string) string {
	lines := strings.Split(body, "\n")
	for i, l := range lines {
		if strings.TrimSpace(l) == "" {
			continue
		}
		if strings.HasPrefix(l, "# ") && strings.EqualFold(strings.TrimSpace(l[2:]), title) {
			return strings.TrimLeft(strings.Join(lines[i+1:], "\n"), "\n")
		}
		break
	}
	return body
}

func (ix *Index) sidebar() string {
	var b strings.Builder
	cur := ""
	for _, t := range ix.Topics {
		if t.NoWiki {
			continue
		}
		if t.Section != cur {
			cur = t.Section
			fmt.Fprintf(&b, "\n**%s**\n\n", cur)
		}
		fmt.Fprintf(&b, "* [[%s|%s]]\n", t.Title, t.WikiName())
	}
	return strings.TrimLeft(b.String(), "\n")
}

// WriteWiki writes WikiPages into dir (created if needed). Files already in dir that are not generated are left alone.
func (ix *Index) WriteWiki(dir string) ([]string, error) {
	pages, err := ix.WikiPages()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	var names []string
	for n, c := range pages {
		if err := os.WriteFile(filepath.Join(dir, n), []byte(c), 0o644); err != nil {
			return nil, err
		}
		names = append(names, n)
	}
	return names, nil
}

// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

// Package help is gtf's own documentation: one Markdown file per topic under topics/, written once and used twice. The client renders the files as
// plain text for /help (gtf's own topics come before the bundled TinyFugue manual), and cmd/helpwiki turns the same files into GitHub wiki pages.
//
// A topic file starts with a small front matter block and then plain Markdown:
//
//	---
//	title: Squatter address
//	topics: squatter-address, squatter-bind, /bind
//	section: Remote access
//	order: 30
//	---
//	# Squatter address
//	...
//
// Links to other topics are written [[Squatter pairing]] or [[how to pair|Squatter pairing]] (text first, then the target: the target may be a
// title, a file name or any name in topics:). GitHub wikis read the same syntax, so the files need no conversion for them beyond page names.
// See README.md in this directory for the authoring rules.
package help

import (
	"embed"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
)

//go:embed topics
var topicFS embed.FS

// Topic is one documentation page.
type Topic struct {
	File    string   // the file name without .md; also a name the topic answers to
	Title   string   // the page title (wiki page name, heading, /help name)
	Names   []string // every name /help answers to (lower case): topics:, the file name and the title
	Section string   // the group in /help topics and the wiki sidebar
	Order   int      // position within the section (then by title)
	NoHelp  bool     // wiki only: not shown by /help (front matter: help: no)
	NoWiki  bool     // /help only: not published to the wiki (front matter: wiki: no)
	Body    string   // the Markdown after the front matter
}

// Index is the set of topics with name lookup.
type Index struct {
	Topics []*Topic // by section order, then Order, then Title
	byName map[string]*Topic
}

// Lookup finds a topic by any of its names; "/name" and "name()" forms and case are forgiven.
func (ix *Index) Lookup(name string) (*Topic, bool) {
	n := strings.ToLower(strings.TrimSpace(name))
	n = strings.ReplaceAll(n, "_", "-") // not used in names, but wiki page names use hyphens for spaces
	for _, c := range []string{n, strings.ReplaceAll(n, "-", " "), strings.TrimPrefix(n, "/"), "/" + n, strings.TrimSuffix(n, "()")} {
		if t, ok := ix.byName[c]; ok {
			return t, true
		}
	}
	return nil, false
}

// Names are all the names the index answers to, sorted (for "topics matching").
func (ix *Index) Names() []string {
	out := make([]string, 0, len(ix.byName))
	for n := range ix.byName {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Parse builds an Index from a file system holding topics/*.md.
func Parse(fsys fs.FS) (*Index, error) {
	ix := &Index{byName: map[string]*Topic{}}
	ents, err := fs.ReadDir(fsys, "topics")
	if err != nil {
		return nil, err
	}
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		b, err := fs.ReadFile(fsys, path.Join("topics", e.Name()))
		if err != nil {
			return nil, err
		}
		t, err := parseTopic(strings.TrimSuffix(e.Name(), ".md"), string(b))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", e.Name(), err)
		}
		ix.Topics = append(ix.Topics, t)
		for _, n := range t.Names {
			if old, dup := ix.byName[n]; dup && old != t {
				return nil, fmt.Errorf("%s: the name %q is also used by %s", e.Name(), n, old.File)
			}
			ix.byName[n] = t
		}
	}
	sort.SliceStable(ix.Topics, func(i, j int) bool {
		a, b := ix.Topics[i], ix.Topics[j]
		if a.Section != b.Section {
			return sectionRank(a.Section) < sectionRank(b.Section) || (sectionRank(a.Section) == sectionRank(b.Section) && a.Section < b.Section)
		}
		if a.Order != b.Order {
			return a.Order < b.Order
		}
		return a.Title < b.Title
	})
	return ix, nil
}

// SectionOrder is the order sections are listed in; sections not named here follow alphabetically.
var SectionOrder = []string{"Start here", "Remote access", "Using gtf", "Scripting", "Layouts and graphics", "Console protocol", "Reference", "About"}

func sectionRank(s string) int {
	for i, n := range SectionOrder {
		if n == s {
			return i
		}
	}
	return len(SectionOrder)
}

func parseTopic(file, src string) (*Topic, error) {
	t := &Topic{File: file}
	src = strings.ReplaceAll(src, "\r\n", "\n")
	if !strings.HasPrefix(src, "---\n") {
		return nil, fmt.Errorf("no front matter (the file must start with ---)")
	}
	rest := src[4:]
	end := strings.Index(rest, "\n---\n")
	if end < 0 {
		return nil, fmt.Errorf("front matter is not closed with ---")
	}
	for _, l := range strings.Split(rest[:end], "\n") {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		k, v, ok := strings.Cut(l, ":")
		if !ok {
			return nil, fmt.Errorf("bad front matter line %q", l)
		}
		k, v = strings.ToLower(strings.TrimSpace(k)), strings.TrimSpace(v)
		switch k {
		case "title":
			t.Title = v
		case "section":
			t.Section = v
		case "order":
			n, err := strconv.Atoi(v)
			if err != nil {
				return nil, fmt.Errorf("order %q is not a number", v)
			}
			t.Order = n
		case "topics":
			for _, n := range strings.Split(v, ",") {
				if n = strings.ToLower(strings.TrimSpace(n)); n != "" {
					t.Names = append(t.Names, n)
				}
			}
		case "help":
			t.NoHelp = isNo(v)
		case "wiki":
			t.NoWiki = isNo(v)
		default:
			return nil, fmt.Errorf("unknown front matter key %q", k)
		}
	}
	if t.Title == "" {
		return nil, fmt.Errorf("no title")
	}
	if t.Section == "" {
		return nil, fmt.Errorf("no section")
	}
	t.Body = strings.TrimLeft(rest[end+5:], "\n")
	for _, n := range []string{strings.ToLower(file), strings.ToLower(t.Title)} {
		if !contains(t.Names, n) {
			t.Names = append(t.Names, n)
		}
	}
	return t, nil
}

func isNo(v string) bool {
	switch strings.ToLower(v) {
	case "no", "false", "off", "0":
		return true
	}
	return false
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

var (
	defaultOnce sync.Once
	defaultIx   *Index
	defaultErr  error
)

// Default is the index of the embedded topics.
func Default() (*Index, error) {
	defaultOnce.Do(func() { defaultIx, defaultErr = Parse(topicFS) })
	return defaultIx, defaultErr
}

// CanonicalName is the name /help prints for a link to the topic: its first topics: name, else the file name.
func (t *Topic) CanonicalName() string {
	if len(t.Names) > 0 {
		return t.Names[0]
	}
	return strings.ToLower(t.File)
}

// WikiName is the wiki page name: the title with spaces as hyphens (GitHub wiki convention), without characters a file name cannot hold.
func (t *Topic) WikiName() string {
	r := strings.NewReplacer(" ", "-", "/", "-", "\\", "-", ":", "-", "?", "", "#", "", "*", "")
	return r.Replace(t.Title)
}

// Validate checks every topic: each [[link]] must resolve, and a topic must not be empty. It returns the problems.
func (ix *Index) Validate() []string {
	var out []string
	for _, t := range ix.Topics {
		if strings.TrimSpace(t.Body) == "" {
			out = append(out, t.File+": empty topic")
		}
		for _, l := range Links(t.Body) {
			if _, ok := ix.Lookup(l.Target); !ok {
				out = append(out, fmt.Sprintf("%s: link to unknown topic %q", t.File, l.Target))
			}
		}
	}
	return out
}

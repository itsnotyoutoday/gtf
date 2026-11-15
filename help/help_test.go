package help

import (
	"strings"
	"testing"
	"testing/fstest"
)

func TestEmbeddedTopicsParseAndEveryLinkResolves(t *testing.T) {
	ix, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	if len(ix.Topics) < 10 {
		t.Fatalf("only %d topics", len(ix.Topics))
	}
	if errs := ix.Validate(); len(errs) > 0 {
		t.Fatal(strings.Join(errs, "\n"))
	}
	for _, tp := range ix.Topics {
		if got, ok := ix.Lookup(tp.Title); !ok || got != tp {
			t.Errorf("%s: not found by its title %q", tp.File, tp.Title)
		}
		if got, ok := ix.Lookup(tp.CanonicalName()); !ok || got != tp {
			t.Errorf("%s: not found by its canonical name %q", tp.File, tp.CanonicalName())
		}
		if !strings.HasPrefix(strings.TrimSpace(tp.Body), "# ") {
			t.Errorf("%s: a topic starts with its # heading", tp.File)
		}
	}
}

func TestParseRejectsBadFrontMatterAndDuplicateNames(t *testing.T) {
	bad := map[string]string{
		"no front matter": "# x\n",
		"unclosed":        "---\ntitle: x\n# x\n",
		"no title":        "---\nsection: s\n---\n# x\n",
		"no section":      "---\ntitle: x\n---\n# x\n",
		"unknown key":     "---\ntitle: x\nsection: s\ncolour: red\n---\n# x\n",
		"bad order":       "---\ntitle: x\nsection: s\norder: first\n---\n# x\n",
	}
	for name, src := range bad {
		if _, err := Parse(fstest.MapFS{"topics/a.md": {Data: []byte(src)}}); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	dup := fstest.MapFS{
		"topics/a.md": {Data: []byte("---\ntitle: A\nsection: s\ntopics: shared\n---\n# A\nx\n")},
		"topics/b.md": {Data: []byte("---\ntitle: B\nsection: s\ntopics: shared\n---\n# B\nx\n")},
	}
	if _, err := Parse(dup); err == nil {
		t.Error("two topics with the same name accepted")
	}
}

func TestLookupForgivesCaseSlashAndHyphens(t *testing.T) {
	ix, _ := Default()
	for _, n := range []string{"Squatter", "SQUATTER", "/squatter", "squatter quick start", "squatter-quick-start", "Squatter-quickstart", "/pair", "pair"} {
		if _, ok := ix.Lookup(n); !ok {
			t.Errorf("%q not found", n)
		}
	}
	if _, ok := ix.Lookup("nosuchthing"); ok {
		t.Error("found a topic that does not exist")
	}
}

func TestRenderTextLinksListsTablesAndCode(t *testing.T) {
	ix, err := Parse(fstest.MapFS{
		"topics/a.md": {Data: []byte("---\ntitle: Alpha page\ntopics: alpha\nsection: s\n---\n# Alpha page\n\nSee [[Beta page]] and [[the other one|beta]], a **bold** `code` word and [docs](https://example.org/x).\n\n* first item that is long enough to wrap around the narrow test width of forty characters\n* second\n\n1. one\n2. two\n\n| k | v |\n|---|---|\n| a | b |\n\n```\nverbatim   spacing\n```\n")},
		"topics/b.md": {Data: []byte("---\ntitle: Beta page\ntopics: beta\nsection: s\n---\n# Beta page\n\nx\n")},
	})
	if err != nil {
		t.Fatal(err)
	}
	a, _ := ix.Lookup("alpha")
	txt := strings.Join(ix.RenderText(a, 40), "\n")
	for _, want := range []string{
		"Alpha page\n",
		"  - first item that is long enough to",
		"    wrap around",
		"  1. one",
		"  k  v\n",
		"  a  b",
		"    verbatim   spacing",
	} {
		if !strings.Contains(txt, want) {
			t.Errorf("missing %q in:\n%s", want, txt)
		}
	}
	flat := strings.Join(strings.Fields(txt), " ")
	for _, want := range []string{"Beta page (/help beta)", "the other one (/help beta)", "a bold code word", "docs <https://example.org/x>"} {
		if !strings.Contains(flat, want) {
			t.Errorf("missing %q in:\n%s", want, txt)
		}
	}
	for _, line := range strings.Split(txt, "\n") {
		if len([]rune(line)) > 40+4 {
			t.Errorf("line wider than the window: %q", line)
		}
	}
}

func TestWikiPagesRewriteLinksAndSkipHelpOnlyTopics(t *testing.T) {
	ix, err := Parse(fstest.MapFS{
		"topics/a.md":   {Data: []byte("---\ntitle: Alpha page\ntopics: alpha\nsection: One\n---\n# Alpha page\n\nSee [[beta]], [[with text|Beta page]], [[Hidden]].\n")},
		"topics/b.md":   {Data: []byte("---\ntitle: Beta page\ntopics: beta\nsection: One\n---\n# Beta page\n\nx\n")},
		"topics/hid.md": {Data: []byte("---\ntitle: Hidden\nsection: Two\nwiki: no\n---\n# Hidden\n\nx\n")},
	})
	if err != nil {
		t.Fatal(err)
	}
	pages, err := ix.WikiPages()
	if err != nil {
		t.Fatal(err)
	}
	a := pages["Alpha-page.md"]
	for _, want := range []string{"[[Beta page|Beta-page]]", "[[with text|Beta-page]]", "Hidden"} {
		if !strings.Contains(a, want) {
			t.Errorf("missing %q in %q", want, a)
		}
	}
	if strings.Contains(a, "# Alpha page") {
		t.Error("the first heading duplicates the wiki page title")
	}
	if strings.Contains(a, "[[Hidden") {
		t.Error("a link to a /help-only topic stays a wiki link")
	}
	if _, ok := pages["Hidden.md"]; ok {
		t.Error("a wiki: no topic was published")
	}
	if sb := pages["_Sidebar.md"]; !strings.Contains(sb, "**One**") || !strings.Contains(sb, "[[Alpha page|Alpha-page]]") || strings.Contains(sb, "Hidden") {
		t.Errorf("sidebar:\n%s", sb)
	}
}

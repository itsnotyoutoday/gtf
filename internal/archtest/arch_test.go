// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

// Package archtest enforces the directory/import structure documented in
// docs/ARCHITECTURE.md by parsing every package's imports.
package archtest

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const mod = "github.com/itsnotyoutoday/gtf"
const engine = "github.com/itsnotyoutoday/gtfparser"

// rule says which internal packages (by prefix, relative to the module) a
// package group may import, and whether it may import the engine module.
type rule struct {
	prefix string   // package directory prefix this applies to
	allow  []string // allowed module-relative import prefixes (self always allowed)
	engine bool
}

var rules = []rule{
	{prefix: ".", allow: nil}, // version.go: the VERSION file, nothing else
	{prefix: "cmd/gtf", allow: []string{"internal/client", "gtflib"}},
	{prefix: "cmd/vtdump", allow: []string{"internal/ui/vt"}},
	{prefix: "cmd/helpwiki", allow: []string{"help"}}, // builds the GitHub wiki pages from the help topics
	{prefix: "internal/client", allow: []string{"internal/ui", "internal/net", "internal/layout", "internal/cmcpui", "internal/update", "internal/handoff", "internal/squatter", "help", "gtflib"}, engine: true},
	{prefix: "internal/handoff", allow: nil},
	{prefix: "internal/squatter", allow: nil}, // the squatter daemon: no gtf package, no engine; only internal/client imports it
	{prefix: "internal/ui", allow: []string{"internal/layout"}},
	{prefix: "internal/net", allow: []string{"internal/net"}},
	{prefix: "internal/layout", allow: nil},
	{prefix: "internal/cmcpui", allow: []string{"internal/layout"}}, // CMCP cell renderer: bound screen tree -> cells (the core module is external)
	{prefix: "internal/update", allow: nil},                         // releases, checksums, replacing the binary and library; no gtf imports
	{prefix: "help", allow: nil},                                    // gtf's own documentation: Markdown topics, the /help renderer and the wiki generator; no gtf package, no engine
	{prefix: "gtflib", allow: nil},
}

func moduleRoot(t *testing.T) string {
	d, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			return d
		}
		n := filepath.Dir(d)
		if n == d {
			t.Fatal("no go.mod")
		}
		d = n
	}
}

func match(dir, prefix string) bool { return dir == prefix || strings.HasPrefix(dir, prefix+"/") }

func TestImportDirections(t *testing.T) {
	root := moduleRoot(t)
	checked := 0
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			n := d.Name()
			if n == ".git" || n == "node_modules" || strings.HasPrefix(n, "_") || (n == ".claude") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") {
			return nil
		}
		rel, _ := filepath.Rel(root, filepath.Dir(p))
		rel = filepath.ToSlash(rel)
		if strings.HasPrefix(rel, "internal/archtest") {
			return nil
		}
		var r *rule
		for i := range rules {
			if match(rel, rules[i].prefix) {
				r = &rules[i]
				break
			}
		}
		f, err := parser.ParseFile(token.NewFileSet(), p, nil, parser.ImportsOnly)
		if err != nil {
			t.Errorf("%s: %v", p, err)
			return nil
		}
		if r == nil {
			t.Errorf("%s: package directory %q is not in the documented structure (docs/ARCHITECTURE.md)", p, rel)
			return nil
		}
		checked++
		for _, im := range f.Imports {
			path, _ := strconv.Unquote(im.Path.Value)
			switch {
			case path == engine || strings.HasPrefix(path, engine+"/"):
				if !r.engine {
					t.Errorf("%s imports the engine (%s); only internal/client may", p, path)
				}
			case strings.HasPrefix(path, mod+"/"):
				target := strings.TrimPrefix(path, mod+"/")
				ok := match(target, r.prefix)
				for _, a := range r.allow {
					if match(target, a) {
						ok = true
					}
				}
				if !ok {
					t.Errorf("%s (%s) must not import %s", p, rel, target)
				}
			}
		}
		return nil
	})
	if checked == 0 {
		t.Fatal("no packages checked")
	}
}

// The batch runner lives in the engine; cmd/gtf must stay a thin main.
func TestCmdIsThin(t *testing.T) {
	root := moduleRoot(t)
	b, err := os.ReadFile(filepath.Join(root, "cmd/gtf/main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(b), "\n"); n > 80 {
		t.Errorf("cmd/gtf/main.go has %d lines; keep logic in internal/client", n)
	}
}

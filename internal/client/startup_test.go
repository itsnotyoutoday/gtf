package client

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/itsnotyoutoday/gtf/internal/ui/vt"
	"github.com/itsnotyoutoday/gtfparser"
)

func startClient(t *testing.T, home string, noRC bool) (*Client, *vt.Screen) {
	t.Helper()
	t.Setenv("HOME", home) // "~" in the world file name comes from the environment
	scr := vt.New(100, 30)
	c, err := New(Config{Term: scr, HomeDir: home, NoRC: noRC, Quiet: true, Interactive: true, NoUpdateCheck: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	return c, scr
}

func listWorlds(c *Client) string {
	var out []string
	c.Do(func(in *gtfparser.Interp) {
		c.capture = &out
		in.Exec("/listworlds")
		c.capture = nil
	})
	return strings.Join(out, "\n")
}

// gtf loads ~/tiny.world (the WORLDFILE macro) at startup without a /loadworld line in the config; -f skips it like the personal config.
func TestWorldFileIsLoadedAtStartup(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "tiny.world"), []byte("/test addworld(\"mudx\", \"\", \"127.0.0.1\", \"4000\")\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, _ := startClient(t, home, false)
	if w := listWorlds(c); !strings.Contains(w, "mudx") {
		t.Fatalf("the world file was not loaded at startup:\n%s", w)
	}
	c2, _ := startClient(t, home, true)
	if w := listWorlds(c2); strings.Contains(w, "mudx") {
		t.Fatalf("-f must not load the world file:\n%s", w)
	}
}

// A config that already says /loadworld does not break anything: the worlds are simply defined (once).
func TestWorldFileAndAnExplicitLoadworldAgree(t *testing.T) {
	home := t.TempDir()
	_ = os.WriteFile(filepath.Join(home, "tiny.world"), []byte("/test addworld(\"mudx\", \"\", \"127.0.0.1\", \"4000\")\n"), 0o600)
	_ = os.WriteFile(filepath.Join(home, ".tfrc"), []byte("/loadworld\n/addworld first 127.0.0.1 4001\n"), 0o600)
	c, scr := startClient(t, home, false)
	w := listWorlds(c)
	if strings.Count(w, "mudx") != 1 || !strings.Contains(w, "first") {
		t.Fatalf("worlds:\n%s\nscreen:\n%s", w, scr.Dump())
	}
}

func TestFeaturesReportWhatGtfHas(t *testing.T) {
	c, _, _ := newTestClient(t)
	var out []string
	c.Do(func(in *gtfparser.Interp) {
		c.capture = &out
		in.Exec("/features")
		c.capture = nil
	})
	got := strings.Join(out, " ")
	for _, want := range []string{"+ssl", "+MCCPv2", "+IPv6", "+256colors"} {
		if !strings.Contains(got, want) {
			t.Errorf("/features lacks %s: %s", want, got)
		}
	}
	for _, not := range []string{"+SOCKS", "+MCCPv1", "+core"} {
		if strings.Contains(got, not) {
			t.Errorf("/features claims %s: %s", not, got)
		}
	}
}

func TestLayoutSavePath(t *testing.T) {
	c, _, _ := newTestClient(t)
	home := c.cfg.HomeDir
	dir := filepath.Join(home, ".gtf", "layouts")
	for _, tc := range []struct {
		args []string
		want string
	}{
		{nil, filepath.Join(dir, "cur.layout")},
		{[]string{"mine"}, filepath.Join(dir, "mine.layout")},
		{[]string{"mine.layout"}, filepath.Join(dir, "mine.layout")},
		{[]string{"./mine"}, "./mine"},
		{[]string{"/tmp/x/mine.layout"}, "/tmp/x/mine.layout"},
		{[]string{"~/lay/mine"}, filepath.Join(home, "lay", "mine")},
	} {
		if got := c.layoutSavePath("cur", tc.args); got != tc.want {
			t.Errorf("/layout save %v: %q, want %q", tc.args, got, tc.want)
		}
	}
}

// Without a world file nothing is said; and the step is a macro, so the personal config can turn it off.
func TestStartupWorldFileIsAMacroTheConfigCanRemove(t *testing.T) {
	home := t.TempDir()
	c, scr := startClient(t, home, false)
	c.Flush()
	if txt := scr.Text(); strings.Contains(txt, "% /") || strings.Contains(txt, "loadworld") {
		t.Fatalf("no world file, yet startup said something:\n%s", txt)
	}
	home2 := t.TempDir()
	_ = os.WriteFile(filepath.Join(home2, "tiny.world"), []byte("/test addworld(\"mudx\", \"\", \"127.0.0.1\", \"4000\")\n"), 0o600)
	_ = os.WriteFile(filepath.Join(home2, ".tfrc"), []byte("/undef gtf_startup\n"), 0o600)
	c2, _ := startClient(t, home2, false)
	if w := listWorlds(c2); strings.Contains(w, "mudx") {
		t.Fatalf("/undef gtf_startup in the config must stop the world file being loaded:\n%s", w)
	}
}

func TestWorldfileFunction(t *testing.T) {
	home := t.TempDir()
	c, _ := startClient(t, home, true)
	var out []string
	c.Do(func(in *gtfparser.Interp) {
		c.capture = &out
		in.Exec("/test echo(worldfile())")
		c.capture = nil
	})
	if got := strings.Join(out, ""); got != filepath.Join(home, "tiny.world") {
		t.Fatalf("worldfile() = %q", got)
	}
}

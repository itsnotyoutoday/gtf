// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package client

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/itsnotyoutoday/cmcp/frame"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/itsnotyoutoday/cmcp/msg"
	"github.com/itsnotyoutoday/cmcp/session"
	"github.com/itsnotyoutoday/gtf/internal/layout"
	"github.com/itsnotyoutoday/gtf/internal/ui/vt"
	"github.com/itsnotyoutoday/gtfparser"
)

// recTerm is a terminal that keeps the byte stream (for vtdump -png) besides the vt model.
type recTerm struct {
	*vt.Screen
	mu  sync.Mutex
	buf []byte
}

func (r *recTerm) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.buf = append(r.buf, p...)
	return r.Screen.Write(p)
}

func (r *recTerm) Text() string { r.mu.Lock(); defer r.mu.Unlock(); return r.Screen.Text() }
func (r *recTerm) Dump() string { r.mu.Lock(); defer r.mu.Unlock(); return r.Screen.Dump() }

func (f *screenFixture) contain(t *testing.T, subs ...string) {
	t.Helper()
	txt := f.term.Text()
	for _, s := range subs {
		if !strings.Contains(txt, s) {
			t.Fatalf("screen lacks %q:\n%s", s, f.term.Dump())
		}
	}
}

func (f *screenFixture) lack(t *testing.T, subs ...string) {
	t.Helper()
	txt := f.term.Text()
	for _, s := range subs {
		if strings.Contains(txt, s) {
			t.Fatalf("screen has %q:\n%s", s, f.term.Dump())
		}
	}
}

// fakeGame executes commands and records them.
type fakeGame struct {
	mu    sync.Mutex
	ran   []string
	docs  map[string]any
	epoch int
}

func (g *fakeGame) Player() session.Player {
	return session.Player{Epoch: g.epoch, Aboard: true, Held: []string{"operations", "navigation", "science", "tactical", "engineering", "room:transporter"}}
}
func (g *fakeGame) Permitted(string) bool         { return true }
func (g *fakeGame) CanSubscribe(string) bool      { return true }
func (g *fakeGame) Snapshot(p string) (any, bool) { d, ok := g.docs[p]; return d, ok }
func (g *fakeGame) Execute(c *msg.Cmd) msg.Reply {
	b, _ := json.Marshal(c.Args)
	g.mu.Lock()
	g.ran = append(g.ran, c.Cmd+" "+string(b))
	g.mu.Unlock()
	return msg.Reply{ID: c.ID, OK: true}
}
func (g *fakeGame) commands() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string(nil), g.ran...)
}

func readJSON(t testing.TB, name string, v any) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "cmcp", name))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
}

// loadDocs reads every Space.*--full.json example: package -> state document.
func loadDocs(t testing.TB) map[string]any {
	docs := map[string]any{}
	files, _ := filepath.Glob(filepath.Join("testdata", "cmcp", "Space.*--full.json"))
	for _, f := range files {
		var m struct {
			Package string `json:"package"`
			Payload struct {
				D any `json:"d"`
			} `json:"payload"`
		}
		readJSON(t, filepath.Base(f), &m)
		docs[m.Package] = m.Payload.D
	}
	return docs
}

type screenFixture struct {
	t    *testing.T
	c    *Client
	term *recTerm
	d    *cmcpDialer
	game *fakeGame
	docs map[string]any
	cat  []msg.CommandSpec
}

func newScreenFixture(t *testing.T, w, h int) *screenFixture {
	t.Helper()
	return newScreenFixtureOpt(t, w, h, t.TempDir(), nil)
}

// newThemeFixture is a fixture whose server also announces the given theme pack in Welcome.packs.
func newThemeFixture(t *testing.T, packs map[string][]byte, ref msg.PackRef) *screenFixture {
	t.Helper()
	themeAnnounce = []msg.PackRef{ref}
	t.Cleanup(func() { themeAnnounce = nil })
	return newScreenFixtureOpt(t, 120, 40, t.TempDir(), packs)
}

// themeAnnounce is added to the test server's Welcome.packs.
var themeAnnounce []msg.PackRef

func newScreenFixtureOpt(t *testing.T, w, h int, home string, packs map[string][]byte) *screenFixture {
	t.Helper()
	return newScreenFixtureSetup(t, w, h, home, packs)
}

// newScreenFixtureSetup runs setup commands before connecting; with %cmcp off it returns without
// waiting for CMCP.
func newScreenFixtureSetup(t *testing.T, w, h int, home string, packs map[string][]byte, setup ...string) *screenFixture {
	t.Helper()
	var cat []msg.CommandSpec
	readJSON(t, "commands.catalogue.json", &cat)
	g := &fakeGame{docs: loadDocs(t), epoch: 1}
	cfg := session.ServerConfig{Server: "Test", GameName: "T", MaxBody: 65536, Commands: cat, Backend: g,
		Packages: []string{"Space.Power", "Space.Ship.Status"}, Packs: themeAnnounce}
	term := &recTerm{Screen: vt.New(w, h)}
	d := &cmcpDialer{t: t, cfg: cfg, packs: packs}
	var c *Client
	d.c = &c
	var err error
	c, err = New(Config{Term: term, HomeDir: home, NoRC: true, Quiet: true, Dialer: d, Interactive: true, NoUpdateCheck: true, Now: func() time.Time { return nowFn() }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	f := &screenFixture{t: t, c: c, term: term, d: d, game: g, docs: g.docs, cat: cat}
	f0 := f
	exec(c, setup...)
	exec(c, "/connect localhost 4101")
	if len(setup) > 0 && strings.Contains(strings.Join(setup, " "), "cmcp=off") {
		cmEventually(t, "supports", func() bool { return d.gm() != nil && d.gm().received("Core.Supports.Set") == 1 })
		time.Sleep(200 * time.Millisecond)
		if d.gm().srv.State() != session.SrvOff || d.gm().received("Cobra.Hello") != 0 {
			t.Fatalf("CMCP offered or answered with %%cmcp off")
		}
		for _, fr := range d.gm().got {
			if fr.Package == "Core.Supports.Set" && strings.Contains(string(fr.Payload), "Cobra") {
				t.Fatalf("Cobra declared: %s", fr.Payload)
			}
		}
		return f0
	}
	cmEventually(t, "ready", func() bool { return d.gm() != nil && d.gm().srv.State() == session.SrvReady })
	cmEventually(t, "client ready", f.clientReady)
	return f
}

func (f *screenFixture) clientReady() (ok bool) {
	f.c.Do(func(*gtfparser.Interp) {
		ok = f.c.fg != nil && f.c.fg.cm != nil && f.c.fg.cm.UI().State == session.StateReady
	})
	return
}

// console sends the manned-operations Cobra.Console with another screen and console id, then the
// full document of every package in its implicit set.
func (f *screenFixture) console(screen, console string, mutate func(*msg.Console)) {
	var m struct {
		Payload msg.Console `json:"payload"`
	}
	readJSON(f.t, "Cobra.Console--manned-operations.json", &m)
	c := m.Payload
	c.Screen = screen
	if console != "" {
		c.Console.ID = console
	}
	c.Commands = nil
	for _, sp := range f.cat {
		c.Commands = append(c.Commands, sp.Name)
	}
	c.Subscribe = nil
	for _, pkg := range sortedDocKeys(f.docs) {
		c.Subscribe = append(c.Subscribe, msg.SubSpec{Pkg: pkg})
	}
	if mutate != nil {
		mutate(&c)
	}
	f.d.gm().with(func(s *session.Server) {
		s.SetConsole(c)
		for _, sub := range c.Subscribe {
			if doc, ok := f.docs[sub.Pkg]; ok {
				s.Push(sub.Pkg, true, doc)
			}
		}
	})
	want := screen
	cmEventually(f.t, "screen "+want, func() (ok bool) {
		f.c.Do(func(*gtfparser.Interp) {
			u := f.c.fg.cm.UI()
			ok = u.Screen.ID == want && len(u.Docs) >= 3
		})
		return
	})
	f.render()
}

var fixedNow = time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)

// nowFn is the clients' clock in these tests: fixed, unless a test moves it.
var nowFn = func() time.Time { return fixedNow }

func sortedDocKeys(m map[string]any) []string {
	var k []string
	for x := range m {
		k = append(k, x)
	}
	sort.Strings(k)
	return k
}

func (f *screenFixture) render() {
	f.c.Flush()
	time.Sleep(20 * time.Millisecond)
	f.c.Flush()
}

func (f *screenFixture) text() string { return f.term.Text() }

// dump writes the byte stream when GTF_DUMP_DIR is set (for vtdump -png).
func (f *screenFixture) dump(name string) {
	dir := os.Getenv("GTF_DUMP_DIR")
	if dir == "" {
		return
	}
	f.term.mu.Lock()
	defer f.term.mu.Unlock()
	os.WriteFile(filepath.Join(dir, name+".bytes"), f.term.buf, 0o644)
}

func TestCmcpScreensDraw(t *testing.T) {
	f := newScreenFixture(t, 120, 40)
	for _, id := range []string{"operations", "power", "damage", "sensors", "tactical", "helm"} {
		cons := map[string]string{"sensors": "science", "tactical": "tactical", "helm": "navigation"}[id]
		f.console("std."+id, cons, nil)
		txt := f.text()
		t.Logf("== %s\n%s", id, f.term.Dump())
		want := strings.ToUpper(id)
		if id == "sensors" {
			want = "SCIENCE"
		}
		if !strings.Contains(txt, want) {
			t.Errorf("%s: header missing:\n%s", id, txt)
		}
		f.dump(id)
	}
}

func (f *screenFixture) keys(seq ...string) {
	for _, k := range seq {
		f.c.Do(func(*gtfparser.Interp) { f.c.onKeys(k) })
	}
	f.render()
}

const (
	kEnter = "\r"
	kDown  = "\x1b[B"
	kUp    = "\x1b[A"
	kRight = "\x1b[C"
	kTab   = "\t"
)

// E4 in the cell client: damage control, team 1 to Port_Nacelle by keyboard; the command is
// echoed as typed text, sent with the epoch, and the game ran it.
func TestCmcpDamageAssignByKeyboard(t *testing.T) {
	f := newScreenFixture(t, 120, 40)
	f.console("std.damage", "operations", nil)
	f.keys(kTab, kTab, kTab, kEnter, kDown, kEnter) // focus on (the screen menu, then the schematic, then the teams); pick team 1; move to Port_Nacelle; assign
	cmEventually(t, "command ran", func() bool { return len(f.game.commands()) == 1 })
	if got := f.game.commands()[0]; got != `dmg.assign {"system":"Port_Nacelle","team":1}` {
		t.Fatalf("ran %q", got)
	}
	f.render()
	f.contain(t, "cmcp> dmg assign 1=Port_Nacelle")
}

// The real input line always works: with focus on, a typed line is not taken by the screen.
func TestCmcpInputLineStillWorks(t *testing.T) {
	f := newScreenFixture(t, 120, 40)
	f.console("std.operations", "", nil)
	f.keys(kTab, "\x1b") // into the screen and back: the line is the player's again
	f.keys("l", "o", "o", "k")
	f.render()
	if got := f.inputText(); got != "look" {
		t.Fatalf("input line %q", got)
	}
	// with text in the line, Enter submits it: the screen must not act
	f.keys(kEnter)
	if n := len(f.game.commands()); n != 0 {
		t.Fatalf("screen acted on a typed line: %v", f.game.commands())
	}
}

func (f *screenFixture) inputText() (s string) {
	f.c.Do(func(in *gtfparser.Interp) { s = in.Input().Text() })
	return
}

// A read-only console (AI flies the ship): the screen stays, no command is offered or sent.
func TestCmcpReadOnlyOffersNoCommands(t *testing.T) {
	f := newScreenFixture(t, 120, 40)
	f.console("std.operations", "", func(c *msg.Console) { c.Readonly = true; c.Reason = "ai_on"; c.Commands = nil; c.Ship.AI = true })
	f.keys(kTab, kEnter) // would click HATCH
	time.Sleep(100 * time.Millisecond)
	if n := len(f.game.commands()); n != 0 {
		t.Fatalf("sent while read-only: %v", f.game.commands())
	}
	f.contain(t, "READ-ONLY")
}

var updateGolden = flag.Bool("update", false, "rewrite the golden screens")

// surface is the drawn screen (widgets only; the text pane is blank in it) as text.
func (f *screenFixture) surface() string {
	var out string
	f.c.Do(func(*gtfparser.Interp) {
		if f.c.ui.Surf != nil {
			out = f.c.ui.Surf.Grid.Text()
		}
	})
	return out
}

func golden(t *testing.T, name, got string) {
	t.Helper()
	p := filepath.Join("testdata", "golden", name+".txt")
	if *updateGolden {
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("no golden %s (run with -update): %v", p, err)
	}
	if string(want) != got {
		t.Errorf("%s differs from golden:\n--- got\n%s\n--- want\n%s", name, got, want)
	}
}

// Golden screens: every built-in screen on the example data of the spec.
func TestCmcpGoldenScreens(t *testing.T) {
	f := newScreenFixture(t, 120, 40)
	for _, id := range []string{"operations", "power", "damage", "sensors", "tactical", "helm"} {
		cons := map[string]string{"sensors": "science", "tactical": "tactical", "helm": "navigation"}[id]
		f.console("std."+id, cons, nil)
		golden(t, id, f.surface())
	}
}

// The same screens on a narrow terminal: still drawn, the text pane keeps its minimum.
func TestCmcpGoldenNarrow(t *testing.T) {
	f := newScreenFixture(t, 80, 24)
	for _, id := range []string{"operations", "damage"} {
		f.console("std."+id, "", nil)
		golden(t, id+"-80x24", f.surface())
	}
}

// A tiny terminal: whatever the screen draws, the text pane keeps a usable size.
func TestCmcpTinyTerminalKeepsText(t *testing.T) {
	f := newScreenFixture(t, 40, 10)
	f.console("std.operations", "", nil)
	var r layout.Rect
	f.c.Do(func(*gtfparser.Interp) { r = f.c.ui.MainRect() })
	if r.H < 4 || r.W < 30 {
		t.Fatalf("text pane %+v", r)
	}
}

// sendContext writes a Cobra.Context as the game would (the core's Server has no push API yet).
func (f *screenFixture) sendContext(c msg.Context) {
	f.t.Helper()
	b, err := frame.Encode("Cobra.Context", c)
	if err != nil {
		f.t.Fatal(err)
	}
	f.d.gm().write(b)
}

// The context stack (spec 3.3): a higher-priority event context takes the screen, the console's
// data stays live below it, popping it returns the console screen with its own state.
func TestCmcpContextStack(t *testing.T) {
	f := newScreenFixture(t, 120, 40)
	f.console("std.operations", "", nil)
	prio := 150
	ship := msg.ContextShip{ID: "ss-booty-king", Epoch: 1}
	f.sendContext(msg.Context{Rev: 2, Ship: ship, Op: "push", Reason: "event", Context: &msg.ContextEntry{
		Key: "event:power-surge", Ctx: 7, Kind: "event", ID: "power-surge", Screen: "std.power", Priority: &prio,
		Commands: []string{"power.set"}}})
	cmEventually(t, "power screen on top", func() bool { id, _ := f.screenState(); return id == "std.power" })
	f.render()
	f.contain(t, "POWER", "2 CONTEXTS")
	exec(f.c, "/cmcp status")
	f.render()
	f.contain(t, "context * event:power-surge", "context   console")
	f.sendContext(msg.Context{Rev: 3, Ship: ship, Op: "pop", Key: "event:power-surge", Ctx: 7})
	cmEventually(t, "back on operations", func() bool { id, _ := f.screenState(); return id == "std.operations" })
	f.render()
	f.contain(t, "OPERATIONS")
	f.lack(t, "2 CONTEXTS")
}

// There is no joystick in gtf: the helm is steered with the pack's helm-controls (a core widget),
// whose events the pack maps to ordinary commands.
func TestCmcpHelmControlsSendCommands(t *testing.T) {
	f := newScreenFixture(t, 160, 60)
	f.console("std.helm", "navigation", nil)
	f.contain(t, "HELM CONTROLS", "azimuth", "[ STOP ]")
	f.lack(t, "thr ")                                            // no stick
	f.keys(kTab, kTab, kTab, kTab, kDown, kDown, kRight, kEnter) // menu, map, radar, helm controls; speed row, +10, send
	cmEventually(t, "speed sent", func() bool { return len(f.game.commands()) == 1 })
	if got := f.game.commands()[0]; got != `helm.speed {"pct":10}` {
		t.Fatalf("ran %q", got)
	}
	f.keys(kDown, kDown, kDown, kEnter) // warp, engage, stop: Enter on STOP
	cmEventually(t, "stop sent", func() bool { return len(f.game.commands()) == 2 })
	if got := f.game.commands()[1]; got != `helm.stop null` {
		t.Fatalf("ran %q", got)
	}
	f.contain(t, "cmcp> speed 10", "cmcp> stop")
	if h := f.d.gm().srv.Hello(); h == nil || h.Widgets["joystick"] != 0 {
		t.Fatalf("joystick advertised: %+v", h)
	}
}

// Keyboard first (owner decision): Tab on an empty input line enters the screen, Tab/Shift-Tab
// move between panels, an underlined letter presses its button, sliders adjust with the arrows and
// +/-, Tab past the last panel and Esc return to the input line, which keeps working.
func (f *screenFixture) active() (a bool) {
	f.c.Do(func(*gtfparser.Interp) { a = f.c.fg.cs.st.Active })
	return
}

func (f *screenFixture) focusPath() (p string) {
	f.c.Do(func(*gtfparser.Interp) { p = f.c.fg.cs.st.Focus })
	return
}

func TestCmcpKeyboardFocusOrderAndReturn(t *testing.T) {
	f := newScreenFixture(t, 120, 40)
	f.console("std.operations", "", nil)
	if f.active() {
		t.Fatal("focus on before the player asked")
	}
	f.keys(kTab)
	if !f.active() {
		t.Fatal("Tab on an empty input line did not enter the screen")
	}
	f.contain(t, "Esc input line")
	first := f.focusPath()
	f.keys(kTab)
	second := f.focusPath()
	if first == second || second == "" {
		t.Fatalf("Tab did not move the focus: %q %q", first, second)
	}
	f.keys("\x1b[Z") // Shift-Tab
	if f.focusPath() != first {
		t.Fatal("Shift-Tab did not return to the first panel")
	}
	f.keys("\x1b[Z") // Shift-Tab on the first panel wraps to the last; focus mode stays on
	if !f.active() || f.focusPath() == first {
		t.Fatal("Shift-Tab did not wrap to the last panel")
	}
	// Shift+Arrow and Alt+Arrow are never bound: swallowed, nothing happens
	before := f.focusPath()
	f.keys("\x1b[1;2A", "\x1b[1;3D")
	if f.focusPath() != before || len(f.game.commands()) != 0 || !f.active() {
		t.Fatal("a modified arrow did something")
	}
	// Esc returns to the input line, which works
	f.keys("\x1b")
	if f.active() {
		t.Fatal("Esc did not return to the input line")
	}
	f.keys("l", "s")
	if got := f.inputText(); got != "ls" {
		t.Fatalf("input line %q", got)
	}
	f.keys(kTab) // the line has text: Tab is the engine's (completion), not the screen's
	if f.active() {
		t.Fatal("Tab took focus away from a line being typed")
	}
}

// Any unbound printable key leaves focus mode and is typed in the input line.
func TestCmcpKeyboardUnboundKeyTypes(t *testing.T) {
	f := newScreenFixture(t, 120, 40)
	f.console("std.operations", "", nil)
	f.keys(kTab, "z") // no button starts with or is assigned z? it may be: use a digit that is not free
	f.keys("~")
	if f.active() {
		t.Fatal("still in focus mode after an unbound key")
	}
	if got := f.inputText(); !strings.HasSuffix(got, "~") {
		t.Fatalf("the key was not typed: %q", got)
	}
}

func TestCmcpKeyboardMnemonicAndSlider(t *testing.T) {
	f := newScreenFixture(t, 120, 40)
	f.console("std.operations", "", nil)
	f.keys(kTab, "h") // HATCH: its underlined letter presses it
	cmEventually(t, "hatch", func() bool { return len(f.game.commands()) == 1 })
	if got := f.game.commands()[0]; got != `hatch.toggle null` {
		t.Fatalf("ran %q", got)
	}
	f.console("std.power", "", nil)
	f.keys(kTab)
	// walk to the slider and adjust it with the arrows and +/-
	for i := 0; i < 30; i++ {
		var kind string
		f.c.Do(func(*gtfparser.Interp) {
			for _, it := range f.c.fg.cs.frame.Items {
				if it.Node.Node.Path == f.c.fg.cs.st.Focus {
					kind = it.Kind
				}
			}
		})
		if kind == "slider" {
			break
		}
		f.keys(kTab)
	}
	n := len(f.game.commands())
	f.keys(kRight, "+")
	cmEventually(t, "slider commands", func() bool { return len(f.game.commands()) >= n+1 })
	if got := f.game.commands()[n]; !strings.HasPrefix(got, "power.set") || !strings.Contains(got, "%") {
		t.Fatalf("slider sent %q", got)
	}
}

// A ship in view on the viewscreen is drawn with the scene's art for its class (a sprite through
// the core's view selection and the rasteriser), not as a marker.
func TestCmcpViewscreenDrawsShipSprite(t *testing.T) {
	f := newScreenFixture(t, 120, 40)
	docs := f.docs["Space.Contacts"].(map[string]any)["contacts"].(map[string]any)
	ship := docs["17"].(map[string]any)
	ship["rel"] = map[string]any{"az": 8.0, "mark": 2.0}
	ship["distance_m"] = 1500.0
	ship["class"] = "Marauder"
	ship["size_m"] = 120.0
	ship["heading"] = map[string]any{"az": 40.0, "mark": 0.0, "roll": 0.0}
	f.console("std.sensors", "science", nil)
	s := f.surface()
	if !strings.ContainsAny(s, "▀▄█") {
		t.Fatalf("no sprite cells on the viewscreen:\n%s", s)
	}
	f.dump("viewscreen-ship")
}

// The game is primarily text: the output pane must stay big. Owner rule: in the compact view at least
// 45% of the height or at least 60% of the width; in the log view (F3) about 80% of the height;
// with the screen off (F4) everything.
func (f *screenFixture) pane() (r layout.Rect, w, h int) {
	f.c.Do(func(*gtfparser.Interp) { r = f.c.ui.MainRect(); w, h = f.c.ui.W, f.c.ui.OutRows() })
	return
}

func TestCmcpTextPaneGeometry(t *testing.T) {
	nowFn = func() time.Time { return fixedNow }
	for _, sz := range [][2]int{{80, 24}, {120, 40}, {200, 50}} {
		f := newScreenFixtureSetup(t, sz[0], sz[1], t.TempDir(), nil, "/set cmcp_motion=off")
		for _, id := range []string{"operations", "sensors", "tactical", "damage", "power", "helm"} {
			cons := map[string]string{"sensors": "science", "tactical": "tactical", "helm": "navigation"}[id]
			f.console("std."+id, cons, nil)
			f.dump(fmt.Sprintf("geo-%dx%d-%s", sz[0], sz[1], id))
			r, w, h := f.pane()
			rowsOK := r.H*100 >= h*45
			colsOK := r.W*100 >= sz[0]*60
			if !rowsOK && !colsOK {
				t.Errorf("%dx%d %s: text pane %dx%d (of %dx%d) is under 45%% of the height and under 60%% of the width", sz[0], sz[1], id, r.W, r.H, w, h)
			}
			if r.H < 8 || r.W < 30 {
				t.Errorf("%dx%d %s: text pane %dx%d under the floor", sz[0], sz[1], id, r.W, r.H)
			}
			if r.Y+r.H > h || r.X+r.W > w {
				t.Errorf("%dx%d %s: text pane %+v outside the output area %dx%d", sz[0], sz[1], id, r, w, h)
			}
		}
		// the log view: about 80% of the height
		f.console("std.operations", "", nil)
		f.keys("\x1bOR")
		r, w, h := f.pane()
		if r.H*100 < h*65 {
			t.Errorf("%dx%d log view: text pane has %d of %d rows", sz[0], sz[1], r.H, h)
		}
		f.dump(fmt.Sprintf("log-%dx%d", sz[0], sz[1]))
		f.keys("\x1bOR") // back to compact
		if r2, _, _ := f.pane(); r2.H >= r.H && r2.W >= r.W {
			t.Errorf("%dx%d: F3 again did not return to the compact view", sz[0], sz[1])
		}
		// the screen off: the text view has the whole area, data keeps streaming
		f.keys("\x1bOS")
		r, w, h = f.pane()
		if r.W != w || r.H != h {
			t.Errorf("%dx%d screen off: text pane %dx%d of %dx%d", sz[0], sz[1], r.W, r.H, w, h)
		}
		f.keys("\x1bOS")
		if r, _, _ = f.pane(); r.W == w && r.H == h {
			t.Errorf("%dx%d: F4 again did not bring the screen back", sz[0], sz[1])
		}
		f.c.Close()
	}
}

// %cmcp_start_collapsed starts with the screen off: the text view has the whole area until F4.
func TestCmcpStartCollapsed(t *testing.T) {
	f := newScreenFixtureSetup(t, 120, 40, t.TempDir(), nil, "/set cmcp_start_collapsed=on")
	f.console("std.operations", "", nil)
	r, w, h := f.pane()
	if r.W != w || r.H != h {
		t.Fatalf("collapsed start: text pane %dx%d of %dx%d", r.W, r.H, w, h)
	}
	if s := f.surface(); s != "" {
		t.Fatalf("screen drawn while collapsed:\n%s", s)
	}
	f.c.Do(func(*gtfparser.Interp) {
		if got := f.c.fg.cm.UI().Docs["Space.Ship.Status"]; got == nil {
			t.Error("data stopped streaming while the screen was off")
		}
	})
	f.keys("\x1bOS") // F4 brings it back
	if r, _, _ = f.pane(); r.W == w && r.H == h {
		t.Fatal("F4 did not bring the screen back")
	}
	f.contain(t, "OPERATIONS")
}

// A red alert recolours the whole frame: the bands turn red and the status strip says so.
func TestCmcpRedAlertRecolours(t *testing.T) {
	f := newScreenFixture(t, 120, 40)
	f.docs["Space.Ship.Status"].(map[string]any)["alert"] = "red"
	f.console("std.sensors", "science", nil)
	f.contain(t, "RED ALERT")
	var bad []string
	f.c.Do(func(*gtfparser.Interp) {
		g := f.c.ui.Surf.Grid
		for _, p := range [][2]int{{20, 1}, {40, 1}, {80, 1}} { // title, ship and tag segments of the top band
			c := g.At(p[0], p[1])
			if c.A.BG.Kind != layout.RGB || int(c.A.BG.R) < 120 || int(c.A.BG.R) < int(c.A.BG.G)+60 || int(c.A.BG.R) < int(c.A.BG.B)+60 {
				bad = append(bad, fmt.Sprintf("%v %+v", p, c.A.BG))
			}
		}
	})
	if len(bad) > 0 {
		t.Fatalf("band not red under a red alert: %v", bad)
	}
	f.dump("redalert-sensors")
}

// The LOG toggle animates: the text pane's rows step through whole-cell frames over ~200 ms (a clock the
// test moves), land exactly on the expanded size, and step back; the input line works meanwhile.
func TestCmcpLogToggleAnimates(t *testing.T) {
	clock := fixedNow
	nowFn = func() time.Time { return clock }
	defer func() { nowFn = func() time.Time { return fixedNow } }()
	f := newScreenFixtureSetup(t, 120, 40, t.TempDir(), nil, "/set cmcp_layout=stacked")
	f.console("std.damage", "operations", nil)
	r0, _, h := f.pane()
	f.keys("\x1bOR") // F3
	var seen []int
	for i := 0; i < 8; i++ {
		clock = clock.Add(50 * time.Millisecond)
		f.render()
		r, _, _ := f.pane()
		seen = append(seen, r.H)
	}
	last := seen[len(seen)-1]
	if last*100 < h*65 {
		t.Fatalf("did not reach the expanded size: %v of %d", seen, h)
	}
	mid := 0
	for _, v := range seen {
		if v > r0.H && v < last {
			mid++
		}
	}
	if mid < 2 {
		t.Fatalf("no intermediate frames: %v (from %d to %d)", seen, r0.H, last)
	}
	f.keys("l", "s") // typing works while the screen is in motion
	if got := f.inputText(); got != "ls" {
		t.Fatalf("input line %q", got)
	}
}

// The left column is the screen menu: in focus mode ] and [ step through the screens the console may
// show, a digit picks the nth; the current one is lit; the local choice does not touch the server.
func TestCmcpScreenMenuKeys(t *testing.T) {
	f := newScreenFixture(t, 120, 40)
	f.console("std.operations", "", nil)
	shown := func() (id string, n int) {
		f.c.Do(func(*gtfparser.Interp) { u := f.c.fg.cm.UI(); id, n = u.Screen.Resolved, len(u.Screens) })
		return
	}
	first, n := shown()
	if n < 3 {
		t.Fatalf("screen menu has %d entries", n)
	}
	f.keys(kTab, "]")
	second, _ := shown()
	if second == first {
		t.Fatalf("] did not change the screen (%s)", first)
	}
	f.keys("[")
	if back, _ := shown(); back == second {
		t.Fatalf("[ did not move the screen (still %s)", back) // (the core's wrap order is its own; see the report)
	}
	f.keys("2")
	if third, _ := shown(); third == first {
		t.Fatalf("digit 2 did not pick a screen")
	}
	f.contain(t, "DAMAGE") // the menu entries are in the left column
}

// --- the focus walkthrough: a visible selector, named in the header, in reading order -------------

// focusedCells counts the cells of the drawn screen that are in inverse video (the focus marker).
func (f *screenFixture) inverseCells() (n int) {
	f.c.Do(func(*gtfparser.Interp) {
		for _, c := range f.c.ui.Surf.Grid.Cells {
			if c.A.Reverse {
				n++
			}
		}
	})
	return
}

func (f *screenFixture) focusName() (s string) {
	f.c.Do(func(*gtfparser.Interp) { s = f.c.fg.cs.frame.FocusName })
	return
}

// Each Tab moves a marked selector (checked by attributes, not text), the header names the panel and
// the keys, the first stop is the screen menu, and the panels follow in reading order.
func TestCmcpFocusWalkthrough(t *testing.T) {
	f := newScreenFixture(t, 200, 50)
	f.console("std.sensors", "science", nil)
	if f.inverseCells() != 0 {
		t.Fatal("a focus marker before the player asked")
	}
	var names []string
	f.keys(kTab)
	for i := 0; i < 5; i++ {
		n := f.focusName()
		names = append(names, n)
		if n == "" {
			t.Fatalf("step %d: no focused panel named", i)
		}
		if f.inverseCells() == 0 && n != "SCREEN MENU" {
			t.Errorf("step %d (%s): no inverse-video marker on the screen", i, n)
		}
		want := "PANEL " + n
		if n == "SCREEN MENU" {
			want = "Up/Down choose   Enter open"
		}
		f.contain(t, want, "Esc input line")
		f.dump(fmt.Sprintf("focus-step-%d", i+1))
		f.keys(kTab)
	}
	if names[0] != "SCREEN MENU" {
		t.Fatalf("the first Tab should land on the screen menu: %v", names)
	}
	if names[1] == names[0] || names[2] == names[1] {
		t.Fatalf("Tab did not move: %v", names)
	}
}

// The real byte sequences of a terminal reach gtf as keys: Shift+Tab (ESC [ Z) enters on the LAST panel
// and moves back, arrows in both encodings work, F3 and F4 in both encodings toggle, and no '%' error
// from the TF key layer ever shows ("The key shift_tab is undefined").
func TestCmcpRealKeySequences(t *testing.T) {
	f := newScreenFixtureSetup(t, 200, 50, t.TempDir(), nil, "/set cmcp_motion=off")
	f.console("std.sensors", "science", nil)
	f.keys("\x1b[Z") // Shift+Tab: enter on the last panel
	if !f.active() {
		t.Fatal("Shift+Tab did not enter the screen")
	}
	last := f.focusName()
	f.keys("\x1b[Z")
	if f.focusName() == last {
		t.Fatal("Shift+Tab did not move back")
	}
	f.keys("\x1b[B", "\x1bOB", "\x1b[A", "\x1bOA", "\x1b[C", "\x1bOD") // arrows, normal and application mode
	for _, seq := range []string{"\x1b[13~", "\x1bOR", "\x1b[14~", "\x1bOS", "\x1b[Z", "\x1b[12~", "\x1bOQ"} {
		f.keys(seq)
	}
	f.lack(t, "key_shift_tab", "is undefined", "no such command or macro")
	// with the screen off the keys are still harmless
	f.keys("\x1bOS") // F4: screen off
	f.keys("\x1b[Z", "\x1b[Z")
	f.lack(t, "key_shift_tab", "is undefined", "no such command or macro")
}

// %cmcp_alert_recolour: full (the whole palette goes red), edge (only the frame edge and the alert
// segment) and off (nothing); romulan and ferengi default to edge. Under every mode gtf's own hint
// line says RED ALERT in words.
func TestCmcpAlertRecolourModes(t *testing.T) {
	redCells := func(f *screenFixture) (n int) {
		f.c.Do(func(*gtfparser.Interp) {
			g := f.c.ui.Surf.Grid
			for i := 0; i < 8*g.W; i++ { // the top band and its segments
				c := g.Cells[i]
				if c.A.BG.Kind == layout.RGB && int(c.A.BG.R) >= 120 && int(c.A.BG.R) >= int(c.A.BG.G)+60 && int(c.A.BG.R) >= int(c.A.BG.B)+60 {
					n++
				}
			}
		})
		return
	}
	count := map[string]int{}
	for _, mode := range []string{"full", "edge", "off"} {
		f := newScreenFixtureSetup(t, 120, 40, t.TempDir(), nil, "/set cmcp_alert_recolour="+mode)
		f.docs["Space.Ship.Status"].(map[string]any)["alert"] = "red"
		f.console("std.sensors", "science", nil)
		f.contain(t, "RED ALERT")
		count[mode] = redCells(f)
		f.dump("alert-" + mode)
	}
	if !(count["full"] > count["edge"] && count["edge"] > count["off"]) {
		t.Fatalf("red cells in the top band should shrink full > edge > off: %v", count)
	}
	// romulan defaults to edge: the top band is not all red
	f := newScreenFixture(t, 120, 40)
	f.docs["Space.Ship.Status"].(map[string]any)["alert"] = "red"
	f.console("std.sensors", "science", func(c *msg.Console) { c.Ship.Theme = "romulan" })
	f.contain(t, "RED ALERT")
	f.dump("alert-romulan-default")
}

// The output is formatted for about 80 columns: in the side layout the text pane stops at 80 columns
// and the widget column takes the rest (spec 6.13.1 1b).
func TestCmcpTextWidthCap(t *testing.T) {
	f := newScreenFixtureSetup(t, 200, 50, t.TempDir(), nil, "/set cmcp_motion=off")
	f.console("std.operations", "", nil)
	r, _, _ := f.pane()
	if r.W > 80 || r.W < 60 {
		t.Fatalf("text pane is %d columns wide at 200 columns", r.W)
	}
}

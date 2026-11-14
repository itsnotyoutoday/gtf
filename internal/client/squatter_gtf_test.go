// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package client

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/itsnotyoutoday/gtf/internal/squatter"
	"github.com/itsnotyoutoday/gtf/internal/squatter/auth"
	"github.com/itsnotyoutoday/gtf/internal/squatter/proto"
	"github.com/itsnotyoutoday/gtf/internal/squatter/wire"
	"github.com/itsnotyoutoday/gtfparser"
)

func gtfPeer(t *testing.T, c *Client, role, name string) *wire.Client {
	t.Helper()
	var code string
	c.Do(func(*gtfparser.Interp) {
		info, err := c.sq.sq.Pair(role)
		if err != nil {
			t.Fatal(err)
		}
		code = info.Code
	})
	key, _ := auth.GenerateKey()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pc, err := wire.Dial(ctx, "wss://"+v(c, "squatter_addr"), wire.ClientOptions{Key: key, PairCode: code, DeviceName: name,
		PinnedFingerprint: v(c, "squatter_fingerprint"), ExpectKind: proto.KindTFSquatterd,
		Ext: map[string]any{"gtf": map[string]any{"v": 1, "features": []string{"author", "presence", "typing", "naws", "worlds"}}}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pc.Close() })
	return pc
}

func awaitExt(t *testing.T, pc *wire.Client, typ string, pred func(map[string]any) bool) map[string]any {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case ev := <-pc.Events():
			var raw []byte
			switch e := ev.(type) {
			case wire.Ext:
				if e.T == typ {
					raw = e.Raw
				}
			case wire.WorldChanged:
				if typ == "world.changed" {
					raw, _ = json.Marshal(e.WorldChangedMsg)
				}
			}
			if raw == nil {
				continue
			}
			var m map[string]any
			_ = json.Unmarshal(raw, &m)
			if pred == nil || pred(m) {
				return m
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %s", typ)
		}
	}
}

func put(t *testing.T, pc *wire.Client, w proto.TFWorld, base uint64, extra map[string]any) (map[string]any, *wire.ProtoError) {
	t.Helper()
	f := map[string]any{"world": w, "baseRev": base}
	for k, x := range extra {
		f[k] = x
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r, err := pc.RawRequest(ctx, "world.put", f)
	if err != nil {
		var pe *wire.ProtoError
		if errors.As(err, &pe) {
			return nil, pe
		}
		t.Fatal(err)
	}
	var m map[string]any
	_ = json.Unmarshal(r.(wire.Ext).Raw, &m)
	return m, nil
}

// A peer edits the world database: the engine's database changes, the world file is rewritten (with a
// backup of what the user had), local edits notify peers, and a stale revision is rejected.
func TestGTFWorldEditsAgainstTheEngine(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "worlds")
	hand := "; my hand-edited worlds\n/test addworld(\"old\", \"\", \"o.example\", \"1\")\n"
	if err := os.WriteFile(file, []byte(hand), 0o644); err != nil {
		t.Fatal(err)
	}
	c, scr, _ := squatClient(t)
	c.Do(func(in *gtfparser.Interp) {
		in.Exec("/def WORLDFILE = " + file)
		in.Exec(`/test addworld("old", "", "o.example", "1")`)
	})
	pc := gtfPeer(t, c, "admin", "phone")
	other := gtfPeer(t, c, "admin", "tablet")

	res, perr := put(t, pc, proto.TFWorld{Name: "nw", Host: "n.example", Port: "4000", Char: "me"}, 0, map[string]any{"password": "sekrit"})
	if perr != nil || res["created"] != true || res["saved"] != true {
		t.Fatalf("%v %v", res, perr)
	}
	rev := uint64(res["world"].(map[string]any)["rev"].(float64))
	// the engine has it, with the password, and nothing leaked into what peers saw
	var w gtfparser.WorldInfo
	c.Do(func(in *gtfparser.Interp) { w, _ = in.World("nw") })
	if w.Host != "n.example" || w.Password != "sekrit" || w.Character != "me" || w.Rev != rev {
		t.Fatalf("%+v", w)
	}
	// the file was rewritten the way /saveworld does, 0600, and the hand-edited one is kept
	got, _ := os.ReadFile(file)
	if !strings.Contains(string(got), `addworld("nw", "", "n.example", "4000", "me", "sekrit")`) || !strings.Contains(string(got), `addworld("old"`) {
		t.Fatalf("world file:\n%s", got)
	}
	if st, _ := os.Stat(file); st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", st.Mode())
	}
	baks, _ := filepath.Glob(file + ".gtf-bak-*")
	if len(baks) != 1 {
		t.Fatalf("backups: %v", baks)
	}
	if b, _ := os.ReadFile(baks[0]); string(b) != hand {
		t.Fatalf("backup content %q", b)
	}
	// a second edit does not pile up backups (gtf wrote the file itself)
	res, perr = put(t, pc, proto.TFWorld{Name: "nw", Host: "n2.example", Port: "4001"}, rev, nil)
	if perr != nil {
		t.Fatal(perr)
	}
	rev2 := uint64(res["world"].(map[string]any)["rev"].(float64))
	if baks, _ = filepath.Glob(file + ".gtf-bak-*"); len(baks) != 1 {
		t.Fatalf("backups: %v", baks)
	}
	c.Do(func(in *gtfparser.Interp) { w, _ = in.World("nw") })
	if w.Password != "sekrit" || w.Host != "n2.example" || w.Character != "" {
		t.Fatalf("password kept, other fields replaced: %+v", w)
	}
	// the tablet was told, with author
	ch := awaitExt(t, other, "world.changed", func(m map[string]any) bool { return m["name"] == "nw" && m["op"] == "put" && m["created"] != true })
	if ch["author"] != "phone" {
		t.Fatalf("%v", ch)
	}
	// the owner sees an audit line without the password
	waitScreen(t, c, scr, "phone (admin) updated world nw [host,port,char]")
	if strings.Contains(scr.Text(), "sekrit") {
		t.Fatal("password on screen")
	}

	// typed locally: the engine notifies, peers hear it as "local", and the revision moves on
	c.Do(func(in *gtfparser.Interp) { in.ExecFull(`/test addworld("nw", "", "typed.example", "9")`) })
	ch = awaitExt(t, other, "world.changed", func(m map[string]any) bool { return m["author"] == "local" })
	if ch["name"] != "nw" || uint64(ch["rev"].(float64)) <= rev2 {
		t.Fatalf("%v", ch)
	}
	// the phone still holds rev2: its update is rejected with the current world
	_, perr = put(t, pc, proto.TFWorld{Name: "nw", Host: "stale", Port: "1"}, rev2, nil)
	if perr == nil || perr.Code != "conflict" || !strings.Contains(string(perr.Data), "typed.example") || strings.Contains(string(perr.Data), "sekrit") {
		t.Fatalf("%+v", perr)
	}
	c.Do(func(in *gtfparser.Interp) { w, _ = in.World("nw") })
	if w.Host != "typed.example" {
		t.Fatal("a rejected update must not change anything")
	}
	// local /unworld and /renameworld reach peers too
	c.Do(func(in *gtfparser.Interp) { in.ExecFull("/renameworld nw nw2") })
	ch = awaitExt(t, other, "world.changed", func(m map[string]any) bool { return m["op"] == "rename" })
	if ch["oldName"] != "nw" || ch["name"] != "nw2" || ch["author"] != "local" {
		t.Fatalf("%v", ch)
	}
	c.Do(func(in *gtfparser.Interp) { in.ExecFull("/unworld nw2") })
	ch = awaitExt(t, other, "world.changed", func(m map[string]any) bool { return m["op"] == "delete" })
	if ch["name"] != "nw2" {
		t.Fatalf("%v", ch)
	}
	// a connected world cannot be deleted by a peer
	c.Do(func(in *gtfparser.Interp) { in.ExecFull(`/test addworld("live", "", "localhost", "4000")`) })
	c.Do(func(in *gtfparser.Interp) { in.ExecFull("/connect -b live") })
	var lrev uint64
	c.Do(func(in *gtfparser.Interp) { x, _ := in.World("live"); lrev = x.Rev })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := pc.RawRequest(ctx, "world.delete", map[string]any{"name": "live", "baseRev": lrev})
	var pe *wire.ProtoError
	if !errors.As(err, &pe) || pe.Code != proto.ErrBusy {
		t.Fatalf("deleting a connected world: %v", err)
	}
	// plain peers keep getting an error
	plain, _ := pairPeer(t, c, "admin", "oldie")
	if _, err := plain.PutWorld(ctx, wire.WorldPutReq{World: proto.TFWorld{Name: "z", Host: "h", Port: "1"}}); err == nil {
		t.Fatal("a plain peer must still get an error")
	}
	// a hand edit made later is backed up again before gtf overwrites it
	if err := os.WriteFile(file, []byte("; edited again\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, perr = put(t, pc, proto.TFWorld{Name: "z2", Host: "h", Port: "2"}, 0, nil); perr != nil {
		t.Fatal(perr)
	}
	if baks, _ = filepath.Glob(file + ".gtf-bak-*"); len(baks) != 2 {
		t.Fatalf("backups: %v", baks)
	}
}

func TestSquatterWorldSaveOffAndNoWorldfile(t *testing.T) {
	c, _, _ := squatClient(t, "/set squatter_world_save=off")
	pc := gtfPeer(t, c, "admin", "phone")
	res, perr := put(t, pc, proto.TFWorld{Name: "a", Host: "h", Port: "1"}, 0, nil)
	if perr != nil || res["saved"] != false {
		t.Fatalf("%v %v", res, perr)
	}
	c2, _, _ := squatClient(t) // WORLDFILE not defined (NoRC, no stdlib macro)
	c2.Do(func(in *gtfparser.Interp) { in.Exec("/undef WORLDFILE") })
	pc2 := gtfPeer(t, c2, "admin", "phone")
	res, perr = put(t, pc2, proto.TFWorld{Name: "a", Host: "h", Port: "1"}, 0, nil)
	if perr != nil || res["saved"] != false || !strings.Contains(res["saveError"].(string), "WORLDFILE") {
		t.Fatalf("%v %v", res, perr)
	}
}

func TestWriteWorldFileBackupsAndPrune(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "w")
	var last string
	if bk, err := writeWorldFile(p, "a\n", &last); err != nil || bk != "" {
		t.Fatalf("new file: %q %v", bk, err)
	}
	if bk, err := writeWorldFile(p, "b\n", &last); err != nil || bk != "" {
		t.Fatalf("our own file needs no backup: %q %v", bk, err)
	}
	for i := 0; i < 8; i++ {
		os.WriteFile(p, []byte("hand "+string(rune('a'+i))+"\n"), 0o644)
		if bk, err := writeWorldFile(p, "c"+string(rune('a'+i))+"\n", &last); err != nil || bk == "" {
			t.Fatalf("hand edit %d: %q %v", i, bk, err)
		}
	}
	if m, _ := filepath.Glob(p + ".gtf-bak-*"); len(m) != 5 {
		t.Fatalf("the 5 newest backups are kept: %v", m)
	}
	// identical content: no backup, still fine
	os.WriteFile(p, []byte("same\n"), 0o600)
	if bk, err := writeWorldFile(p, "same\n", nil); err != nil || bk != "" {
		t.Fatalf("%q %v", bk, err)
	}
	// through a symlink the target is replaced, not the link
	target := filepath.Join(dir, "real")
	os.WriteFile(target, []byte("x\n"), 0o600)
	link := filepath.Join(dir, "link")
	os.Symlink(target, link)
	if _, err := writeWorldFile(link, "y\n", nil); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Lstat(link); fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("the symlink was replaced")
	}
	if b, _ := os.ReadFile(target); string(b) != "y\n" {
		t.Fatalf("%q", b)
	}
}

// The squatter_naws policy through the real client: the width the game connection holds follows it.
func TestGTFNAWSThroughTheClient(t *testing.T) {
	c, _, d := squatClient(t, "/set squatter_naws=active")
	c.Do(func(in *gtfparser.Interp) { in.Exec("/addworld w god pw localhost 4000"); in.Exec("/connect w") })
	srv := <-d.srv
	srv.Expect([]byte("connect god pw\r\n"))
	pc := gtfPeer(t, c, "operator", "phone")
	ss := pc.Sessions()
	if len(ss) != 1 {
		t.Fatalf("%+v", ss)
	}
	id := ss[0].ID
	cols := func() int {
		var n int
		c.Do(func(*gtfparser.Interp) {
			if s := c.squatSession(id); s != nil && s.conn != nil {
				n = s.conn.Env().Snapshot().Cols
			}
		})
		return n
	}
	var local int
	c.Do(func(*gtfparser.Interp) { local = c.ui.W })
	if cols() != local {
		t.Fatalf("cols %d, terminal %d", cols(), local)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := pc.Attach(ctx, id, nil, 0); err != nil {
		t.Fatal(err)
	}
	if err := pc.Resize(ctx, id, 150, 40); err != nil {
		t.Fatal(err)
	}
	if err := pc.Send(ctx, id, "hi"); err != nil {
		t.Fatal(err)
	}
	srv.Expect([]byte("hi\r\n"))
	eventually(t, "the peer's width after its input", func() bool { return cols() == 150 })
	// the local user types: the terminal's width is back
	c.Do(func(*gtfparser.Interp) { c.onKeys("look\r") })
	eventually(t, "the terminal's width after local input", func() bool { return cols() == local })
	// the policy variable applies at once: local means peers never matter
	c.Do(func(in *gtfparser.Interp) { in.Exec("/set squatter_naws=local"); c.squatCheck() })
	_ = pc.Send(ctx, id, "again")
	time.Sleep(600 * time.Millisecond)
	if cols() != local {
		t.Fatalf("local policy: cols %d", cols())
	}
	// a bad value keeps the default and warns
	_, w := func() (any, []string) {
		cfg, ws := squatterConfigFor("squatter_naws", "sideways")
		return cfg, ws
	}()
	if len(w) != 1 || !strings.Contains(w[0], "squatter_naws") {
		t.Fatalf("%v", w)
	}
}

func squatterConfigFor(name, val string) (any, []string) {
	cfg, w := squatter.ConfigFromVarsMap(map[string]string{name: val})
	return cfg, w
}

// G1: what the squatter's own commands print never enters a scrollback a paired device can replay.
func TestSquatterCommandOutputIsNotReplayedToPeers(t *testing.T) {
	c, scr, d := squatClient(t)
	c.Do(func(in *gtfparser.Interp) { in.Exec("/addworld w god pw localhost 4000"); in.Exec("/connect w") })
	srv := <-d.srv
	srv.Expect([]byte("connect god pw\r\n"))
	srv.Send([]byte("Welcome\r\n"))
	waitScreen(t, c, scr, "Welcome")
	viewer, _ := pairPeer(t, c, "viewer", "eyes")
	adm, _ := pairPeer(t, c, "admin", "SquatClient on my host")
	dir := v(c, "squatter_dir")
	fp := v(c, "squatter_fingerprint")
	c.Do(func(in *gtfparser.Interp) {
		for _, l := range []string{"/squatter", "/squatter list", "/squatter who", "/squatter fingerprint", "/squatter kick nobody", "/squatter unpair nobody"} {
			in.ExecFull(l)
		}
	})
	waitScreen(t, c, scr, "Paired devices")
	if !strings.Contains(scr.Text(), fp) {
		t.Fatal("the local user must still see the output")
	}
	id := viewer.Sessions()[0].ID
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := viewer.Attach(ctx, id, nil, 0); err != nil {
		t.Fatal(err)
	}
	var all []string
	deadline := time.After(5 * time.Second)
loop:
	for {
		select {
		case ev := <-viewer.Events():
			all = append(all, peerLines([]wire.Event{ev})...)
			if b, ok := ev.(wire.Backlog); ok && b.Done {
				break loop
			}
		case <-deadline:
			t.Fatal("no backlog")
		}
	}
	text := strings.Join(all, "\n")
	for _, secret := range []string{"listening", fp, dir, "eyes", "SquatClient on my host", "Paired devices", "Connected clients", "squatter:"} {
		if strings.Contains(text, secret) {
			t.Fatalf("replay contains %q:\n%s", secret, text)
		}
	}
	if !strings.Contains(text, "Welcome") {
		t.Fatalf("replay lost the game text:\n%s", text)
	}
	// and an admin's tf.exec still gets the output back (private, captured)
	r, err := adm.Exec(ctx, "/squatter list")
	if err != nil || !strings.Contains(strings.Join(r.Out, "\n"), "Paired devices") {
		t.Fatalf("%+v %v", r, err)
	}
}

// G3: kick and unpair take the whole rest of the line (device names have spaces).
func TestSquatterUnpairAndKickByNameWithSpaces(t *testing.T) {
	c, scr, _ := squatClient(t)
	_, _ = pairPeer(t, c, "operator", "SquatClient on my host")
	_, _ = pairPeer(t, c, "operator", "SquatClient on other")
	c.Do(func(in *gtfparser.Interp) { in.ExecFull("/squatter kick SquatClient on") })
	c.Flush()
	c.Do(func(in *gtfparser.Interp) { in.ExecFull("/squatter kick SquatClient on my host") })
	waitScreen(t, c, scr, "1 client(s) disconnected")
	c.Do(func(in *gtfparser.Interp) { in.ExecFull("/squatter unpair SquatClient on my host") })
	waitScreen(t, c, scr, "unpaired SquatClient on my host")
	var n int
	c.Do(func(*gtfparser.Interp) { d, _ := c.sq.sq.Devices(); n = len(d) })
	if n != 1 {
		t.Fatalf("%d devices left", n)
	}
}

// G2 and G5: an ad hoc connect keeps the peer's name, and tf.fg tells the foreground world.
func TestAdhocConnectKeepsNameAndFgFlag(t *testing.T) {
	c, scr, d := squatClient(t)
	c.Do(func(in *gtfparser.Interp) { in.Exec("/addworld w god pw localhost 4000"); in.Exec("/connect w") })
	srv := <-d.srv
	srv.Expect([]byte("connect god pw\r\n"))
	srv.Send([]byte("hi\r\n"))
	waitScreen(t, c, scr, "hi")
	pc, _ := pairPeer(t, c, "operator", "phone")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	eventually(t, "fg flag", func() bool {
		l, err := pc.List(ctx)
		return err == nil && len(l) == 1 && l[0].TF != nil && l[0].TF.Fg
	})
	info, err := pc.Connect(ctx, proto.World{Name: "adhoc", Host: "localhost", Port: 4001})
	if err != nil || info.Name != "adhoc" || info.TF == nil || info.TF.Fg {
		t.Fatalf("%+v %v", info, err)
	}
	<-d.srv
	info2, err := pc.Connect(ctx, proto.World{Name: "adhoc", Host: "localhost", Port: 4002})
	if err != nil || info2.Name != "adhoc-2" {
		t.Fatalf("%+v %v", info2, err)
	}
	<-d.srv
	// the peer's connect never moved gtf's foreground world
	l, _ := pc.List(ctx)
	for _, s := range l {
		if s.Name == "w" && !s.TF.Fg {
			t.Fatal("the foreground world changed")
		}
	}
	// without a usable name the old behaviour stays
	info3, err := pc.Connect(ctx, proto.World{Host: "localhost", Port: 4003})
	if err != nil || !strings.HasPrefix(info3.Name, "(unnamed") {
		t.Fatalf("%+v %v", info3, err)
	}
}

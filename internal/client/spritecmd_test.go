// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package client

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/itsnotyoutoday/gtf/internal/layout"
	"github.com/itsnotyoutoday/gtf/internal/net/nettest"
	"github.com/itsnotyoutoday/gtf/internal/ui/vt"
	"github.com/itsnotyoutoday/gtfparser"
)

// teeTerm is a vt screen that also records the bytes written to it, so a test
// can hand the real terminal stream to cmd/vtdump for a picture.
type teeTerm struct {
	*vt.Screen
	rec bytes.Buffer
}

func (t *teeTerm) Write(p []byte) (int, error) {
	t.rec.Write(p)
	return t.Screen.Write(p)
}

const spriteDir = "testdata/sprites/"

func spriteClient(t *testing.T) (*Client, *teeTerm) {
	t.Helper()
	tt := &teeTerm{Screen: vt.New(100, 40)}
	c, err := New(Config{Term: tt, HomeDir: t.TempDir(), NoRC: true, Quiet: true, Dialer: &fakeDialer{t: t, srv: make(chan *nettest.Server, 1)}, Interactive: true, NoUpdateCheck: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	c.ui.Depth = layout.DepthTrue // what a COLORTERM=truecolor terminal gets
	return c, tt
}

// attrs of the screen cell at x, y as the vt model parsed them from the real byte stream.
func cellAt(tt *teeTerm, x, y int) vt.Cell { return tt.Screen.CellAt(x, y) }

func TestSpriteCommandPrintsHalfBlocks(t *testing.T) {
	c, tt := spriteClient(t)
	c.Do(func(in *gtfparser.Interp) {
		in.Exec("/sprite " + spriteDir + "warbird.json 20 10 -b #0e1018")
	})
	deadline := 0
	for ; deadline < 100; deadline++ {
		c.Flush()
		if strings.ContainsAny(tt.Screen.Text(), "▀▄") {
			break
		}
	}
	txt := tt.Screen.Text()
	if !strings.ContainsAny(txt, "▀▄") {
		t.Fatalf("no half blocks on the screen:\n%s", tt.Screen.Dump())
	}
	// find a drawn cell and check it carries 24-bit colour from the stream
	found := false
	for y := 0; y < tt.Screen.H && !found; y++ {
		for x := 0; x < tt.Screen.W; x++ {
			ce := cellAt(tt, x, y)
			if ce.R == '▀' && strings.Contains(ce.Attr, "fg=") && strings.Contains(ce.Attr, "/") {
				found = true
				break
			}
		}
	}
	if !found {
		t.Errorf("no cell with 24-bit colour:\n%s", tt.Screen.Dump())
	}
	if p := os.Getenv("SPRITE_STREAM"); p != "" {
		os.WriteFile(p, tt.rec.Bytes(), 0o644)
	}
}

func TestSpriteCommandErrors(t *testing.T) {
	c, tt := spriteClient(t)
	bad := filepath.Join(t.TempDir(), "bad.json")
	os.WriteFile(bad, []byte(`{"layers":[{"shapes":[{"type":"banana"}]}]}`), 0o644)
	c.Do(func(in *gtfparser.Interp) {
		in.Exec("/sprite")
		in.Exec("/sprite /no/such/file.json")
		in.Exec("/sprite " + bad)
		in.Exec("/sprite " + spriteDir + "saucer.json 99999 99999")
		in.Exec("/sprite " + spriteDir + "saucer.json 10 5 -m wobbly")
	})
	c.Flush()
	out := tt.Screen.Text()
	for _, want := range []string{"usage: /sprite", "no such file", "unknown shape type", "limit exceeded", "-m wants"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, tt.Screen.Dump())
		}
	}
}

func TestSpritePanelAndCanvas(t *testing.T) {
	c, tt := spriteClient(t)
	c.Do(func(in *gtfparser.Interp) {
		in.Exec("/layout new sp")
		in.Exec("/panel -dock right -w 26 -border single -title Ship ship = {sprite saucer}")
		in.Exec("/sprite " + spriteDir + "saucer.json 24 8 -m quad -b #000000 -z hull=0.5 -g beacon=0 -p saucer")
		in.Exec("/canvas new pic 30 10 #101018")
		in.Exec("/canvas sprite pic " + spriteDir + "planet.json 2 2 40 40 zones=land=0.5")
		in.Exec("/panel -dock bottom -h 10 view = {canvas pic}")
	})
	c.Flush()
	c.Do(func(*gtfparser.Interp) {})
	c.Flush()
	txt := tt.Screen.Text()
	if !strings.ContainsAny(txt, "▘▝▖▗▌▐▀▄▚▞▛▜▙▟") {
		t.Errorf("sprite panel shows no block glyphs:\n%s", tt.Screen.Dump())
	}
	if strings.Contains(txt, "no such canvas") || strings.Contains(txt, "sprite:") {
		t.Errorf("errors on screen:\n%s", tt.Screen.Dump())
	}
	if _, ok := c.panels.canvases["pic"]; !ok {
		t.Fatal("canvas missing")
	}
	// the planet reached the canvas pixels
	cv := c.panels.canvases["pic"]
	lit := 0
	for x := 0; x < cv.W; x++ {
		for y := 0; y < cv.H; y++ {
			if col, _ := cv.PixelAt(x, y); col.R != 16 || col.G != 16 || col.B != 24 {
				lit++
			}
		}
	}
	if lit < 300 {
		t.Errorf("only %d canvas pixels changed", lit)
	}
	if p := os.Getenv("SPRITE_STREAM2"); p != "" {
		os.WriteFile(p, tt.rec.Bytes(), 0o644)
	}
	c.Do(func(in *gtfparser.Interp) { in.Exec("/sprite -d saucer") })
	if _, ok := c.panels.sprites["saucer"]; ok {
		t.Error("/sprite -d did not forget it")
	}
}

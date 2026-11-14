package client

// glue.go gathers the pieces that exist only until the engine's triggers
// rewrite lands (see docs/ARCHITECTURE.md "merge checklist"): our own parsing
// of tf attribute strings (attrs.go) and the hand-made LOGIN (session.go).

import (
	"os"
	"path/filepath"

	"github.com/itsnotyoutoday/gtf/gtflib"
)

var cachedGtfLib string

// gtfLibDir is the directory the gtf-only library was extracted to.
func gtfLibDir() string {
	if cachedGtfLib == "" {
		d, err := gtflib.CacheDir()
		if err != nil {
			return ""
		}
		cachedGtfLib = d
	}
	return cachedGtfLib
}

// loadGtfLib loads the gtf-only scripts layered on top of the tf library.
func (c *Client) loadGtfLib() {
	d := gtfLibDir()
	if d == "" {
		return
	}
	c.in.SetVar("GTFLIBDIR", d)
	for _, f := range []string{"layout.tf"} {
		p := filepath.Join(d, "gtf", f)
		if _, err := os.Stat(p); err == nil {
			if err := c.in.LoadFile(p); err != nil {
				c.echoLine("% " + err.Error())
			}
		}
	}
}

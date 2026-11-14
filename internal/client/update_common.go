// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package client

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/itsnotyoutoday/gtf/internal/update"
)

// restartHook is set by the in-place restart support; nil means this build cannot restart in place.
var restartHook func(c *Client, force bool) error

// dataDir is ~/.gtf, where settings and update state live.
func dataDir(home string) string { return filepath.Join(home, ".gtf") }

func exePath() string {
	p, err := os.Executable()
	if err != nil {
		p = os.Args[0]
	}
	return strings.TrimSuffix(p, " (deleted)")
}

// installedLib is the library directory an installer put next to this binary, but only when it
// belongs to this exact version (otherwise the library embedded in the binary is used).
func installedLib(home string) (dir string, rec update.Record, ok bool) {
	d := update.FindLibDir(exePath(), home, os.Getenv)
	if d == "" {
		return "", rec, false
	}
	rec, err := update.LoadRecord(d)
	if err != nil {
		return "", rec, false
	}
	return d, rec, true
}

// updateArgs are the parsed options of /update and gtf --update.
type updateArgs struct {
	install, restart bool
	channel          string // "" = use the setting
	check            string // "", "on", "off"
	bad              string
}

func parseUpdateArgs(words []string) updateArgs {
	var a updateArgs
	for _, w := range words {
		w = strings.TrimLeft(w, "-")
		switch {
		case w == "y" || w == "yes" || w == "install":
			a.install = true
		case w == "r" || w == "restart":
			a.restart, a.install = true, true
		case strings.HasPrefix(w, "channel="), strings.HasPrefix(w, "c="):
			a.channel = w[strings.IndexByte(w, '=')+1:]
		case strings.HasPrefix(w, "check="):
			a.check = strings.ToLower(w[strings.IndexByte(w, '=')+1:])
		case w == "":
		default:
			a.bad = w
		}
	}
	return a
}

// applySettings stores -channel= and -check= and returns the settings to use.
func applySettings(home string, a updateArgs, say func(string)) update.Settings {
	path := update.ConfigPath(dataDir(home))
	s, err := update.Load(path)
	if err != nil {
		say("% " + err.Error())
	}
	changed := false
	if a.channel != "" {
		if !update.ValidChannel(a.channel) {
			say(fmt.Sprintf("%% update: unknown channel %q (use %q or %q)", a.channel, update.ChannelStable, update.ChannelPrerelease))
		} else {
			s.UpdateChannel, changed = a.channel, true
		}
	}
	switch a.check {
	case "on":
		s.UpdateCheck, changed = true, true
	case "off":
		s.UpdateCheck, changed = false, true
	}
	if changed {
		if err := s.Save(path); err != nil {
			say("% update: cannot save settings: " + err.Error())
		} else {
			say(fmt.Sprintf("%% update: following the %s channel; daily check %s.", s.UpdateChannel, map[bool]string{true: "on", false: "off"}[s.UpdateCheck]))
		}
	}
	return s
}

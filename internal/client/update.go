// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package client

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/itsnotyoutoday/gtf/internal/update"
	"github.com/itsnotyoutoday/gtfparser"
)

// /update checks the GitHub releases for a newer gtf and, with -y, installs it (binary and library
// directory together, checksum-verified; see docs/INSTALL.md).
//
//	/update              say whether a newer version exists
//	/update -y           install it (the running gtf keeps going until it is restarted)
//	/update -y -r        install it and restart in place, keeping connections open (when supported)
//	/update -channel=stable|prerelease      which releases to follow
//	/update -check=on|off                   the once-a-day check at startup
//
// "gtf --update [-y]" does the same from a shell, without starting the client (use it with sudo for
// a system-wide install).

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

// newUpdater builds the updater for the running binary.
func newUpdater(home string) *update.Updater {
	u := &update.Updater{
		Client:  &update.Client{HTTP: &http.Client{Timeout: 60 * time.Second}, API: os.Getenv("GTF_UPDATE_API"), Repo: os.Getenv("GTF_UPDATE_REPO")},
		Version: Version,
		ExePath: exePath(),
		GOOS:    runtime.GOOS,
		GOARCH:  runtime.GOARCH,
	}
	if d, rec, ok := installedLib(home); ok {
		u.LibDir, u.Rec = d, rec
	}
	return u
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

func (c *Client) cmdUpdate(in *gtfparser.Interp, args string) gtfparser.Value {
	a := parseUpdateArgs(strings.Fields(args))
	if a.bad != "" {
		in.Warn("update: unknown option -" + a.bad + " (use -y, -r, -channel=stable|prerelease, -check=on|off)")
		return gtfparser.Int(0)
	}
	home := c.cfg.HomeDir
	s := applySettings(home, a, c.echoLine)
	u := newUpdater(home)
	if u.Version == "" {
		in.Warn("update: unknown running version")
		return gtfparser.Int(0)
	}
	c.echoLine("% Checking for updates ...")
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		var out strings.Builder
		res, err := u.Run(ctx, s.UpdateChannel, a.install, &out)
		c.Post(func(*gtfparser.Interp) {
			for _, ln := range strings.Split(strings.TrimRight(out.String(), "\n"), "\n") {
				if ln != "" {
					c.echoLine("% " + ln)
				}
			}
			if err != nil {
				c.echoLine("% update: " + err.Error())
				if update.IsPermission(err) || strings.Contains(err.Error(), "permission denied") {
					c.echoLine("% " + update.PermissionHint(u.ExePath))
				}
				c.afterTask()
				return
			}
			if res.Applied {
				switch {
				case a.restart && restartHook != nil:
					c.echoLine("% Restarting into gtf " + res.Latest + " (connections stay open) ...")
					if rerr := restartHook(c, false); rerr != nil {
						c.echoLine("% restart: " + rerr.Error())
					}
				case a.restart:
					c.echoLine("% This build cannot restart in place; quit gtf and start it again to use " + res.Latest + ".")
				case restartHook != nil:
					c.echoLine("% Type /restart to switch to " + res.Latest + " without dropping connections.")
				default:
					c.echoLine("% Quit gtf and start it again to use " + res.Latest + ".")
				}
			}
			c.afterTask()
		})
	}()
	return gtfparser.Int(1)
}

// startUpdateCheck says at startup when a newer version is known, and once a day asks GitHub.
// It never blocks the client and stays silent when offline.
func (c *Client) startUpdateCheck() {
	if !c.cfg.Interactive || os.Getenv("GTF_NO_UPDATE_CHECK") != "" {
		return
	}
	home := c.cfg.HomeDir
	if _, err := update.ParseVersion(Version); err != nil {
		return
	}
	s, _ := update.Load(update.ConfigPath(dataDir(home)))
	if !s.UpdateCheck {
		return
	}
	statePath := update.StatePath(dataDir(home))
	st := update.LoadState(statePath)
	notice := func(v string) {
		c.echoLine(fmt.Sprintf("%% gtf %s is available (you have %s). Type /update for details, /update -y to install it.", v, Version))
	}
	if v := st.AvailableFor(Version); v != "" {
		notice(v)
	}
	if !st.Due(time.Now()) {
		return
	}
	u := newUpdater(home)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		res, _, err := u.Check(ctx, s.UpdateChannel)
		next := update.State{LastCheck: time.Now().UnixMilli()}
		if err == nil && res.Newer {
			next.Available = res.Latest
		}
		_ = next.Save(statePath)
		if err == nil && res.Newer && st.Available != res.Latest {
			c.Post(func(*gtfparser.Interp) { notice(res.Latest); c.afterTask() })
		}
	}()
}

// RunUpdateCLI implements "gtf --update [-y] [-channel=...]": it needs no terminal.
func RunUpdateCLI(args []string) int {
	home, _ := os.UserHomeDir()
	if h := os.Getenv("HOME"); h != "" {
		home = h
	}
	a := parseUpdateArgs(args)
	if a.bad != "" {
		fmt.Fprintln(os.Stderr, "gtf --update: unknown option -"+a.bad)
		return 2
	}
	s := applySettings(home, a, func(t string) { fmt.Println(strings.TrimPrefix(t, "% ")) })
	u := newUpdater(home)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	res, err := u.Run(ctx, s.UpdateChannel, a.install, os.Stdout)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gtf --update:", err)
		if update.IsPermission(err) || strings.Contains(err.Error(), "permission denied") {
			fmt.Fprintln(os.Stderr, update.PermissionHint(u.ExePath))
		}
		return 1
	}
	if res.Applied {
		fmt.Println("Running gtf sessions keep the old version until restarted (/restart keeps connections open).")
	}
	return 0
}

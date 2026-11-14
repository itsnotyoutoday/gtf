//go:build !pkgbuild

// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package client

import (
	"context"
	"fmt"
	"net/http"
	"os"
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

// newUpdater builds the updater for the running binary.
func newUpdater(home string) *update.Updater {
	u := &update.Updater{
		Client:  &update.Client{HTTP: &http.Client{Timeout: 60 * time.Second}, API: os.Getenv("GTF_UPDATE_API"), Web: os.Getenv("GTF_UPDATE_WEB"), Repo: os.Getenv("GTF_UPDATE_REPO")},
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
	if !c.cfg.Interactive || c.cfg.NoUpdateCheck || os.Getenv("GTF_NO_UPDATE_CHECK") != "" {
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
	managed := newUpdater(home).Managed()
	notice := func(v string) {
		if managed {
			c.echoLine(fmt.Sprintf("%% gtf %s is available (you have %s). Ask your system administrator to upgrade the gtf package.", v, Version))
			return
		}
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

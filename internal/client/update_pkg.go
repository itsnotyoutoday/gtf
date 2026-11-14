//go:build pkgbuild

// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package client

import (
	"fmt"
	"os"
	"strings"

	"github.com/itsnotyoutoday/gtf/internal/update"
	"github.com/itsnotyoutoday/gtfparser"
)

// The distribution package build (deb/rpm, built with -tags pkgbuild). The package manager owns the
// files, so this gtf contains no code to fetch or install anything from GitHub. /update only compares the
// gtf that is running with the one the package manager has installed on this machine:
//
//	/update       says whether a newer gtf is installed (the administrator upgraded the package while
//	              this gtf was running) or that this is the newest one on the system
//	/update -r    restarts into the installed binary without dropping connections (when supported)
//
// Newer releases than what is installed are for the system administrator to install.

const adminAdvice = "Newer releases are installed by your system administrator with the package manager " +
	"(for example: sudo apt install --only-upgrade gtf, or sudo dnf upgrade gtf)."

// installedVersion is the gtf version the package manager has put on disk, "" when unknown.
func installedVersion(home string) string {
	d := update.FindLibDir(exePath(), home, os.Getenv)
	if d == "" {
		return ""
	}
	rec, err := update.LoadRecord(d)
	if err != nil {
		return ""
	}
	return rec.Version
}

// newerInstalled reports whether the installed version is newer than the running one.
func newerInstalled(home string) (string, bool) {
	v := installedVersion(home)
	if v == "" {
		return "", false
	}
	return v, update.Newer(Version, v)
}

func (c *Client) cmdUpdate(in *gtfparser.Interp, args string) gtfparser.Value {
	a := parseUpdateArgs(strings.Fields(args))
	if a.bad != "" {
		in.Warn("update: unknown option -" + a.bad + " (use -r, -check=on|off)")
		return gtfparser.Int(0)
	}
	home := c.cfg.HomeDir
	applySettings(home, a, c.echoLine)
	if a.install && !a.restart {
		c.echoLine("% This gtf was installed by the system package manager and cannot install updates itself.")
		c.echoLine("% " + adminAdvice)
		return gtfparser.Int(0)
	}
	v, newer := newerInstalled(home)
	switch {
	case newer && a.restart:
		if restartHook == nil {
			c.echoLine("% gtf " + v + " is installed; this build cannot restart in place. Quit gtf and start it again.")
			return gtfparser.Int(1)
		}
		c.echoLine("% Restarting into gtf " + v + " (connections stay open) ...")
		if err := restartHook(c, false); err != nil {
			c.echoLine("% restart: " + err.Error())
			return gtfparser.Int(0)
		}
	case newer:
		c.echoLine(fmt.Sprintf("%% gtf %s is installed on this system (you are running %s).", v, Version))
		if restartHook != nil {
			c.echoLine("% Type /update -r to switch to it without dropping your connections.")
		} else {
			c.echoLine("% Quit gtf and start it again to use it.")
		}
	case a.restart:
		c.echoLine("% You are already running the newest gtf installed on this system (" + Version + ").")
	default:
		c.echoLine("% gtf " + Version + " is the newest version installed on this system.")
		c.echoLine("% " + adminAdvice)
	}
	return gtfparser.Int(1)
}

// startUpdateCheck says at startup when a newer gtf than the running one is installed. No network.
func (c *Client) startUpdateCheck() {
	if !c.cfg.Interactive || c.cfg.NoUpdateCheck || os.Getenv("GTF_NO_UPDATE_CHECK") != "" {
		return
	}
	s, _ := update.Load(update.ConfigPath(dataDir(c.cfg.HomeDir)))
	if !s.UpdateCheck {
		return
	}
	if v, newer := newerInstalled(c.cfg.HomeDir); newer {
		c.echoLine(fmt.Sprintf("%% gtf %s is installed on this system (you are running %s). Type /update -r to switch to it, keeping your connections.", v, Version))
	}
}

// RunUpdateCLI implements "gtf --update": it reports what is installed; it never changes anything.
func RunUpdateCLI(args []string) int {
	home, _ := os.UserHomeDir()
	if h := os.Getenv("HOME"); h != "" {
		home = h
	}
	if a := parseUpdateArgs(args); a.bad != "" {
		fmt.Fprintln(os.Stderr, "gtf --update: unknown option -"+a.bad)
		return 2
	}
	if v, newer := newerInstalled(home); newer {
		fmt.Printf("gtf %s is installed on this system; this program is %s.\n", v, Version)
		return 0
	}
	fmt.Printf("gtf %s is the newest version installed on this system.\n%s\n", Version, adminAdvice)
	return 0
}

// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package client

import (
	"fmt"
	"os"
	"strings"

	"github.com/itsnotyoutoday/gtf/internal/ui"
)

const usage = `usage: gtf [-L<dir>] [-f[file]] [-c<cmd>] [-nlq] [-v] [<world> | <host> <port>]
  -L<dir>   use <dir> as the tf library directory (default: the embedded copy)
  -f        do not load the personal config file (.tfrc)
  -c<cmd>   execute <cmd> at startup (repeatable)
  -n        no automatic connection
  -l        no automatic login
  -q        quiet login
  -v        show the version
       gtf --update [-y] [-channel=stable|prerelease]   check for (or with -y install) a newer gtf`

// Main is the program entry point (cmd/gtf is only a call to it). It returns
// the process exit status.
func Main(args []string) int {
	cfg := Config{Interactive: true}
	var rest []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-v" || a == "--version":
			fmt.Println(VersionLine())
			if ev := EngineVersion(); ev != "" {
				fmt.Println("engine: gtfparser " + ev + ", TinyFugue " + TFVersion + " compatible")
			}
			return 0
		case a == "--update":
			return RunUpdateCLI(args[i+1:])
		case a == "-h" || a == "--help" || a == "-?":
			fmt.Println(usage)
			return 0
		case strings.HasPrefix(a, "-L"):
			cfg.LibDir = strings.TrimPrefix(a, "-L")
			if cfg.LibDir == "" && i+1 < len(args) {
				i++
				cfg.LibDir = args[i]
			}
		case strings.HasPrefix(a, "-c"):
			v := strings.TrimPrefix(a, "-c")
			if v == "" && i+1 < len(args) {
				i++
				v = args[i]
			}
			cfg.Commands = append(cfg.Commands, v)
		case strings.HasPrefix(a, "-f"):
			cfg.NoRC = true
		case strings.HasPrefix(a, "-") && len(a) > 1 && strings.Trim(a[1:], "nlq") == "":
			for _, ch := range a[1:] {
				switch ch {
				case 'n':
					cfg.NoAuto = true
				case 'l':
					cfg.NoLogin = true
				case 'q':
					cfg.Quiet = true
				}
			}
		case strings.HasPrefix(a, "-"):
			fmt.Fprintln(os.Stderr, "gtf: unknown option "+a)
			fmt.Fprintln(os.Stderr, usage)
			return 2
		default:
			rest = append(rest, a)
		}
	}
	cfg.World = strings.Join(rest, " ")
	cfg.Args = os.Args
	spec, err := RestoreSpecFromEnv(os.Environ())
	if err != nil {
		fmt.Fprintln(os.Stderr, "gtf:", err)
		return 1
	}
	if spec != nil && spec.Probe {
		return ProbeMain(spec, cfg) // a rehearsal child of /restart: no terminal
	}
	var restoration *Restoration
	if spec != nil {
		// started by /restart: the connections are in inherited descriptors
		restoration, err = LoadRestoration(*spec)
		if err != nil {
			fmt.Fprintln(os.Stderr, "gtf: /restart state unusable, starting fresh (the connections are lost):", err)
			closeInherited(spec.FDs)
			restoration = nil
		}
	}
	if !ui.IsTerminal() {
		fmt.Fprintln(os.Stderr, "gtf: standard input and output must be a terminal")
		return 1
	}
	var tty *ui.TTY
	if restoration != nil && len(restoration.Termios()) > 0 {
		tty, err = ui.OpenTTYFrom(restoration.Termios())
	} else {
		tty, err = ui.OpenTTY()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "gtf:", err)
		return 1
	}
	defer tty.Close()
	cfg.Term = tty
	cfg.Keys = tty.Input()
	cfg.Resized = tty.Resized
	cfg.Suspend = tty.Suspend
	cfg.Restore = restoration
	c, err := New(cfg)
	if err != nil && restoration != nil {
		// the state could not be applied: say so and start fresh rather than leave the user without a client
		fmt.Fprintln(os.Stderr, "gtf: /restart state unusable, starting fresh (the connections are lost):", err)
		restoration.CloseFiles()
		cfg.Restore = nil
		c, err = New(cfg)
	}
	if err != nil {
		tty.Close()
		fmt.Fprintln(os.Stderr, "gtf:", err)
		return 1
	}
	c.Start()
	<-c.Done()
	// leave the cursor below the screen contents
	fmt.Fprint(os.Stdout, "\x1b[0m\x1b[?25h\r\n")
	c.Close()
	return 0
}

// closeInherited closes descriptors a failed /restart left to this process.
func closeInherited(fds []int) {
	for _, n := range fds {
		os.NewFile(uintptr(n), "inherited").Close()
	}
}

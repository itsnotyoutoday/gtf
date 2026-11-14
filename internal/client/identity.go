// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package client

import (
	"runtime/debug"
)

// Identity: the single source of truth for the program's name, version and
// notices. Everything user-visible (banner, -v, the version variable,
// /help copyright, telnet client version) is derived from these.
const (
	Name      = "GoTinyFugue"
	ShortName = "gtf"
	// TFVersion is the TinyFugue release gtf is compatible with.
	TFVersion = "5.0 beta 8"

	Holder    = "Rick Bird"
	Year      = "2026"
	License   = "GPL-3.0-or-later"
	ClientURL = "https://github.com/itsnotyoutoday/gtf"
	EngineURL = "https://github.com/itsnotyoutoday/gtfparser"
)

// Version is the release string. Release builds set it with
// -ldflags "-X github.com/itsnotyoutoday/gtf/internal/client.Version=0.0.2"; a build without
// that is the development version below.
var Version = "0.0.1"

// VersionLine is "GoTinyFugue version 0.0.1 (gtf)".
func VersionLine() string {
	return Name + " version " + Version + " (" + ShortName + ")"
}

// VersionVar is the value of the tf `version' variable (what /version
// prints): it names us and states the tf release we are compatible with.
func VersionVar() string {
	return VersionLine() + ", compatible with TinyFugue " + TFVersion
}

// CopyrightLine is the second banner line.
func CopyrightLine() string {
	return "Copyright (C) " + Year + " " + Holder + ". Inspired by and compatible with TinyFugue " +
		TFVersion + ", Copyright (C) 1993-2007 Ken Keys."
}

// EngineVersion reports the linked gtfparser module version, if known.
func EngineVersion() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	for _, d := range bi.Deps {
		if d.Path == "github.com/itsnotyoutoday/gtfparser" {
			if d.Replace != nil && d.Replace.Version == "" {
				return d.Version + " (local)"
			}
			return d.Version
		}
	}
	return ""
}

// ownNotice is shown first by /help copyright; the original TinyFugue
// notice follows it unchanged.
var ownNotice = []string{
	Name + " (" + ShortName + ") " + Version,
	"Copyright (C) " + Year + " " + Holder,
	"",
	"This program is free software: you can redistribute it and/or modify it",
	"under the terms of the GNU General Public License as published by the Free",
	"Software Foundation, either version 3 of the License, or (at your option)",
	"any later version.",
	"",
	"This program is distributed in the hope that it will be useful, but",
	"WITHOUT ANY WARRANTY; without even the implied warranty of MERCHANTABILITY",
	"or FITNESS FOR A PARTICULAR PURPOSE.  See the GNU General Public License",
	"for more details.",
	"",
	"  " + ClientURL,
	"  " + EngineURL,
	"",
	Name + " is inspired by and compatible with TinyFugue by Ken Keys, whose",
	"standard library it embeds unmodified.  The original TinyFugue copyright",
	"and license notice follows.",
	"",
}

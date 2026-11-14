// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

// Package gtf holds one thing: the program's version, read from the VERSION file at compile time.
// VERSION is the single place the number lives: the client (/version, gtf -v, the telnet client
// version, /update), the build scripts, the packages and the release workflow all use it, and a
// release tag must match it.
package gtf

import (
	_ "embed"
	"strings"
)

//go:embed VERSION
var versionFile string

// Version is the contents of the VERSION file, e.g. "0.0.2-alpha3" (no leading "v").
func Version() string { return strings.TrimSpace(versionFile) }

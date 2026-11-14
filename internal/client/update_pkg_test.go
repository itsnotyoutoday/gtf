//go:build pkgbuild

// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package client

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/itsnotyoutoday/gtf/internal/update"
)

// In the distribution build /update compares with what the package manager installed on disk.
func TestPkgBuildReportsNewerInstalledVersion(t *testing.T) {
	old := Version
	Version = "0.0.2"
	defer func() { Version = old }()
	lib := t.TempDir()
	if err := os.MkdirAll(filepath.Join(lib, "tf"), 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(lib, "tf", "stdlib.tf"), nil, 0o644)
	t.Setenv("GTF_LIBDIR", lib)
	write := func(v string) {
		if err := (update.Record{Scope: "package", Version: v}).Save(lib); err != nil {
			t.Fatal(err)
		}
	}
	write("0.0.2")
	if v, newer := newerInstalled(t.TempDir()); newer {
		t.Fatalf("same version reported as newer: %q", v)
	}
	write("0.0.3")
	if v, newer := newerInstalled(t.TempDir()); !newer || v != "0.0.3" {
		t.Fatalf("a package upgrade under a running gtf was not noticed: %q %v", v, newer)
	}
	if !strings.Contains(adminAdvice, "system administrator") {
		t.Fatal("advice")
	}
}

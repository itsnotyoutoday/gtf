// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package update

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// RecordFile is written by install.sh (and kept by /update) in the library directory.
const RecordFile = "install.json"

// Record says how this copy of gtf was installed.
type Record struct {
	Scope   string `json:"scope"`   // "user", "global", or "package" (installed by a deb/rpm)
	BinDir  string `json:"bindir"`  // where the gtf binary lives
	LibDir  string `json:"libdir"`  // the library directory (contains tf/, gtf/, examples/)
	Version string `json:"version"` // the version installed
	Repo    string `json:"repo,omitempty"`
	// ManagedBy names the package manager that owns this copy: "package" (deb/rpm) or "brew". Such a
	// copy is never replaced by /update: only that package manager may change it.
	ManagedBy string `json:"managed_by,omitempty"`
}

// IsManaged reports whether the system package manager owns this installation.
func (r Record) IsManaged() bool { return r.Scope == "package" || r.ManagedBy != "" }

// IsBrewPath reports whether exe lives in a Homebrew Cellar (the resolved path of a brew-installed gtf).
func IsBrewPath(exe string) bool {
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	return strings.Contains(filepath.ToSlash(exe), "/Cellar/")
}

// IsSystemPath reports whether exe lives where a distribution's packages put programs. A binary
// there is treated as package-managed even without an install record, so /update never overwrites it.
func IsSystemPath(exe string) bool {
	switch filepath.Dir(exe) {
	case "/usr/bin", "/bin", "/usr/sbin", "/sbin":
		return true
	}
	return false
}

// LoadRecord reads <libdir>/install.json.
func LoadRecord(libDir string) (Record, error) {
	var r Record
	b, err := os.ReadFile(filepath.Join(libDir, RecordFile))
	if err != nil {
		return r, err
	}
	return r, json.Unmarshal(b, &r)
}

// Save writes the record into libDir.
func (r Record) Save(libDir string) error {
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(libDir, RecordFile), append(b, '\n'), 0o644)
}

// validLib reports whether dir looks like a gtf library directory.
func validLib(dir string) bool {
	st, err := os.Stat(filepath.Join(dir, "tf", "stdlib.tf"))
	return err == nil && !st.IsDir()
}

// FindLibDir finds the installed library directory, in this order: $GTF_LIBDIR; ../lib/gtf next to
// the binary (so a relocated install keeps working); ~/lib/gtf; /usr/local/lib/gtf; /usr/lib/gtf.
// It returns "" when none exists (gtf then uses the copy embedded in the binary).
func FindLibDir(exe, home string, getenv func(string) string) string {
	var cands []string
	if d := getenv("GTF_LIBDIR"); d != "" {
		cands = append(cands, d)
	}
	if exe != "" {
		if real, err := filepath.EvalSymlinks(exe); err == nil {
			exe = real
		}
		cands = append(cands, filepath.Join(filepath.Dir(exe), "..", "lib", "gtf"))
	}
	if home != "" {
		cands = append(cands, filepath.Join(home, "lib", "gtf"))
	}
	cands = append(cands, "/usr/local/lib/gtf", "/usr/lib/gtf")
	for _, c := range cands {
		c = filepath.Clean(c)
		if validLib(c) {
			return c
		}
	}
	return ""
}

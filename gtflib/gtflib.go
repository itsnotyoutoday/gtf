// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

// Package gtflib embeds the gtf-only TF library: scripts that add commands
// and macros on top of the unmodified real tf library (which the engine's
// tflib package ships). Nothing here is part of the core TF language.
package gtflib

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
)

//go:embed gtf
var embedded embed.FS

// FS is the embedded tree; paths look like "gtf/layout.tf".
var FS fs.FS = embedded

// Version is a short content hash.
func Version() string {
	h := sha256.New()
	fs.WalkDir(embedded, ".", func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			b, _ := fs.ReadFile(embedded, p)
			h.Write([]byte(p))
			h.Write(b)
		}
		return nil
	})
	return hex.EncodeToString(h.Sum(nil))[:12]
}

// Extract writes the embedded tree under dir (so the interpreter, which
// loads from disk, can /load it) and returns dir.
func Extract(dir string) (string, error) {
	err := fs.WalkDir(embedded, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		dst := filepath.Join(dir, filepath.FromSlash(p))
		if d.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		b, err := fs.ReadFile(embedded, p)
		if err != nil {
			return err
		}
		if old, err := os.ReadFile(dst); err == nil && string(old) == string(b) {
			return nil
		}
		return os.WriteFile(dst, b, 0o644)
	})
	return dir, err
}

// CacheDir extracts to the user cache directory and returns it.
func CacheDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		base = os.TempDir()
	}
	return Extract(filepath.Join(base, "gtf", "gtflib-"+Version()))
}

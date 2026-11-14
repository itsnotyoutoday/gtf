// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

// Package gtflib embeds the TinyFugue library that gtf ships: an unmodified
// copy of the real tf library (tf/, see TF-NOTICE.md) and the gtf-only scripts
// (gtf/) that add commands and macros on top of it. Nothing here is part of the
// core TF language, and the scripting engine itself does not contain either.
package gtflib

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
)

//go:embed gtf
var embedded embed.FS

//go:embed tf
var embeddedTF embed.FS

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

// TF is the unmodified real tf library (stdlib.tf, ...), rooted at the library directory.
var TF fs.FS = func() fs.FS { s, _ := fs.Sub(embeddedTF, "tf"); return s }()

// TFVersion is a short content hash of the embedded tf library.
func TFVersion() string {
	h := sha256.New()
	names, _ := fs.Glob(TF, "*")
	sort.Strings(names)
	for _, n := range names {
		b, _ := fs.ReadFile(TF, n)
		h.Write([]byte(n))
		h.Write(b)
	}
	return hex.EncodeToString(h.Sum(nil))[:12]
}

// ExtractTF writes the real tf library into dir (the interpreter loads library
// files from disk) and returns dir.
func ExtractTF(dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	names, err := fs.Glob(TF, "*")
	if err != nil {
		return "", err
	}
	for _, n := range names {
		b, err := fs.ReadFile(TF, n)
		if err != nil {
			return "", err
		}
		p := filepath.Join(dir, path.Base(n))
		if old, err := os.ReadFile(p); err == nil && string(old) == string(b) {
			continue
		}
		if err := os.WriteFile(p, b, 0o644); err != nil {
			return "", err
		}
	}
	return dir, nil
}

// TFCacheDir extracts the real tf library under the user cache directory.
func TFCacheDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		base = os.TempDir()
	}
	return ExtractTF(filepath.Join(base, "gtf", "tflib-"+TFVersion()))
}

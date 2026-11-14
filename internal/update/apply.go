// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package update

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// ReplaceBinary puts newBin at exePath: it is written to exePath+".new" (same directory, so the
// rename cannot cross filesystems), the current file is kept as exePath+".old" (a hard link, or a
// copy where links are not possible) and ".new" is renamed over exePath. The rename is atomic and,
// on Unix, safe while the old binary is running. Returns the path of the kept previous binary.
func ReplaceBinary(exePath string, newBin []byte) (string, error) {
	newPath, oldPath := exePath+".new", exePath+".old"
	if err := os.WriteFile(newPath, newBin, 0o755); err != nil {
		return "", fmt.Errorf("cannot write next to the binary (%s): %w", filepath.Dir(exePath), err)
	}
	if err := os.Chmod(newPath, 0o755); err != nil {
		_ = os.Remove(newPath)
		return "", err
	}
	kept := ""
	if _, err := os.Stat(exePath); err == nil {
		_ = os.Remove(oldPath)
		if err := os.Link(exePath, oldPath); err != nil {
			if err := copyFile(exePath, oldPath); err != nil {
				_ = os.Remove(newPath)
				return "", fmt.Errorf("keeping the previous binary: %w", err)
			}
		}
		kept = oldPath
	}
	if err := os.Rename(newPath, exePath); err != nil {
		_ = os.Remove(newPath)
		return "", err
	}
	return kept, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

// IsPermission reports whether err is "you may not write there".
func IsPermission(err error) bool { return errors.Is(err, fs.ErrPermission) }

// CanWrite reports whether files can be created in dir (it is created if it does not exist and
// its parent is writable).
func CanWrite(dir string) error {
	f, err := os.CreateTemp(dir, ".gtf-write-test-*")
	if err != nil {
		return err
	}
	name := f.Name()
	f.Close()
	return os.Remove(name)
}

// swapDir replaces dir with the freshly built next (a sibling), keeping the old tree as dir+".old".
// It returns a function that puts the old tree back.
func swapDir(dir, next string) (rollback func(), err error) {
	old := dir + ".old"
	_ = os.RemoveAll(old)
	had := false
	if _, err := os.Stat(dir); err == nil {
		had = true
		if err := os.Rename(dir, old); err != nil {
			return nil, err
		}
	}
	if err := os.Rename(next, dir); err != nil {
		if had {
			_ = os.Rename(old, dir)
		}
		return nil, err
	}
	return func() {
		if had {
			_ = os.RemoveAll(dir)
			_ = os.Rename(old, dir)
		}
	}, nil
}

// Updater checks for and installs new versions of gtf.
type Updater struct {
	Client  *Client
	Version string // the running version; one that is not semver (a development build) is never updated
	ExePath string // the running binary's path
	LibDir  string // the installed library directory, or "" when gtf runs from its embedded library
	Rec     Record // how it was installed (scope, directories); written back after an update
	GOOS    string
	GOARCH  string
}

// Result is the outcome of Check or Update.
type Result struct {
	Current string
	Latest  string // "" when nothing was found
	Newer   bool
	Applied bool
	Kept    string // the previous binary, after an update
}

// Check looks up the newest release for the channel and says whether it is newer than the running one.
// A development (non-semver) running version is unknown: no network call is made.
func (u *Updater) Check(ctx context.Context, channel string) (res Result, rel Release, err error) {
	res = Result{Current: u.Version}
	if _, perr := ParseVersion(u.Version); perr != nil {
		return res, Release{}, fmt.Errorf("this is a development build (version %q): it is never updated", u.Version)
	}
	rel, err = u.Client.Latest(ctx, channel)
	if err != nil {
		return res, rel, err
	}
	res.Latest = rel.Version()
	res.Newer = Newer(u.Version, rel.Tag)
	return res, rel, nil
}

// Update installs the release over the running binary and, when there is an installed library
// directory, over that too. Both are checked for write permission first, so a user who needs
// sudo hears so before anything is touched. The library is swapped first and put back if the
// binary cannot be replaced.
func (u *Updater) Update(ctx context.Context, rel Release) (Result, error) {
	res := Result{Current: u.Version, Latest: rel.Version(), Newer: true}
	if err := CanWrite(filepath.Dir(u.ExePath)); err != nil {
		return res, fmt.Errorf("cannot write to %s: %w", filepath.Dir(u.ExePath), err)
	}
	if u.LibDir != "" {
		if err := CanWrite(filepath.Dir(u.LibDir)); err != nil {
			return res, fmt.Errorf("cannot write to %s: %w", filepath.Dir(u.LibDir), err)
		}
	}
	tgz, err := u.Client.Download(ctx, rel, u.GOOS, u.GOARCH)
	if err != nil {
		return res, err
	}
	bin, err := ExtractFile(tgz, "bin/gtf")
	if err != nil {
		return res, err
	}
	var rollback func()
	if u.LibDir != "" {
		next := u.LibDir + ".new"
		_ = os.RemoveAll(next)
		if err := ExtractTree(tgz, "lib/gtf", next); err != nil {
			_ = os.RemoveAll(next)
			return res, err
		}
		// what belongs to this machine, not to the release, carries over
		if b, err := os.ReadFile(filepath.Join(u.LibDir, "local.tf")); err == nil {
			_ = os.WriteFile(filepath.Join(next, "local.tf"), b, 0o644)
		}
		rec := u.Rec
		rec.Version, rec.LibDir = rel.Version(), u.LibDir
		if rec.Scope == "" {
			rec.Scope = "user"
		}
		if rec.BinDir == "" {
			rec.BinDir = filepath.Dir(u.ExePath)
		}
		if err := rec.Save(next); err != nil {
			_ = os.RemoveAll(next)
			return res, err
		}
		if rollback, err = swapDir(u.LibDir, next); err != nil {
			_ = os.RemoveAll(next)
			return res, fmt.Errorf("replacing %s: %w", u.LibDir, err)
		}
	}
	kept, err := ReplaceBinary(u.ExePath, bin)
	if err != nil {
		if rollback != nil {
			rollback()
		}
		return res, err
	}
	res.Applied, res.Kept = true, kept
	return res, nil
}

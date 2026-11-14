// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

// Package handoff is the versioned state file one gtf process leaves for the
// next one when it restarts itself (/restart, docs/RESTART.md). It knows the
// envelope only: named sections, each with its own version and a required
// flag, a list saying which inherited descriptor belongs to which section and
// item, and a checksum. What a section contains is its owner's business.
//
// The rules are those of squatClientDaemon's docs/RELOAD.md ("version map"):
//
//  1. Reading older state always works: a section carries the version it was
//     written with and its reader migrates it.
//  2. Reading newer state is allowed only when the writer says so: MinReader is
//     the oldest reader format that can still use the file.
//  3. Unknown sections are skipped unless they are marked required, in which
//     case the reader refuses. A known section written at a higher version than
//     the reader knows follows the same rule.
//  4. Refusing is clean: every check happens before anything is touched.
package handoff

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Magic identifies a state file; Format is the envelope version written by this build.
const (
	Magic  = "GTFRESTART"
	Format = 1
)

// Errors returned by Decode (use errors.Is).
var (
	ErrCorrupt = errors.New("handoff: the state file is corrupt")
	ErrTooNew  = errors.New("handoff: the state file was written by a newer gtf that this one cannot read")
)

// Section is one named part of the state.
type Section struct {
	V        int             `json:"v"`
	Required bool            `json:"required,omitempty"` // a reader that does not know this section must refuse the file
	Data     json.RawMessage `json:"data"`
}

// FD says which section and item owns inherited descriptor number Slot. The
// new process receives the descriptors' numbers in the environment (see the
// client's restart code), in slot order; sections refer to them by slot.
type FD struct {
	Section string `json:"section"`
	Item    string `json:"item"`
}

// File is the whole state.
type File struct {
	Magic     string             `json:"magic"`
	Format    int                `json:"format"`
	MinReader int                `json:"minReader"` // oldest reader format that can use this file (0 = same as Format)
	Writer    string             `json:"writer,omitempty"`
	Created   int64              `json:"created"`
	FDs       []FD               `json:"fds,omitempty"`
	Sections  map[string]Section `json:"sections"`
	Sum       string             `json:"sum"` // SHA-256 over the sections, against truncation and bit rot (not authentication)
}

// New returns an empty file for this build.
func New(writer string, created int64) *File {
	return &File{Magic: Magic, Format: Format, MinReader: Format, Writer: writer, Created: created, Sections: map[string]Section{}}
}

// Set adds a section with the given version, encoding v as JSON.
func (f *File) Set(name string, version int, required bool, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("handoff: section %s: %w", name, err)
	}
	f.Sections[name] = Section{V: version, Required: required, Data: b}
	return nil
}

func (f *File) checksum() string {
	h := sha256.New()
	names := make([]string, 0, len(f.Sections))
	for n := range f.Sections {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		s := f.Sections[n]
		fmt.Fprintf(h, "%s\x00%d\x00%t\x00", n, s.V, s.Required)
		h.Write(s.Data)
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Encode serialises the file (computing the checksum).
func (f *File) Encode() ([]byte, error) {
	f.Sum = f.checksum()
	return json.Marshal(f)
}

// Known maps a section name to the highest version this reader understands.
type Known map[string]int

// Bundle is a decoded, accepted file.
type Bundle struct {
	File    *File
	Skipped []string // optional sections this reader does not know (or knows only at a lower version)
}

// Section returns a section the reader knows.
func (b *Bundle) Section(name string) (Section, bool) {
	s, ok := b.File.Sections[name]
	for _, sk := range b.Skipped {
		if sk == name {
			return Section{}, false
		}
	}
	return s, ok
}

// Decode parses and checks a state file against what this reader knows. Any
// problem returns an error and nothing is applied.
func Decode(data []byte, known Known) (*Bundle, error) {
	var f File
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCorrupt, err)
	}
	if f.Magic != Magic {
		return nil, fmt.Errorf("%w: not a gtf restart file", ErrCorrupt)
	}
	min := f.MinReader
	if min == 0 {
		min = f.Format
	}
	if min > Format {
		return nil, fmt.Errorf("%w (file format %d needs a reader of format %d, this build reads %d)", ErrTooNew, f.Format, min, Format)
	}
	if f.Sections == nil {
		f.Sections = map[string]Section{}
	}
	if f.checksum() != f.Sum {
		return nil, fmt.Errorf("%w: checksum mismatch", ErrCorrupt)
	}
	b := &Bundle{File: &f}
	names := make([]string, 0, len(f.Sections))
	for n := range f.Sections {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		s := f.Sections[n]
		kv, ok := known[n]
		switch {
		case !ok || s.V > kv:
			if s.Required {
				return nil, fmt.Errorf("%w: required section %q (version %d) is unknown to this build", ErrTooNew, n, s.V)
			}
			b.Skipped = append(b.Skipped, n)
		case s.V < 0:
			return nil, fmt.Errorf("%w: section %q has an invalid version", ErrCorrupt, n)
		}
	}
	for _, fd := range f.FDs {
		if _, ok := f.Sections[fd.Section]; !ok {
			return nil, fmt.Errorf("%w: descriptor for unknown section %q", ErrCorrupt, fd.Section)
		}
	}
	return b, nil
}

// Write stores the file at path: created 0600 in a private directory, written
// to a temporary name, fsynced and renamed. The content can include key
// material of live TLS sessions, so the new process deletes it right after
// reading it and the old one deletes it when a restart is abandoned.
func Write(path string, f *File) error {
	data, err := f.Encode()
	if err != nil {
		return err
	}
	defer clear(data)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".restart-*")
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		if !ok {
			tmp.Close()
			os.Remove(tmp.Name())
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return err
	}
	ok = true
	return nil
}

// Read loads and decodes the file at path. With remove it is deleted
// immediately after being read, whether or not decoding succeeds. A file that
// is readable by others is refused (it may hold key material).
func Read(path string, known Known, remove bool) (*Bundle, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if fi.Mode().Perm()&0o077 != 0 {
		if remove {
			os.Remove(path)
		}
		return nil, fmt.Errorf("handoff: %s is accessible by others (mode %v); refusing it", path, fi.Mode().Perm())
	}
	data, err := os.ReadFile(path)
	if remove {
		os.Remove(path)
	}
	if err != nil {
		return nil, err
	}
	defer clear(data)
	b, err := Decode(data, known)
	if err != nil {
		return nil, err
	}
	return b, nil
}

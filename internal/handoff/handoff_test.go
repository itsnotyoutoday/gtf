// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package handoff

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sample(t *testing.T) *File {
	f := New("gtf test", 1700000000)
	if err := f.Set("sessions", 1, true, map[string]any{"a": 1}); err != nil {
		t.Fatal(err)
	}
	if err := f.Set("extra", 3, false, []string{"x"}); err != nil {
		t.Fatal(err)
	}
	f.FDs = []FD{{Section: "sessions", Item: "w"}}
	return f
}

func TestRoundTripAndPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "state")
	if err := Write(path, sample(t)); err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(path)
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", fi.Mode().Perm())
	}
	if d, _ := os.Stat(filepath.Dir(path)); d.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode %v", d.Mode().Perm())
	}
	b, err := Read(path, Known{"sessions": 1, "extra": 3}, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("file not deleted after reading")
	}
	s, ok := b.Section("sessions")
	if !ok || s.V != 1 || string(s.Data) != `{"a":1}` {
		t.Fatalf("%+v", s)
	}
	if len(b.File.FDs) != 1 || b.File.FDs[0].Item != "w" || len(b.Skipped) != 0 {
		t.Fatalf("%+v", b)
	}
	// no leftover temporary files
	if ents, _ := os.ReadDir(filepath.Dir(path)); len(ents) != 0 {
		t.Fatalf("leftovers: %v", ents)
	}
}

func TestVersionRules(t *testing.T) {
	data, _ := sample(t).Encode()
	// an older reader: an optional section it does not know, or knows only at a lower version, is skipped
	b, err := Decode(data, Known{"sessions": 1})
	if err != nil || len(b.Skipped) != 1 || b.Skipped[0] != "extra" {
		t.Fatalf("%v %+v", err, b)
	}
	if _, ok := b.Section("extra"); ok {
		t.Fatal("skipped section returned")
	}
	b, err = Decode(data, Known{"sessions": 1, "extra": 2})
	if err != nil || len(b.Skipped) != 1 {
		t.Fatalf("lower version: %v %+v", err, b)
	}
	// a required section the reader does not know: refused
	if _, err := Decode(data, Known{"extra": 3}); !errors.Is(err, ErrTooNew) {
		t.Fatalf("got %v", err)
	}
	// ... also when the version is higher than the reader's
	f := New("w", 1)
	f.Set("sessions", 5, true, 1)
	d2, _ := f.Encode()
	if _, err := Decode(d2, Known{"sessions": 1}); !errors.Is(err, ErrTooNew) {
		t.Fatalf("got %v", err)
	}
	// a newer envelope whose writer says old readers cannot use it
	f = sample(t)
	f.Format, f.MinReader = Format+1, Format+1
	d3, _ := f.Encode()
	if _, err := Decode(d3, Known{"sessions": 1, "extra": 3}); !errors.Is(err, ErrTooNew) {
		t.Fatalf("got %v", err)
	}
	// ... but one that only added optional things (minReader unchanged) is read
	f = sample(t)
	f.Format = Format + 1
	d4, _ := f.Encode()
	if _, err := Decode(d4, Known{"sessions": 1, "extra": 3}); err != nil {
		t.Fatalf("compatible newer format refused: %v", err)
	}
}

func TestCorruption(t *testing.T) {
	data, _ := sample(t).Encode()
	known := Known{"sessions": 1, "extra": 3}
	cases := map[string][]byte{
		"empty":     nil,
		"garbage":   []byte("not json at all"),
		"truncated": data[:len(data)/2],
		"bad magic": []byte(strings.Replace(string(data), Magic, "OTHER", 1)),
		"bit flip":  []byte(strings.Replace(string(data), `{"a":1}`, `{"a":2}`, 1)),
		"no sum":    []byte(strings.Replace(string(data), `"sum":"`, `"sum":"0`, 1)),
	}
	for name, d := range cases {
		if _, err := Decode(d, known); !errors.Is(err, ErrCorrupt) {
			t.Errorf("%s: got %v", name, err)
		}
	}
	// a descriptor naming a section that is not there
	f := sample(t)
	f.FDs = append(f.FDs, FD{Section: "ghost"})
	d, _ := f.Encode()
	if _, err := Decode(d, known); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("got %v", err)
	}
	var v any
	if json.Unmarshal(data, &v) != nil {
		t.Fatal("sample is not JSON")
	}
}

func TestReadRefusesWorldReadable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state")
	Write(path, sample(t))
	os.Chmod(path, 0o644)
	if _, err := Read(path, Known{"sessions": 1}, true); err == nil {
		t.Fatal("accepted a world-readable file")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("refused file not deleted")
	}
}

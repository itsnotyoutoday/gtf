//go:build !pkgbuild

// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSettings(t *testing.T) {
	dir := t.TempDir()
	p := ConfigPath(dir)
	if s, err := Load(p); err != nil || s != Defaults() || !s.UpdateCheck || s.UpdateChannel != ChannelPrerelease {
		t.Fatalf("missing file: %+v %v", s, err)
	}
	write := func(body string) { _ = os.WriteFile(p, []byte(body), 0o600) }
	write(`{"update_check": false, "other": {"x": 1}}`)
	if s, err := Load(p); err != nil || s.UpdateCheck || s.UpdateChannel != ChannelPrerelease {
		t.Fatalf("partial: %+v %v", s, err)
	}
	write(`{"update_channel":"nightly"}`)
	if s, err := Load(p); err == nil || s.UpdateChannel != ChannelPrerelease {
		t.Fatalf("bad channel: %+v %v", s, err)
	}
	write(`{"keep": 7}`)
	if err := (Settings{UpdateCheck: false, UpdateChannel: ChannelStable}).Save(p); err != nil {
		t.Fatal(err)
	}
	s, err := Load(p)
	if err != nil || s.UpdateCheck || s.UpdateChannel != ChannelStable {
		t.Fatalf("round trip: %+v %v", s, err)
	}
	if b, _ := os.ReadFile(p); !strings.Contains(string(b), `"keep"`) {
		t.Fatal("unknown field dropped on save")
	}
}

func TestVersionCompare(t *testing.T) {
	order := []string{"0.0.0-abc1234", "0.1.0-test1", "0.1.0-test2", "0.1.0", "v0.1.1", "0.2.0", "1.0.0"}
	for i := range order {
		for j := range order {
			a, err := ParseVersion(order[i])
			if err != nil {
				t.Fatal(err)
			}
			b, _ := ParseVersion(order[j])
			want := 0
			if i < j {
				want = -1
			} else if i > j {
				want = 1
			}
			if got := a.Compare(b); got != want {
				t.Errorf("%s vs %s = %d want %d", order[i], order[j], got, want)
			}
		}
	}
	for _, bad := range []string{"dev", "", "1.2", "1.2.x", "01.2.3", "1.2.3-"} {
		if _, err := ParseVersion(bad); err == nil {
			t.Errorf("ParseVersion(%q) accepted", bad)
		}
	}
	if Newer("dev", "9.9.9") || !Newer("0.1.0-test1", "v0.1.0") || Newer("0.1.0", "0.1.0") {
		t.Error("Newer")
	}
}

func TestAssetName(t *testing.T) {
	cases := map[[2]string]string{
		{"linux", "amd64"}:  "gtf-0.1.0-linux-amd64.tar.gz",
		{"linux", "arm"}:    "gtf-0.1.0-linux-armv7.tar.gz",
		{"darwin", "arm64"}: "gtf-0.1.0-darwin-arm64.tar.gz",
	}
	for k, want := range cases {
		if got := AssetName("v0.1.0-rel", k[0], k[1]); got != want {
			t.Errorf("%v: %s want %s", k, got, want)
		}
	}
}

func sum(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }

func TestVerifyChecksum(t *testing.T) {
	data := []byte("hello")
	good := fmt.Sprintf("%s  a.tar.gz\n%s *b.tar.gz\n", sum(data), strings.Repeat("0", 64))
	if err := VerifyChecksum([]byte(good), "a.tar.gz", data); err != nil {
		t.Fatal(err)
	}
	if err := VerifyChecksum([]byte(good), "b.tar.gz", data); err == nil || !strings.Contains(err.Error(), "mismatch") {
		t.Fatalf("mismatch not caught: %v", err)
	}
	if err := VerifyChecksum([]byte(good), "c.tar.gz", data); err == nil {
		t.Fatal("missing entry accepted")
	}
}

func TestPickLatest(t *testing.T) {
	rels := []Release{{Tag: "v0.1.0-alpha1", Prerelease: true}, {Tag: "v0.2.0-beta1", Prerelease: true}, {Tag: "v0.1.0-rel"}, {Tag: "v9.9.9", Draft: true}, {Tag: "nightly"}}
	if r, err := PickLatest(rels, ChannelPrerelease); err != nil || r.Tag != "v0.2.0-beta1" {
		t.Fatalf("%v %v", r.Tag, err)
	}
	if r, err := PickLatest(rels, ChannelStable); err != nil || r.Tag != "v0.1.0-rel" {
		t.Fatalf("%v %v", r.Tag, err)
	}
	if _, err := PickLatest(rels[:2], ChannelStable); err != ErrNoRelease {
		t.Fatalf("%v", err)
	}
}

func makeTarball(t *testing.T, ver, label string, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	dir := fmt.Sprintf("gtf-%s-%s", ver, label)
	_ = tw.WriteHeader(&tar.Header{Name: dir + "/", Mode: 0o755, Typeflag: tar.TypeDir})
	for n, c := range files {
		mode := int64(0o644)
		if strings.HasPrefix(n, "bin/") {
			mode = 0o755
		}
		_ = tw.WriteHeader(&tar.Header{Name: dir + "/" + n, Mode: mode, Size: int64(len(c)), Typeflag: tar.TypeReg})
		_, _ = tw.Write([]byte(c))
	}
	_ = tw.Close()
	_ = zw.Close()
	return buf.Bytes()
}

var releaseFiles = map[string]string{
	"bin/gtf":               "NEW-gtf",
	"lib/gtf/tf/stdlib.tf":  "NEW-stdlib",
	"lib/gtf/gtf/layout.tf": "NEW-layout",
	"lib/gtf/examples/a.tf": "NEW-example",
	"README.md":             "x",
}

// fakeReleases serves a releases list, tarballs and SHA256SUMS; corrupt makes the checksum wrong.
func fakeReleases(t *testing.T, ver string, corrupt bool) (*Client, *int) {
	t.Helper()
	tgz := makeTarball(t, ver, AssetLabel("linux", "amd64"), releaseFiles)
	name := AssetName(ver, "linux", "amd64")
	digest := sum(tgz)
	if corrupt {
		digest = strings.Repeat("a", 64)
	}
	hits := new(int)
	mux := http.NewServeMux()
	var base string
	mux.HandleFunc("/repos/o/r/releases", func(w http.ResponseWriter, r *http.Request) {
		*hits++
		_ = json.NewEncoder(w).Encode([]Release{
			{Tag: "v0.0.1-rel"},
			{Tag: "v" + ver, Prerelease: strings.Contains(ver, "-"), Assets: []Asset{
				{Name: name, URL: base + "/dl/" + name}, {Name: "SHA256SUMS", URL: base + "/dl/SHA256SUMS"}}},
		})
	})
	mux.HandleFunc("/dl/"+name, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(tgz) })
	mux.HandleFunc("/dl/SHA256SUMS", func(w http.ResponseWriter, r *http.Request) { fmt.Fprintf(w, "%s  %s\n", digest, name) })
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	base = ts.URL
	return &Client{HTTP: ts.Client(), API: ts.URL, Repo: "o/r"}, hits
}

func TestUpdateReplacesBinaryAndLibrary(t *testing.T) {
	c, _ := fakeReleases(t, "0.3.0-beta1", false)
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	lib := filepath.Join(root, "lib", "gtf")
	_ = os.MkdirAll(bin, 0o755)
	_ = os.MkdirAll(filepath.Join(lib, "tf"), 0o755)
	exe := filepath.Join(bin, "gtf")
	_ = os.WriteFile(exe, []byte("OLD"), 0o755)
	_ = os.WriteFile(filepath.Join(lib, "tf", "stdlib.tf"), []byte("OLD-stdlib"), 0o644)
	_ = os.WriteFile(filepath.Join(lib, "local.tf"), []byte("MY-LOCAL"), 0o644)
	u := &Updater{Client: c, Version: "0.2.0", ExePath: exe, LibDir: lib, Rec: Record{Scope: "user", BinDir: bin}, GOOS: "linux", GOARCH: "amd64"}
	res, rel, err := u.Check(context.Background(), ChannelPrerelease)
	if err != nil || !res.Newer || res.Latest != "0.3.0-beta1" {
		t.Fatalf("%+v %v", res, err)
	}
	if _, _, err := u.Check(context.Background(), ChannelStable); err != nil {
		if err != ErrNoRelease {
			t.Fatalf("stable: %v", err)
		}
	}
	res, err = u.Update(context.Background(), rel)
	if err != nil || !res.Applied {
		t.Fatalf("%+v %v", res, err)
	}
	read := func(p string) string { b, _ := os.ReadFile(p); return string(b) }
	if read(exe) != "NEW-gtf" || read(exe+".old") != "OLD" {
		t.Fatalf("binary %q old %q", read(exe), read(exe+".old"))
	}
	if read(filepath.Join(lib, "tf", "stdlib.tf")) != "NEW-stdlib" || read(filepath.Join(lib, "examples", "a.tf")) != "NEW-example" {
		t.Fatal("library not replaced")
	}
	if read(filepath.Join(lib, "local.tf")) != "MY-LOCAL" {
		t.Fatal("the user's local.tf was lost")
	}
	if rec, err := LoadRecord(lib); err != nil || rec.Version != "0.3.0-beta1" || rec.Scope != "user" {
		t.Fatalf("record %+v %v", rec, err)
	}
	if read(filepath.Join(lib+".old", "tf", "stdlib.tf")) != "OLD-stdlib" {
		t.Fatal("previous library not kept")
	}
	if fi, _ := os.Stat(exe); fi.Mode().Perm()&0o100 == 0 {
		t.Fatal("not executable")
	}
}

func TestUpdateRefusesBadChecksumAndTouchesNothing(t *testing.T) {
	c, _ := fakeReleases(t, "0.3.0", true)
	root := t.TempDir()
	exe := filepath.Join(root, "gtf")
	_ = os.WriteFile(exe, []byte("OLD"), 0o755)
	u := &Updater{Client: c, Version: "0.2.0", ExePath: exe, GOOS: "linux", GOARCH: "amd64"}
	_, rel, err := u.Check(context.Background(), ChannelStable)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := u.Update(context.Background(), rel); err == nil || !strings.Contains(err.Error(), "mismatch") {
		t.Fatalf("err = %v", err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "OLD" {
		t.Fatal("binary replaced despite bad checksum")
	}
}

func TestUpdateNeedsWritableDirsAndRollsBack(t *testing.T) {
	c, _ := fakeReleases(t, "0.3.0", false)
	root := t.TempDir()
	lib := filepath.Join(root, "lib", "gtf")
	_ = os.MkdirAll(filepath.Join(lib, "tf"), 0o755)
	_ = os.WriteFile(filepath.Join(lib, "tf", "stdlib.tf"), []byte("OLD-stdlib"), 0o644)
	// the binary's directory does not exist: Update must fail before touching the library
	u := &Updater{Client: c, Version: "0.2.0", ExePath: filepath.Join(root, "nodir", "gtf"), LibDir: lib, GOOS: "linux", GOARCH: "amd64"}
	_, rel, _ := u.Check(context.Background(), ChannelStable)
	if _, err := u.Update(context.Background(), rel); err == nil {
		t.Fatal("expected an error")
	}
	if b, _ := os.ReadFile(filepath.Join(lib, "tf", "stdlib.tf")); string(b) != "OLD-stdlib" {
		t.Fatal("library changed although the update could not run")
	}
}

func TestDevBuildNeverChecks(t *testing.T) {
	c, hits := fakeReleases(t, "0.3.0", false)
	u := &Updater{Client: c, Version: "dev", ExePath: filepath.Join(t.TempDir(), "x"), GOOS: "linux", GOARCH: "amd64"}
	if _, _, err := u.Check(context.Background(), ChannelStable); err == nil || *hits != 0 {
		t.Fatalf("dev build checked the network: %v hits=%d", err, *hits)
	}
	u.Version, u.GOARCH = "0.1.0", "riscv64"
	_, rel, err := u.Check(context.Background(), ChannelStable)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := u.Update(context.Background(), rel); err == nil || !strings.Contains(err.Error(), "no file") {
		t.Fatalf("err = %v", err)
	}
}

func TestArchiveRejectsEscapes(t *testing.T) {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	_ = tw.WriteHeader(&tar.Header{Name: "gtf-1.0.0-linux-amd64/../../evil", Mode: 0o644, Size: 1, Typeflag: tar.TypeReg})
	_, _ = tw.Write([]byte("x"))
	_ = tw.Close()
	_ = zw.Close()
	if err := ExtractTree(buf.Bytes(), "lib/gtf", filepath.Join(t.TempDir(), "d")); err == nil || !strings.Contains(err.Error(), "unsafe") {
		t.Fatalf("path escape accepted: %v", err)
	}
	if _, err := ExtractFile(makeTarball(t, "1.0.0", "linux-amd64", map[string]string{"other": "x"}), "bin/gtf"); err == nil {
		t.Fatal("missing binary accepted")
	}
}

func TestFindLibDir(t *testing.T) {
	root := t.TempDir()
	mk := func(p string) string {
		d := filepath.Join(root, p)
		_ = os.MkdirAll(filepath.Join(d, "tf"), 0o755)
		_ = os.WriteFile(filepath.Join(d, "tf", "stdlib.tf"), nil, 0o644)
		return d
	}
	next := mk("pfx/lib/gtf")
	_ = os.MkdirAll(filepath.Join(root, "pfx", "bin"), 0o755)
	exe := filepath.Join(root, "pfx", "bin", "gtf")
	_ = os.WriteFile(exe, nil, 0o755)
	env := map[string]string{}
	get := func(k string) string { return env[k] }
	if realNext, err := filepath.EvalSymlinks(next); err == nil {
		next = realNext
	}
	if got := FindLibDir(exe, "/nonexistent", get); got != next {
		t.Fatalf("next to the binary: %q want %q", got, next)
	}
	over := mk("over")
	env["GTF_LIBDIR"] = over
	if got := FindLibDir(exe, "/nonexistent", get); got != over {
		t.Fatalf("GTF_LIBDIR: %q", got)
	}
	env["GTF_LIBDIR"] = filepath.Join(root, "nothing")
	if got := FindLibDir(filepath.Join(root, "elsewhere", "gtf"), filepath.Join(root, "nohome"), get); strings.HasPrefix(got, root) {
		t.Fatalf("found a library that is not there: %q", got)
	}
}

func TestStateAndDue(t *testing.T) {
	p := StatePath(t.TempDir())
	if s := LoadState(p); !s.Due(time.Now()) || s.AvailableFor("0.1.0") != "" {
		t.Fatalf("%+v", s)
	}
	now := time.Now()
	if err := (State{LastCheck: now.UnixMilli(), Available: "0.2.0"}).Save(p); err != nil {
		t.Fatal(err)
	}
	s := LoadState(p)
	if s.Due(now.Add(23*time.Hour)) || !s.Due(now.Add(25*time.Hour)) {
		t.Fatal("Due")
	}
	if s.AvailableFor("0.1.0") != "0.2.0" || s.AvailableFor("0.2.0") != "" || s.AvailableFor("dev") != "" {
		t.Fatal("AvailableFor")
	}
}

func TestManagedInstallNeverUpdates(t *testing.T) {
	c, _ := fakeReleases(t, "0.3.0", false)
	root := t.TempDir()
	exe := filepath.Join(root, "gtf")
	_ = os.WriteFile(exe, []byte("OLD"), 0o755)
	for _, rec := range []Record{{Scope: "package"}, {Scope: "global", ManagedBy: "rpm"}, {Scope: "package", ManagedBy: "brew"}} {
		u := &Updater{Client: c, Version: "0.2.0", ExePath: exe, Rec: rec, GOOS: "linux", GOARCH: "amd64"}
		var out strings.Builder
		res, err := u.Run(context.Background(), ChannelStable, true, &out)
		if err != nil || !res.Newer || res.Applied {
			t.Fatalf("%+v: %+v %v", rec, res, err)
		}
		want := "system administrator"
		if rec.ManagedBy == "brew" {
			want = "brew upgrade gtf"
		}
		if !strings.Contains(out.String(), want) || !strings.Contains(out.String(), "0.3.0") {
			t.Fatalf("advice missing: %q", out.String())
		}
		_, rel, _ := u.Check(context.Background(), ChannelStable)
		if _, err := u.Update(context.Background(), rel); err != ErrManaged {
			t.Fatalf("Update on a managed install: %v", err)
		}
		if b, _ := os.ReadFile(exe); string(b) != "OLD" {
			t.Fatal("a package-managed binary was replaced")
		}
	}
	cellar := filepath.Join(root, "opt", "homebrew", "Cellar", "gtf", "0.2.0", "bin")
	_ = os.MkdirAll(cellar, 0o755)
	_ = os.WriteFile(filepath.Join(cellar, "gtf"), nil, 0o755)
	if !IsBrewPath(filepath.Join(cellar, "gtf")) || IsBrewPath(filepath.Join(root, "gtf")) {
		t.Fatal("IsBrewPath")
	}
	if !IsSystemPath("/usr/bin/gtf") || IsSystemPath("/usr/local/bin/gtf") || IsSystemPath(filepath.Join(root, "gtf")) {
		t.Fatal("IsSystemPath")
	}
}

// When the API is rate limited (403) the release feed still finds the newest release and its files.
func TestLatestFallsBackToTheFeedWhenTheAPIIsRateLimited(t *testing.T) {
	tgz := makeTarball(t, "0.4.0-alpha1", AssetLabel("linux", "amd64"), releaseFiles)
	name := AssetName("0.4.0-alpha1", "linux", "amd64")
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/o/r/releases", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"API rate limit exceeded"}`, http.StatusForbidden)
	})
	mux.HandleFunc("/o/r/releases.atom", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<feed><entry><id>tag:github.com,2008:Repository/1/v0.4.0-alpha1</id></entry>`+
			`<entry><id>tag:github.com,2008:Repository/1/v0.3.0-rel</id></entry>`+
			`<entry><id>tag:github.com,2008:Repository/1/nightly</id></entry></feed>`)
	})
	mux.HandleFunc("/o/r/releases/download/v0.4.0-alpha1/"+name, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(tgz) })
	mux.HandleFunc("/o/r/releases/download/v0.4.0-alpha1/SHA256SUMS", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%s  %s\n", sum(tgz), name)
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()
	c := &Client{HTTP: ts.Client(), API: ts.URL, Web: ts.URL, Repo: "o/r"}
	rel, err := c.Latest(context.Background(), ChannelPrerelease)
	if err != nil || rel.Tag != "v0.4.0-alpha1" || !rel.Prerelease {
		t.Fatalf("prerelease channel: %+v %v", rel, err)
	}
	if rel, err = c.Latest(context.Background(), ChannelStable); err != nil || rel.Tag != "v0.3.0-rel" {
		t.Fatalf("stable channel: %+v %v", rel, err)
	}
	rel, _ = c.Latest(context.Background(), ChannelPrerelease)
	if data, err := c.Download(context.Background(), rel, "linux", "amd64"); err != nil || len(data) == 0 {
		t.Fatalf("download through the feed: %v", err)
	}
	// both the API and the feed down: the API's error is reported
	bad := &Client{HTTP: ts.Client(), API: ts.URL, Web: ts.URL + "/none", Repo: "o/r"}
	if _, err := bad.Latest(context.Background(), ChannelStable); err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("want the API's 403, got %v", err)
	}
}

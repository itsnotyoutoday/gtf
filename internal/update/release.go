// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package update

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// DefaultRepo and DefaultAPI are where releases are published.
const (
	DefaultRepo = "itsnotyoutoday/gtf"
	DefaultAPI  = "https://api.github.com"
)

const (
	maxAPIBody   = 4 << 20
	maxAsset     = 200 << 20
	maxFile      = 150 << 20
	maxTree      = 400 << 20
	checksumFile = "SHA256SUMS"
)

// Doer is the part of *http.Client that is used; tests inject an httptest-backed client.
type Doer interface {
	Do(*http.Request) (*http.Response, error)
}

// Client talks to the GitHub releases API.
type Client struct {
	HTTP Doer   // default http.DefaultClient
	API  string // default DefaultAPI
	Repo string // default DefaultRepo
}

// Asset is one downloadable file of a release.
type Asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}

// Release is a GitHub release.
type Release struct {
	Tag        string  `json:"tag_name"`
	Prerelease bool    `json:"prerelease"`
	Draft      bool    `json:"draft"`
	Assets     []Asset `json:"assets"`
}

// Version is the tag without a leading "v" and without the "-rel" marker of final releases.
func (r Release) Version() string { return strings.TrimSuffix(strings.TrimPrefix(r.Tag, "v"), "-rel") }

func (c *Client) doer() Doer {
	if c != nil && c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

func (c *Client) get(ctx context.Context, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "gtf-update")
	req.Header.Set("Accept", "application/vnd.github+json, application/octet-stream, */*")
	resp, err := c.doer().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("GET %s: response too large", url)
	}
	return b, nil
}

// Latest returns the newest (by semver, not by API order) published release allowed by the channel.
// Releases whose tag is not a version, and drafts, are ignored. ErrNoRelease if none qualifies.
func (c *Client) Latest(ctx context.Context, channel string) (Release, error) {
	api, repo := DefaultAPI, DefaultRepo
	if c != nil && c.API != "" {
		api = strings.TrimRight(c.API, "/")
	}
	if c != nil && c.Repo != "" {
		repo = c.Repo
	}
	body, err := c.get(ctx, fmt.Sprintf("%s/repos/%s/releases?per_page=50", api, repo), maxAPIBody)
	if err != nil {
		return Release{}, err
	}
	var rels []Release
	if err := json.Unmarshal(body, &rels); err != nil {
		return Release{}, fmt.Errorf("releases list: %w", err)
	}
	return PickLatest(rels, channel)
}

// ErrNoRelease means no release matches the channel.
var ErrNoRelease = errors.New("no matching release found")

// PickLatest chooses the newest release for the channel from a list.
func PickLatest(rels []Release, channel string) (Release, error) {
	var best Release
	var bestV Version
	found := false
	for _, r := range rels {
		if r.Draft {
			continue
		}
		v, err := ParseVersion(r.Tag)
		if err != nil {
			continue
		}
		if channel == ChannelStable && (r.Prerelease || v.IsPrerelease()) {
			continue
		}
		if !found || v.Compare(bestV) > 0 {
			best, bestV, found = r, v, true
		}
	}
	if !found {
		return Release{}, ErrNoRelease
	}
	return best, nil
}

// AssetLabel is the os-arch part of the release file name: GOARCH "arm" is published as "armv7".
func AssetLabel(goos, goarch string) string {
	if goarch == "arm" {
		goarch = "armv7"
	}
	return goos + "-" + goarch
}

// AssetName is the tarball published for a version and platform, e.g. gtf-0.1.0-linux-amd64.tar.gz.
func AssetName(version, goos, goarch string) string {
	return fmt.Sprintf("gtf-%s-%s.tar.gz", strings.TrimSuffix(strings.TrimPrefix(version, "v"), "-rel"), AssetLabel(goos, goarch))
}

// FindAsset returns the release asset called name.
func (r Release) FindAsset(name string) (Asset, bool) {
	for _, a := range r.Assets {
		if a.Name == name {
			return a, true
		}
	}
	return Asset{}, false
}

// ParseChecksums parses a sha256sum-style file ("<hex>  <name>" per line) into name -> hex digest.
func ParseChecksums(r io.Reader) (map[string]string, error) {
	out := map[string]string{}
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) != 2 || len(f[0]) != 64 {
			continue
		}
		out[strings.TrimPrefix(f[1], "*")] = strings.ToLower(f[0])
	}
	return out, sc.Err()
}

// VerifyChecksum checks data against the digest listed for name in a SHA256SUMS body.
// A missing entry is an error: an unverifiable download is never installed.
func VerifyChecksum(sums []byte, name string, data []byte) error {
	m, err := ParseChecksums(strings.NewReader(string(sums)))
	if err != nil {
		return err
	}
	want, ok := m[name]
	if !ok {
		return fmt.Errorf("%s has no entry for %s; refusing to install", checksumFile, name)
	}
	sum := sha256.Sum256(data)
	if got := hex.EncodeToString(sum[:]); got != want {
		return fmt.Errorf("checksum mismatch for %s (want %s, got %s)", name, want, got)
	}
	return nil
}

// Download fetches the tarball for this platform and the release's SHA256SUMS, verifies the
// checksum and returns the tarball bytes.
func (c *Client) Download(ctx context.Context, rel Release, goos, goarch string) ([]byte, error) {
	name := AssetName(rel.Version(), goos, goarch)
	asset, ok := rel.FindAsset(name)
	if !ok {
		return nil, fmt.Errorf("release %s has no file %s for %s/%s", rel.Tag, name, goos, goarch)
	}
	sumAsset, ok := rel.FindAsset(checksumFile)
	if !ok {
		return nil, fmt.Errorf("release %s has no %s; refusing to install", rel.Tag, checksumFile)
	}
	sums, err := c.get(ctx, sumAsset.URL, 1<<20)
	if err != nil {
		return nil, err
	}
	data, err := c.get(ctx, asset.URL, maxAsset)
	if err != nil {
		return nil, err
	}
	if err := VerifyChecksum(sums, name, data); err != nil {
		return nil, err
	}
	return data, nil
}

// archiveEntries walks a release tarball (one top-level directory, which is stripped) and calls f
// for every regular file and directory with its cleaned relative path. It refuses paths that escape
// the tree and anything that is not a plain file or directory (links are skipped).
func archiveEntries(tgz []byte, f func(rel string, h *tar.Header, r io.Reader) error) error {
	zr, err := gzip.NewReader(bytes.NewReader(tgz))
	if err != nil {
		return err
	}
	tr := tar.NewReader(zr)
	var total int64
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		for _, el := range strings.Split(h.Name, "/") {
			if el == ".." {
				return fmt.Errorf("unsafe path %q in the release archive", h.Name)
			}
		}
		name := path.Clean(strings.TrimPrefix(h.Name, "./"))
		parts := strings.SplitN(name, "/", 2)
		if len(parts) < 2 { // the top-level directory itself
			continue
		}
		rel := parts[1]
		if rel == "" || rel == "." || strings.HasPrefix(rel, "../") || rel == ".." || path.IsAbs(rel) || strings.Contains(rel, "\x00") {
			return fmt.Errorf("unsafe path %q in the release archive", h.Name)
		}
		if h.Typeflag != tar.TypeReg && h.Typeflag != tar.TypeDir {
			continue
		}
		if h.Size > maxFile {
			return fmt.Errorf("%s in the archive is too large", rel)
		}
		total += h.Size
		if total > maxTree {
			return errors.New("the release archive is too large")
		}
		if err := f(rel, h, tr); err != nil {
			return err
		}
	}
}

// ExtractFile returns the regular file rel (e.g. "bin/gtf") from a release tarball.
func ExtractFile(tgz []byte, rel string) ([]byte, error) {
	var out []byte
	found := false
	err := archiveEntries(tgz, func(p string, h *tar.Header, r io.Reader) error {
		if h.Typeflag != tar.TypeReg || p != rel {
			return nil
		}
		b, err := io.ReadAll(io.LimitReader(r, maxFile+1))
		if err != nil {
			return err
		}
		out, found = b, true
		return nil
	})
	if err != nil {
		return nil, err
	}
	if !found || len(out) == 0 {
		return nil, fmt.Errorf("%s not found in the release archive", rel)
	}
	return out, nil
}

// ExtractTree writes everything under prefix (e.g. "lib/gtf") of the archive into dst, which is
// created. Files keep the executable bit; everything else is 0644/0755.
func ExtractTree(tgz []byte, prefix, dst string) error {
	prefix = strings.Trim(prefix, "/") + "/"
	n := 0
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	err := archiveEntries(tgz, func(p string, h *tar.Header, r io.Reader) error {
		if !strings.HasPrefix(p+"/", prefix) {
			return nil
		}
		target := filepath.Join(dst, filepath.FromSlash(strings.TrimPrefix(p, strings.TrimSuffix(prefix, "/"))))
		if h.Typeflag == tar.TypeDir {
			return os.MkdirAll(target, 0o755)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		mode := os.FileMode(0o644)
		if h.Mode&0o111 != 0 {
			mode = 0o755
		}
		f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
		if err != nil {
			return err
		}
		if _, err := io.Copy(f, io.LimitReader(r, maxFile+1)); err != nil {
			f.Close()
			return err
		}
		n++
		return f.Close()
	})
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("%s not found in the release archive", prefix)
	}
	return nil
}

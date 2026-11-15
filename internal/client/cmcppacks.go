// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package client

// cmcppacks.go delivers layout packs (spec 8, 16.6): the shared fetch package does the hashing,
// origin pinning, inline reassembly, caching and signature checks; this file asks the player
// (consent, spec 8.8), remembers the answers and hands verified bytes to pack.Parse.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/itsnotyoutoday/cmcp/go/fetch"
	"github.com/itsnotyoutoday/cmcp/go/frame"
	"github.com/itsnotyoutoday/cmcp/go/msg"
	"github.com/itsnotyoutoday/cmcp/go/pack"
	"github.com/itsnotyoutoday/cmcp/go/session"
	"github.com/itsnotyoutoday/cmcp/go/theme"
	"github.com/itsnotyoutoday/gtf/internal/cmcpui"
	"github.com/itsnotyoutoday/gtfparser"
)

// cmcpPacks is the client-wide pack machinery: one cache and one list of decisions for all worlds.
type cmcpPacks struct {
	mu      sync.Mutex
	cache   *fetch.Cache
	keyring *fetch.Keyring
	consent map[string]string // "host\x00packid" -> "allow" | "deny"
	file    string
}

func (c *Client) packStore() *cmcpPacks {
	if c.cmPacks != nil {
		return c.cmPacks
	}
	dir := filepath.Join(c.cfg.HomeDir, ".gtf", "cmcp")
	p := &cmcpPacks{keyring: fetch.NewKeyring(), consent: map[string]string{}, file: filepath.Join(dir, "consent.json")}
	cache, err := fetch.OpenCache(filepath.Join(dir, "packs"), fetch.DefaultCacheBytes)
	if err != nil {
		cache, _ = fetch.OpenCache("", fetch.DefaultCacheBytes) // memory only: it still works
	}
	p.cache = cache
	var disk struct {
		Consent map[string]string `json:"consent"`
		TOFU    map[string]string `json:"tofu"`
	}
	if b, err := os.ReadFile(p.file); err == nil && json.Unmarshal(b, &disk) == nil {
		if disk.Consent != nil {
			p.consent = disk.Consent
		}
		p.keyring.Load(disk.TOFU)
	}
	c.cmPacks = p
	return p
}

func (p *cmcpPacks) save() {
	b, _ := json.MarshalIndent(map[string]any{"consent": p.consent, "tofu": p.keyring.Records()}, "", " ")
	os.MkdirAll(filepath.Dir(p.file), 0o700)
	os.WriteFile(p.file, b, 0o600)
}

func consentKey(host, id string) string { return strings.ToLower(host) + "\x00" + id }

// packJob is one pack a session wants.
type packJob struct {
	ref    msg.PackRef
	state  string // asking | fetching | ready | refused | failed
	inline *fetch.Inline
	why    string
	ship   string // the ship whose announcement asked for it: a question outlives neither the ship nor the pack's need
}

// consentMode is %cmcp_pack_consent: always (default: packs and themes announced by the game's
// origin are taken; every check stays), ask (the F5/F6 question) or never.
func (c *Client) consentMode() string {
	switch v := strings.ToLower(strings.TrimSpace(c.in.Var("cmcp_pack_consent"))); v {
	case "ask", "never":
		return v
	}
	return "always"
}

// fullRef completes an announced identity with the entry the server listed (size, url, sig).
func fullRef(u *session.UI, id *msg.PackIdent) msg.PackRef {
	for _, p := range u.Ship.Packs {
		if p.SHA256 == id.SHA256 {
			return p
		}
	}
	if u.Welcome != nil {
		for _, p := range u.Welcome.Packs {
			if p.SHA256 == id.SHA256 {
				return p
			}
		}
	}
	return msg.PackRef{ID: id.ID, Version: id.Version, SHA256: id.SHA256, Kind: "layout"}
}

// cmcpEnsurePacks starts getting the pack the console's screen names when it is not usable yet.
func (c *Client) cmcpEnsurePacks(s *Session) {
	if s.cm == nil {
		return
	}
	u := s.cm.UI()
	cs := s.cmScr()
	id := u.Screen.PackWanted
	if id == nil || !u.Screen.Fallback {
		return
	}
	if j := cs.jobs[id.SHA256]; j != nil {
		return
	}
	ref := fullRef(u, id)
	if ref.Kind != "" && ref.Kind != "layout" {
		return
	}
	j := &packJob{ref: ref}
	if cs.jobs == nil {
		cs.jobs = map[string]*packJob{}
	}
	cs.jobs[ref.SHA256] = j
	c.packStart(s, j)
}

func (c *Client) packStart(s *Session, j *packJob) {
	ps := c.packStore()
	host := s.World.Host
	trust := ps.keyring.Check(host, j.ref)
	if trust == fetch.TrustInvalid {
		c.packFailed(s, j, "invalid signature")
		return
	}
	mode := c.consentMode()
	dec := ps.consent[consentKey(host, j.ref.ID)]
	switch {
	case mode == "never" || dec == "deny":
		j.state, j.why = "refused", "refused"
		c.cmcpDebug("pack %s refused", j.ref.ID)
		return
	case mode == "always" || dec == "allow" || !fetch.NeedsPrompt(trust):
		if mode == "always" && dec != "allow" {
			kind := "interface pack"
			if j.ref.Kind == "theme" {
				kind = "interface theme"
			}
			c.echoTo(s.scr, "", fmt.Sprintf("%% Accepted %s %s %s from %s", kind, j.ref.ID, j.ref.Version, host))
		}
		c.packFetch(s, j)
		return
	}
	j.state = "asking"
	kind := "layout pack"
	if j.ref.Kind == "theme" {
		kind = "theme pack"
	}
	line := fmt.Sprintf("%% %s offers a %s %q %s (%d KiB). Use it? /cmcp pack allow | deny   (or F5 / F6)",
		host, kind, j.ref.ID, j.ref.Version, (j.ref.Size+1023)/1024)
	c.echoTo(s.scr, "", line)
}

// packDecide answers a consent prompt (id "" = the one pending).
func (c *Client) packDecide(s *Session, id string, allow bool) bool {
	cs := s.cmScr()
	for _, j := range cs.jobs {
		if j.state != "asking" || (id != "" && j.ref.ID != id) {
			continue
		}
		ps := c.packStore()
		k := consentKey(s.World.Host, j.ref.ID)
		if allow {
			ps.consent[k] = "allow"
			ps.save()
			c.packFetch(s, j)
		} else {
			ps.consent[k] = "deny"
			ps.save()
			j.state = "refused"
		}
		c.scheduleRender()
		return true
	}
	return false
}

func (c *Client) packFailed(s *Session, j *packJob, why string) {
	j.state, j.why = "failed", why
	if j.ref.Kind == "theme" {
		s.cmScr().reg.NoteFailed(j.ref.ID)
	}
	s.cm.PackFailed(c.cfg.Now(), j.ref, why)
	c.cmcpDebug("pack %s: %s (staying on the built-in screens)", j.ref.ID, why)
	if s.cm != nil {
		s.cm.PackAvailable(j.ref.ID, j.ref.SHA256, false)
	}
}

// packFetch gets the bytes: cache, then inline chunks, then HTTPS from the announced origin.
func (c *Client) packFetch(s *Session, j *packJob) {
	ps := c.packStore()
	host := s.World.Host
	if raw, _, ok := ps.cache.Get(host, j.ref.SHA256); ok {
		c.packInstall(s, j, raw, false)
		return
	}
	j.state = "fetching"
	s.cm.PackProgress(c.cfg.Now(), j.ref, 0, int64(j.ref.Size))
	u := s.cm.UI()
	switch {
	case j.ref.Inline && s.conn != nil:
		in, err := fetch.NewInline(j.ref, c.cfg.Now(), 0)
		if err != nil {
			c.packFailed(s, j, err.Error())
			return
		}
		j.inline = in
		s.conn.SendProto("gmcp", msg.PkgPackGet, msg.PackGet{ID: j.ref.ID, Version: j.ref.Version, SHA256: j.ref.SHA256})
	case u.Welcome != nil && u.Welcome.Assets != nil && u.Welcome.Assets.Origin != "":
		a := u.Welcome.Assets
		pol := fetch.Policy{GameHost: host, AllowedOrigins: map[string]bool{}, DevTrustLoopbackTLS: onVar(c.in.Var("cmcp_dev_tls")),
			// developer-only (%cmcp_dev_loopback_http, default off): an http:// origin is accepted only when it is
			// loopback, equal to the one THIS connection's Welcome announced, and the game host is loopback too
			DevAllowLoopbackHTTP: onVar(c.in.Var("cmcp_dev_loopback_http")), Announced: a.Origin}
		for _, o := range strings.Fields(c.in.Var("cmcp_origins")) {
			pol.AllowedOrigins[strings.ToLower(o)] = true
		}
		f := fetch.NewFetcher(pol)
		blob := fetch.BlobFor(fetch.Origin{Origin: a.Origin, Base: a.Base}, j.ref)
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), fetch.PackTotal)
			defer cancel()
			raw, err := f.Fetch(ctx, blob)
			c.Post(func(*gtfparser.Interp) {
				if err != nil {
					c.packFailed(s, j, err.Error())
				} else {
					c.packInstall(s, j, raw, true)
				}
				c.afterTask()
			})
		}()
	default:
		c.packFailed(s, j, "no way to fetch (not inline, no origin)")
	}
}

// cmcpPackFrame takes the pack transfer messages (the session core only notes them).
func (c *Client) cmcpPackFrame(s *Session, f frame.Frame) {
	cs := s.cmScr()
	switch f.Package {
	case msg.PkgPackData:
		v, err := msg.Decode(f.Package, f.Payload)
		d, ok := v.(*msg.PackData)
		if err != nil || !ok {
			return
		}
		j := cs.jobs[d.SHA256]
		if j == nil || j.inline == nil {
			return
		}
		raw, err := j.inline.Add(c.cfg.Now(), d)
		if err != nil {
			s.conn.SendProto("gmcp", msg.PkgPackCancel, j.inline.Cancel())
			c.packFailed(s, j, err.Error())
			return
		}
		if raw != nil {
			j.inline = nil
			c.packInstall(s, j, raw, true)
		}
	case msg.PkgPackFail:
		v, _ := msg.Decode(f.Package, f.Payload)
		if pf, ok := v.(*msg.PackFail); ok {
			if j := cs.jobs[pf.SHA256]; j != nil {
				c.packFailed(s, j, "server: "+pf.Err)
			}
		}
	}
}

// packInstall validates (whole or nothing) and activates a pack whose bytes are verified.
func (c *Client) packInstall(s *Session, j *packJob, raw []byte, store bool) {
	ps := c.packStore()
	host := s.World.Host
	opt := pack.Options{}
	if u := s.cm.UI(); u.Catalogue != nil {
		opt.Commands = map[string]struct{}{}
		for n := range u.Catalogue {
			opt.Commands[n] = struct{}{}
		}
	}
	if j.ref.Kind == "theme" {
		c.themeInstall(s, j, raw, store)
		return
	}
	p, err := pack.Parse(raw, opt)
	if err != nil {
		ps.cache.Remove(host, j.ref.SHA256)
		c.packFailed(s, j, err.Error())
		return
	}
	if store {
		ps.cache.Put(host, fetch.Meta{ID: j.ref.ID, Version: j.ref.Version, Kind: "layout", SHA256: j.ref.SHA256, Size: len(raw), Fetched: c.cfg.Now(), Host: host}, raw)
	}
	ps.keyring.Accept(host, j.ref)
	ps.save()
	s.cmScr().packs[j.ref.ID] = p
	j.state = "ready"
	s.cm.PackDownloaded(c.cfg.Now(), j.ref)
	s.cm.PackAvailable(j.ref.ID, j.ref.SHA256, true)
	c.cmcpPump(s)
	c.scheduleRender()
}

// cmcpPacksList is /cmcp packs.
func (c *Client) cmcpPacksList(s *Session) []string {
	out := []string{fmt.Sprintf("%% consent mode: %s; cache: %d KiB", c.consentMode(), c.packStore().cache.Size()/1024)}
	if s == nil || s.cs == nil {
		return append(out, "% no packs for this world")
	}
	for _, j := range s.cs.jobs {
		out = append(out, fmt.Sprintf("%% %s %s  %s  (%s)", j.ref.ID, j.ref.Version, j.state, j.why))
	}
	for id := range s.cs.packs {
		out = append(out, "% active: "+id)
	}
	return out
}

var _ = time.Second

// cmcpPackWanted starts getting a pack the core announced as wanted (the active ship, screen or context
// names it, or WantPack asked): layout packs only; sound and theme packs are not used by gtf.
func (c *Client) cmcpPackWanted(s *Session, ref msg.PackRef) {
	if ref.Kind != "" && ref.Kind != "layout" && ref.Kind != "theme" {
		return
	}
	cs := s.cmScr()
	if cs.jobs == nil {
		cs.jobs = map[string]*packJob{}
	}
	if cs.jobs[ref.SHA256] != nil {
		return
	}
	j := &packJob{ref: ref, ship: s.cm.UI().Ship.ID}
	cs.jobs[ref.SHA256] = j
	c.packStart(s, j)
}

// themeInstall validates a downloaded theme pack (theme.Parse: schema, limits, the cell rendition's
// floors) and keeps it for the session; a pack with no cell part is not selectable by a cell client
// (spec 19.8), so selection falls back to the built-in theme of the same id, then plain.
func (c *Client) themeInstall(s *Session, j *packJob, raw []byte, store bool) {
	ps := c.packStore()
	host := s.World.Host
	tp, err := theme.Parse(raw)
	if err != nil {
		ps.cache.Remove(host, j.ref.SHA256)
		c.packFailed(s, j, "theme: "+err.Error())
		return
	}
	if store {
		ps.cache.Put(host, fetch.Meta{ID: j.ref.ID, Version: j.ref.Version, Kind: "theme", SHA256: j.ref.SHA256, Size: len(raw), Fetched: c.cfg.Now(), Host: host}, raw)
	}
	ps.keyring.Accept(host, j.ref)
	ps.save()
	cs := s.cmScr()
	if cs.themes == nil {
		cs.themes = map[string]*theme.Pack{}
	}
	if old := cs.themes[tp.ID]; old == nil || semverGT(tp.Version, old.Version) {
		cs.themes[tp.ID] = tp
	}
	cs.reg.NoteVerified(tp.ID, tp.Version, tp.Parts)
	j.state = "ready"
	s.cm.PackDownloaded(c.cfg.Now(), j.ref)
	c.scheduleRender()
}

// semverParts splits "1.10.0-rc.1" into numbers and the pre-release identifiers.
func semverParts(v string) (nums [3]int, pre []string) {
	core, rest, _ := strings.Cut(v, "-")
	for i, x := range strings.SplitN(core, ".", 3) {
		n, _ := strconv.Atoi(x)
		nums[i] = n
	}
	if rest != "" {
		pre = strings.Split(rest, ".")
	}
	return
}

// semverGT reports a > b by semver 2.0 precedence (spec 19.10): numeric major.minor.patch, a
// pre-release below its release, identifiers left to right (numeric below alphanumeric).
func semverGT(a, b string) bool {
	an, ap := semverParts(a)
	bn, bp := semverParts(b)
	for i := range an {
		if an[i] != bn[i] {
			return an[i] > bn[i]
		}
	}
	switch {
	case len(ap) == 0 && len(bp) == 0:
		return false
	case len(ap) == 0:
		return true
	case len(bp) == 0:
		return false
	}
	for i := 0; i < len(ap) && i < len(bp); i++ {
		x, ex := strconv.Atoi(ap[i])
		y, ey := strconv.Atoi(bp[i])
		switch {
		case ex == nil && ey == nil && x != y:
			return x > y
		case ex == nil && ey != nil:
			return false
		case ex != nil && ey == nil:
			return true
		case ex != nil && ey != nil && ap[i] != bp[i]:
			return ap[i] > bp[i]
		}
	}
	return len(ap) > len(bp)
}

// cmcpResolveTheme applies the fallback chain of spec 19.10 to a selected theme id: a downloaded pack
// (verified, consented, with a cell part) when it beats the embedded copy (higher version, or
// %cmcp_dev_force_theme_download), else the embedded theme of that id, else plain. It asks the core to
// fetch the announced pack the first time the selection needs it.
func (c *Client) cmcpResolveTheme(s *Session, u *session.UI, id string) cmcpui.Theme {
	cs := s.cmScr()
	force := onVar(c.in.Var("cmcp_dev_force_theme_download"))
	cs.reg.SetForce(force)
	if u.Welcome != nil {
		for _, p := range u.Welcome.Packs {
			if p.Kind != "theme" || !strings.EqualFold(p.ID, id) || cs.themes[id] != nil || cs.themeAsked[id] {
				continue
			}
			// the selected theme is announced: fetch it when it beats the embedded copy (higher version,
			// or %cmcp_dev_force_theme_download); only the cell rendition is requested (spec 19.1)
			if b := theme.Builtin(id); b == nil || force || semverGT(p.Version, b.Version) {
				if cs.themeAsked == nil {
					cs.themeAsked = map[string]bool{}
				}
				cs.themeAsked[id] = true
				s.cm.WantTheme(id)
				c.cmcpPump(s)
			}
		}
	}
	want, src := cs.reg.Resolve(id)
	if src == theme.SourcePack {
		if dl := cs.themes[want]; dl != nil {
			return cmcpui.ThemeFromPack(dl)
		}
	}
	return cmcpui.ThemeByName(want)
}

// cmcpThemesList is /cmcp themes: the embedded themes, the announced theme packs and which one is used.
func (c *Client) cmcpThemesList(s *Session) []string {
	out := []string{"% embedded: " + strings.Join(theme.BuiltinIDs(), " ")}
	if s == nil || s.cm == nil {
		return out
	}
	u := s.cm.UI()
	cs := s.cmScr()
	if u.Welcome != nil {
		for _, p := range u.Welcome.Packs {
			if p.Kind == "theme" {
				st := "announced"
				if cs.themes[p.ID] != nil {
					st = "downloaded"
				}
				out = append(out, fmt.Sprintf("%% announced: %s %s (%s)", p.ID, p.Version, st))
			}
		}
	}
	id := c.cmcpThemeFor(s, u, "")
	src := "embedded"
	if dl := cs.themes[id]; dl != nil {
		src = "downloaded " + dl.Version
	} else if theme.Builtin(id) == nil {
		src = "plain (no such theme)"
	}
	out = append(out, "% active: "+id+" ("+src+"); %cmcp_theme_mode="+orStr(c.in.Var("cmcp_theme_mode"), "ship"))
	return out
}

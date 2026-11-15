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
	"strings"
	"sync"
	"time"

	"github.com/itsnotyoutoday/cmcp/fetch"
	"github.com/itsnotyoutoday/cmcp/frame"
	"github.com/itsnotyoutoday/cmcp/msg"
	"github.com/itsnotyoutoday/cmcp/pack"
	"github.com/itsnotyoutoday/cmcp/session"
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
}

// consentMode is %cmcp_pack_consent: ask (default), always or never.
func (c *Client) consentMode() string {
	switch v := strings.ToLower(strings.TrimSpace(c.in.Var("cmcp_pack_consent"))); v {
	case "always", "never":
		return v
	}
	return "ask"
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
		c.packFetch(s, j)
		return
	}
	j.state = "asking"
	line := fmt.Sprintf("%% %s offers a layout pack %q %s (%d KiB). Use it? /cmcp pack allow | deny   (screen keys: y / n)",
		host, j.ref.ID, j.ref.Version, (j.ref.Size+1023)/1024)
	c.echoTo(s.scr, "", line)
	c.cmcpNotice(s, "pack "+j.ref.ID+": y = allow, n = refuse")
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
		pol := fetch.Policy{GameHost: host, AllowedOrigins: map[string]bool{}, DevTrustLoopbackTLS: onVar(c.in.Var("cmcp_dev_tls"))}
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

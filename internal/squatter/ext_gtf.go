// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package squatter

// The gtf extension to SWP/TFSP ("shared control"; docs/SWP-GTF.md): negotiation, presence, typing
// indicators, author tagging, per-peer views and rate limits. A peer that does not announce the
// extension in its hello is served exactly as before.

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/itsnotyoutoday/gtf/internal/squatter/proto"
	"github.com/itsnotyoutoday/gtf/internal/squatter/wire"
)

// Extension identity and versions.
const (
	GTFName    = "gtf" // the key in hello.ext and welcome.ext
	GTFVersion = 1     // highest version spoken by this server
)

// Feature flags of the gtf extension, version 1.
const (
	FeatAuthor   = "author"   // echo lines carry "author"
	FeatPresence = "presence" // gtf.view, gtf.presence
	FeatTyping   = "typing"   // gtf.typing
	FeatNAWS     = "naws"     // resize is honoured; gtf.naws is sent
	FeatWorlds   = "worlds"   // world.put/delete/rename with revisions, world.changed
	// FeatMacros is reserved for macro/trigger/gag sync (the macro.* family, see docs/SWP-GTF.md,
	// "Reserved"). Nothing implements it: a server never announces it and ignores it in a hello.
	FeatMacros = "macros"
)

// Extra error codes of the extension.
const (
	ErrUnsupported  = "unsupported"   // a message gtf does not implement for this peer (world.* without the extension, tf.script.*)
	ErrConflict     = "conflict"      // optimistic concurrency: the base revision is stale (data = current record)
	ErrNotFound     = "not_found"     // no such world
	ErrExists       = "exists"        // rename target exists
	ErrPolicyDenied = "policy_denied" // reserved: refused by the owner's execution policy for peer-authored code
)

// Timing and limits (variables so tests can shorten them).
// timing holds the durations (atomic so tests can shorten them while timers of an earlier test run).
var timing struct{ typingTTL, typingMinGap, nawsDebounce atomic.Int64 }

func init() {
	timing.typingTTL.Store(int64(6 * time.Second)) // a typing indicator expires without a refresh
	timing.typingMinGap.Store(int64(800 * time.Millisecond))
	timing.nawsDebounce.Store(int64(300 * time.Millisecond))
}

func typingTTL() time.Duration    { return time.Duration(timing.typingTTL.Load()) }
func typingMinGap() time.Duration { return time.Duration(timing.typingMinGap.Load()) }
func nawsDebounce() time.Duration { return time.Duration(timing.nawsDebounce.Load()) }

const (
	maxPeerWidth = 1000
	minPeerWidth = 10
)

func gtfServerFeatures(cfg Config) []string {
	return []string{FeatAuthor, FeatPresence, FeatTyping, FeatNAWS, FeatWorlds}
}

// bucket is a token bucket.
type bucket struct {
	tokens, rate, burst float64
	last                time.Time
}

func (b *bucket) allow(now time.Time) bool {
	if b.last.IsZero() {
		b.tokens = b.burst
	} else {
		b.tokens += now.Sub(b.last).Seconds() * b.rate
		if b.tokens > b.burst {
			b.tokens = b.burst
		}
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// Rate classes (messages per second, burst).
var rateClasses = map[string][2]float64{
	"send": {20, 40}, "view": {10, 20}, "typing": {5, 5}, "resize": {5, 10}, "world": {5, 10},
}

// gtfPeer is the extension state of one connected peer (guarded by Squatter.pmu).
type gtfPeer struct {
	v        int
	features map[string]bool

	view       string // the session the peer displays ("" none)
	typing     string // the session it is typing into ("" not typing)
	typingGen  uint64
	typingTime *time.Timer
	lastTyping time.Time

	attached  map[string]bool
	widths    map[string]int // per session, from resize
	lastWidth int
	lastInput map[string]time.Time

	lim map[string]*bucket
}

func (g *gtfPeer) has(f string) bool { return g != nil && g.features[f] }

func (g *gtfPeer) allow(class string) bool {
	b := g.lim[class]
	if b == nil {
		c := rateClasses[class]
		b = &bucket{rate: c[0], burst: c[1]}
		g.lim[class] = b
	}
	return b.allow(time.Now())
}

// localState is gtf's own terminal user (guarded by Squatter.pmu).
type localState struct {
	view       string
	typing     string
	typingGen  uint64
	typingTime *time.Timer
	lastTyping time.Time
	cols, rows int
	lastInput  time.Time
}

// gtfOf returns p's extension state (nil for a plain peer). Caller must not hold pmu.
func (s *Squatter) gtfOf(p wire.Peer) *gtfPeer {
	s.pmu.Lock()
	defer s.pmu.Unlock()
	if pi := s.peers[p.ID()]; pi != nil {
		return pi.gtf
	}
	return nil
}

// parseAnnouncement reads what p announced: hello.ext.gtf, or the simple hello.offer list. ok=false
// means nothing valid was announced. offered maps feature -> the spelling used in the offer list
// (for hello.offer only, to answer in the same spelling).
func parseAnnouncement(ex wire.Extender) (v, minV int, feats []string, offered map[string]string, ok bool) {
	if raw := ex.Ext(GTFName); raw != nil {
		var h struct {
			V        int      `json:"v"`
			MinV     int      `json:"minV"`
			Features []string `json:"features"`
		}
		if json.Unmarshal(raw, &h) != nil || h.V < 1 {
			return 0, 0, nil, nil, false
		}
		return h.V, h.MinV, h.Features, nil, true
	}
	offer := ex.Offer()
	offered = map[string]string{}
	named := false
	var bare []string
	for _, n := range offer {
		switch {
		case n == GTFName:
			named = true
			offered[GTFName] = n
		case strings.HasPrefix(n, GTFName+"."):
			f := strings.TrimPrefix(n, GTFName+".")
			offered[f] = n
			feats = append(feats, f)
		default:
			bare = append(bare, n)
		}
	}
	if !named {
		return 0, 0, nil, nil, false
	}
	if len(feats) == 0 { // "gtf" alone (or with bare feature names): those names, else every feature
		for _, n := range bare {
			offered[n] = n
			feats = append(feats, n)
		}
		if len(feats) == 0 {
			feats = gtfServerFeatures(Config{}) // answered as "gtf" only; the list is in ext.gtf.features
		}
	}
	return 1, 0, feats, offered, true
}

// WelcomeExt implements wire.ExtWelcomer: it negotiates the extension for p and returns the
// welcome's "ext" member (nil = nothing to say, the plain welcome) and "negotiated" list.
func (b *backend) WelcomeExt(p wire.Peer) (map[string]any, []string) {
	s := b.s
	cfg := s.Config()
	ex, ok := p.(wire.Extender)
	if !ok || !cfg.GTF || cfg.Kind != KindTF {
		return nil, nil
	}
	hv, minV, announced, offered, ok := parseAnnouncement(ex)
	if !ok {
		return nil, nil // nothing, or not a valid announcement: behave as if there was none
	}
	if minV > GTFVersion {
		return map[string]any{GTFName: map[string]any{"v": 0, "error": "version_mismatch", "serverV": GTFVersion}}, nil
	}
	server := gtfServerFeatures(cfg)
	isServer := map[string]bool{}
	for _, f := range server {
		isServer[f] = true
	}
	agreed := map[string]bool{}
	var list []string
	for _, f := range announced {
		if isServer[f] && !agreed[f] {
			agreed[f] = true
			list = append(list, f)
		}
	}
	sort.Strings(list)
	if list == nil {
		list = []string{}
	}
	v := min(hv, GTFVersion)
	g := &gtfPeer{v: v, features: agreed, attached: map[string]bool{}, widths: map[string]int{}, lastInput: map[string]time.Time{}, lim: map[string]*bucket{}}
	s.pmu.Lock()
	pi := s.peers[p.ID()]
	if pi != nil {
		pi.gtf = g
	}
	s.pmu.Unlock()
	if pi == nil {
		return nil, nil
	}
	s.sendPresence(p)
	s.broadcastPresence(p.ID())
	var neg []string
	if offered != nil {
		neg = append(neg, offered[GTFName])
		for _, f := range list {
			if sp, ok := offered[f]; ok {
				neg = append(neg, sp)
			}
		}
	}
	return map[string]any{GTFName: map[string]any{
		"v": v, "serverV": GTFVersion, "features": list, "serverFeatures": server,
		"cid": cidOf(p), "naws": cfg.NAWS,
		"limits": map[string]any{"typingTtlMs": typingTTL().Milliseconds(), "typingMinGapMs": typingMinGap().Milliseconds(),
			"nawsDebounceMs": nawsDebounce().Milliseconds(), "minCols": minPeerWidth, "maxCols": maxPeerWidth},
	}}, neg
}

func cidOf(p wire.Peer) string { return strconv.FormatUint(p.ID(), 10) }

// ---------------------------------------------------------------- presence

type presEntry struct {
	CID    string `json:"cid"`
	Device string `json:"device,omitempty"`
	Name   string `json:"name"`
	Role   string `json:"role"`
	GTF    bool   `json:"gtf,omitempty"`
	Local  bool   `json:"local,omitempty"`
	View   string `json:"view"`
	Typing string `json:"typing"`
	Since  int64  `json:"since,omitempty"`
}

func (s *Squatter) presenceLocked() []presEntry {
	out := []presEntry{{CID: "local", Name: "local", Role: "local", Local: true, GTF: true, View: s.local.view, Typing: s.local.typing}}
	ids := make([]uint64, 0, len(s.peers))
	for id := range s.peers {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		pi := s.peers[id]
		e := presEntry{CID: strconv.FormatUint(id, 10), Device: pi.p.DeviceID(), Name: pi.p.DeviceName(), Role: pi.role, Since: pi.joined.UnixMilli()}
		if g := pi.gtf; g != nil {
			e.GTF, e.View, e.Typing = true, g.view, g.typing
		}
		out = append(out, e)
	}
	return out
}

func presenceMsg(peers []presEntry) map[string]any {
	return map[string]any{"v": proto.Version, "t": "gtf.presence", "peers": peers, "ts": nowMs()}
}

// presenceTargets are the peers that asked for presence; except is a peer id to skip (0 = none).
func (s *Squatter) presenceTargets(feature string, except uint64) []wire.Peer {
	s.pmu.Lock()
	defer s.pmu.Unlock()
	var out []wire.Peer
	for id, pi := range s.peers {
		if id != except && pi.gtf.has(feature) {
			out = append(out, pi.p)
		}
	}
	return out
}

// sendPresence sends the full snapshot to one peer.
func (s *Squatter) sendPresence(p wire.Peer) {
	s.pmu.Lock()
	msg := presenceMsg(s.presenceLocked())
	s.pmu.Unlock()
	p.Send(msg)
}

// broadcastPresence sends the snapshot to every presence peer except the one with id except.
func (s *Squatter) broadcastPresence(except uint64) {
	targets := s.presenceTargets(FeatPresence, except)
	if len(targets) == 0 {
		return
	}
	s.pmu.Lock()
	msg := presenceMsg(s.presenceLocked())
	s.pmu.Unlock()
	for _, p := range targets {
		p.Send(msg)
	}
}

func (s *Squatter) broadcastTyping(cid, name, session string, on bool, except uint64) {
	msg := map[string]any{"v": proto.Version, "t": "gtf.typing", "cid": cid, "name": name, "session": session, "typing": on}
	for _, p := range s.presenceTargets(FeatTyping, except) {
		p.Send(msg)
	}
}

// Presence returns the snapshot (for /squatter who and tests).
func (s *Squatter) Presence() []PresenceInfo {
	s.pmu.Lock()
	defer s.pmu.Unlock()
	var out []PresenceInfo
	for _, e := range s.presenceLocked() {
		out = append(out, PresenceInfo{CID: e.CID, Name: e.Name, Role: e.Role, Ext: e.GTF, Local: e.Local, View: e.View, Typing: e.Typing})
	}
	return out
}

// PresenceInfo is one attached client as /squatter who shows it.
type PresenceInfo struct {
	CID, Name, Role string
	Ext, Local      bool
	View, Typing    string // session ids
}

// isLocalView reports whether session is the one gtf's own terminal shows (tf.fg). Callers may hold
// a session's lock.
func (s *Squatter) isLocalView(session string) bool {
	s.pmu.Lock()
	defer s.pmu.Unlock()
	return s.local.view == session && session != ""
}

// LocalView tells the squatter which session gtf's own terminal shows ("" none). Engine goroutine.
func (s *Squatter) LocalView(session string) {
	if !s.active.Load() {
		return
	}
	s.pmu.Lock()
	old := s.local.view
	s.local.view = session
	s.pmu.Unlock()
	if old != session {
		s.broadcastPresence(0)
		// tf.fg of the two sessions changed
		for _, id := range []string{old, session} {
			if ss := s.session(id); ss != nil {
				ss.mu.Lock()
				ss.broadcastLocked()
				ss.mu.Unlock()
			}
		}
	}
}

// LocalTyping tells the squatter whether the local user is typing into session. Engine goroutine.
func (s *Squatter) LocalTyping(session string, on bool) {
	if !s.active.Load() {
		return
	}
	s.pmu.Lock()
	cur := s.local.typing
	switch {
	case on && cur == session:
		// still typing: keep the indicator alive (no broadcast; peers keep it until it expires)
		if now := time.Now(); now.Sub(s.local.lastTyping) > typingMinGap() {
			s.local.lastTyping = now
			s.local.typingGen++
			gen := s.local.typingGen
			if s.local.typingTime != nil {
				s.local.typingTime.Stop()
			}
			s.local.typingTime = time.AfterFunc(typingTTL(), func() { s.expireLocalTyping(gen) })
		}
		s.pmu.Unlock()
		return
	case !on && cur == "":
		s.pmu.Unlock()
		return
	}
	s.local.typingGen++
	gen := s.local.typingGen
	if s.local.typingTime != nil {
		s.local.typingTime.Stop()
	}
	if on {
		s.local.typing = session
		s.local.typingTime = time.AfterFunc(typingTTL(), func() { s.expireLocalTyping(gen) })
	} else {
		s.local.typing = ""
	}
	s.pmu.Unlock()
	s.broadcastTyping("local", "local", firstNonEmptyStr(session, cur), on, 0)
}

func firstNonEmptyStr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func (s *Squatter) expireLocalTyping(gen uint64) {
	s.pmu.Lock()
	if s.local.typingGen != gen || s.local.typing == "" {
		s.pmu.Unlock()
		return
	}
	sess := s.local.typing
	s.local.typing = ""
	s.pmu.Unlock()
	s.broadcastTyping("local", "local", sess, false, 0)
}

// LocalInput records that the local user submitted a line: it makes the terminal the "active"
// client for the NAWS policy and ends the typing indicator. Engine goroutine.
func (s *Squatter) LocalInput() {
	if !s.active.Load() {
		return
	}
	s.pmu.Lock()
	s.local.lastInput = time.Now()
	s.pmu.Unlock()
	s.LocalTyping("", false)
	s.nawsTouch()
}

// ---------------------------------------------------------------- per-peer messages

type viewReq struct {
	Session string `json:"session"`
}

type typingReq struct {
	Session string `json:"session"`
	Typing  bool   `json:"typing"`
}

// handleGTF serves the gtf.* messages. handled=false means it is not one of them.
func (b *backend) handleGTF(p wire.Peer, g *gtfPeer, t string, raw []byte) (bool, error) {
	s := b.s
	switch t {
	case "gtf.view":
		if !g.has(FeatPresence) {
			return true, wire.Errf(proto.ErrBadRequest, "gtf.view needs the presence feature")
		}
		var r viewReq
		if json.Unmarshal(raw, &r) != nil || len(r.Session) > 128 {
			return true, wire.Errf(proto.ErrBadRequest, "bad gtf.view")
		}
		if r.Session != "" && s.session(r.Session) == nil {
			return true, wire.Errf(proto.ErrNoSuchSession, "no such session")
		}
		s.pmu.Lock()
		ok := g.allow("view")
		changed := ok && g.view != r.Session
		if changed {
			g.view = r.Session
		}
		s.pmu.Unlock()
		if !ok {
			return true, wire.Errf(proto.ErrRateLimited, "too many gtf.view messages")
		}
		if changed {
			s.broadcastPresence(0)
		}
		return true, nil
	case "gtf.typing":
		if !g.has(FeatTyping) {
			return true, wire.Errf(proto.ErrBadRequest, "gtf.typing needs the typing feature")
		}
		if err := need(p, rankOperator, "typing"); err != nil {
			return true, err
		}
		var r typingReq
		if json.Unmarshal(raw, &r) != nil || r.Session == "" || len(r.Session) > 128 {
			return true, wire.Errf(proto.ErrBadRequest, "bad gtf.typing")
		}
		if s.session(r.Session) == nil {
			return true, wire.Errf(proto.ErrNoSuchSession, "no such session")
		}
		s.setPeerTyping(p, g, r.Session, r.Typing)
		return true, nil
	case "gtf.presence.get":
		if !g.has(FeatPresence) {
			return true, wire.Errf(proto.ErrBadRequest, "needs the presence feature")
		}
		s.sendPresence(p)
		return true, nil
	}
	return false, nil
}

// setPeerTyping applies a typing on/off from a peer. Updates faster than typingMinGap() are dropped
// silently (a client refreshes a still-typing indicator every few seconds); an indicator that is
// not refreshed within typingTTL() expires.
func (s *Squatter) setPeerTyping(p wire.Peer, g *gtfPeer, session string, on bool) {
	now := time.Now()
	s.pmu.Lock()
	if on && g.typing == session && now.Sub(g.lastTyping) < typingMinGap() {
		s.pmu.Unlock()
		return
	}
	if !g.allow("typing") {
		s.pmu.Unlock()
		return
	}
	prev := g.typing
	g.lastTyping = now
	g.typingGen++
	gen := g.typingGen
	if g.typingTime != nil {
		g.typingTime.Stop()
		g.typingTime = nil
	}
	if on {
		g.typing = session
		g.typingTime = time.AfterFunc(typingTTL(), func() { s.expirePeerTyping(p, g, gen) })
	} else {
		g.typing = ""
	}
	changed := (on && prev != session) || (!on && prev != "")
	s.pmu.Unlock()
	if changed {
		s.broadcastTyping(cidOf(p), p.DeviceName(), session, on, p.ID())
	}
}

func (s *Squatter) expirePeerTyping(p wire.Peer, g *gtfPeer, gen uint64) {
	s.pmu.Lock()
	if g.typingGen != gen || g.typing == "" {
		s.pmu.Unlock()
		return
	}
	sess := g.typing
	g.typing = ""
	s.pmu.Unlock()
	s.broadcastTyping(cidOf(p), p.DeviceName(), sess, false, p.ID())
}

// noteInput records that p sent text into session: typing ends, the peer becomes the "active"
// client for the NAWS policy.
func (s *Squatter) noteInput(p wire.Peer, g *gtfPeer, session string) {
	if g == nil {
		return
	}
	s.pmu.Lock()
	g.lastInput[session] = time.Now()
	was := g.typing
	if g.typingTime != nil {
		g.typingTime.Stop()
		g.typingTime = nil
	}
	g.typingGen++
	g.typing = ""
	s.pmu.Unlock()
	if was != "" {
		s.broadcastTyping(cidOf(p), p.DeviceName(), was, false, p.ID())
	}
	s.nawsTouch(session)
}

// peerGone removes a leaving peer's extension state and tells the others.
func (s *Squatter) peerGone(g *gtfPeer, sessions []string) {
	if g == nil {
		return
	}
	s.pmu.Lock()
	if g.typingTime != nil {
		g.typingTime.Stop()
	}
	g.typingGen++
	s.pmu.Unlock()
	s.broadcastPresence(0)
	s.nawsTouch(sessions...)
}

// setAttached keeps the peer's set of attached sessions (for the NAWS candidates).
func (s *Squatter) setAttached(p wire.Peer, session string, on bool) {
	s.pmu.Lock()
	g := (*gtfPeer)(nil)
	if pi := s.peers[p.ID()]; pi != nil {
		g = pi.gtf
	}
	if g != nil {
		if on {
			g.attached[session] = true
		} else {
			delete(g.attached, session)
		}
	}
	s.pmu.Unlock()
	if g != nil {
		s.nawsTouch(session)
	}
}

// ---------------------------------------------------------------- resize (NAWS input)

// resizeFrom handles a resize message from an extension peer.
func (b *backend) resizeFrom(p wire.Peer, g *gtfPeer, session string, cols int) error {
	s := b.s
	if err := need(p, rankOperator, "resizing"); err != nil {
		return err
	}
	if s.session(session) == nil {
		return wire.Errf(proto.ErrNoSuchSession, "no such session")
	}
	if cols < 1 || cols > maxPeerWidth {
		return wire.Errf(proto.ErrBadRequest, "cols must be 1..1000")
	}
	s.pmu.Lock()
	ok := g.allow("resize")
	if ok {
		g.widths[session] = cols
		g.lastWidth = cols
	}
	s.pmu.Unlock()
	if !ok {
		return wire.Errf(proto.ErrRateLimited, "too many resize messages")
	}
	s.nawsTouch()
	return nil
}

func (g *gtfPeer) attachedSet() map[string]bool {
	if g == nil {
		return nil
	}
	return g.attached
}

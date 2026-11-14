// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package squatter

import (
	"time"

	"github.com/itsnotyoutoday/gtf/internal/squatter/proto"
)

// The terminal width the game is told (NAWS) when several clients are attached. Lines travel
// unwrapped, so every client wraps for itself; this one number is shared. squatter_naws picks it:
//
//	local   gtf's own terminal only (no extension involved)
//	active  the width of whichever attached client (a peer or the local terminal) last sent input
//	min     the smallest of all attached clients
//	max     the largest
//
// A client "attached to a session" counts for that session: the local terminal always, a peer with
// the naws feature that attached the session, has sent a width (for that session, else its last
// one) and has the operator role or better. Widths are clamped to 10..1000. A change is debounced
// (nawsDebounce()) and handed to the game once per session through Host.ApplyNAWS.

func clampCols(n int) int {
	if n < minPeerWidth {
		return minPeerWidth
	}
	if n > maxPeerWidth {
		return maxPeerWidth
	}
	return n
}

// computeNAWS is the width the policy gives for a session now.
func (s *Squatter) computeNAWS(session string) int {
	policy := s.Config().NAWS
	s.pmu.Lock()
	defer s.pmu.Unlock()
	return s.computeLocked(policy, session)
}

type candidate struct {
	cols int
	last time.Time
}

func (s *Squatter) computeLocked(policy, session string) int {
	local := s.local.cols
	if local <= 0 {
		local = 80
	}
	if policy == NAWSLocal {
		return clampCols(local)
	}
	cands := []candidate{{local, s.local.lastInput}}
	for _, pi := range s.peers {
		g := pi.gtf
		if g == nil || !g.has(FeatNAWS) || !g.attached[session] || rank(pi.p) < rankOperator {
			continue
		}
		w := g.widths[session]
		if w == 0 {
			w = g.lastWidth
		}
		if w == 0 {
			continue
		}
		cands = append(cands, candidate{w, g.lastInput[session]})
	}
	best := cands[0]
	for _, c := range cands[1:] {
		switch policy {
		case NAWSMin:
			if c.cols < best.cols {
				best = c
			}
		case NAWSMax:
			if c.cols > best.cols {
				best = c
			}
		default: // active: the latest input wins; on a tie the local terminal (first) keeps it
			if c.last.After(best.last) {
				best = c
			}
		}
	}
	return clampCols(best.cols)
}

// SessionCols is the width to tell the game for a session now, and it records that value as the
// one the game has (so a later identical result sends nothing). The client calls it when a
// connection opens and when its own terminal changes; engine goroutine.
func (s *Squatter) SessionCols(session string, localCols int) int {
	if localCols > 0 {
		s.SetLocalCols(localCols)
	}
	if !s.active.Load() || session == "" {
		return localCols
	}
	n := s.computeNAWS(session)
	s.nmu.Lock()
	s.nsent[session] = n
	s.nmu.Unlock()
	return n
}

// SetLocalCols records the local terminal's width (no debounce, no sending: see LocalResized).
func (s *Squatter) SetLocalCols(cols int) {
	s.pmu.Lock()
	s.local.cols = cols
	s.pmu.Unlock()
}

// LocalResized is called when gtf's own terminal changed size. With squatter_naws=local it returns
// false and the client resizes the game connections itself as it always did; otherwise the squatter
// re-evaluates every session (debounced) and returns true.
func (s *Squatter) LocalResized(cols, rows int) bool {
	s.pmu.Lock()
	s.local.cols, s.local.rows = cols, rows
	s.pmu.Unlock()
	if !s.active.Load() || s.Config().NAWS == NAWSLocal {
		return false
	}
	s.nawsTouch()
	return true
}

// nawsTouch schedules a re-evaluation of the given sessions (all when none are named).
func (s *Squatter) nawsTouch(sessions ...string) {
	if !s.active.Load() || s.Config().NAWS == NAWSLocal {
		return
	}
	if len(sessions) == 0 {
		for _, ss := range s.sessionList() {
			sessions = append(sessions, ss.id)
		}
	}
	s.nmu.Lock()
	defer s.nmu.Unlock()
	for _, id := range sessions {
		if t := s.ntimers[id]; t != nil {
			t.Reset(nawsDebounce())
			continue
		}
		id := id
		s.ntimers[id] = time.AfterFunc(nawsDebounce(), func() { s.nawsFire(id) })
	}
}

func (s *Squatter) nawsFire(id string) {
	s.nmu.Lock()
	delete(s.ntimers, id)
	s.nmu.Unlock()
	if !s.active.Load() || s.session(id) == nil || s.Config().NAWS == NAWSLocal {
		return
	}
	n := s.computeNAWS(id)
	s.pmu.Lock()
	local := clampCols(s.local.cols)
	s.pmu.Unlock()
	s.nmu.Lock()
	old, known := s.nsent[id]
	if !known {
		old = local // never told otherwise: the game has gtf's own terminal width
	}
	same := old == n
	if !same {
		s.nsent[id] = n
	}
	s.nmu.Unlock()
	if same {
		return
	}
	s.host.Post(func() { s.host.ApplyNAWS(id, n) })
	msg := map[string]any{"v": proto.Version, "t": "gtf.naws", "session": id, "cols": n, "policy": s.Config().NAWS}
	for _, p := range s.presenceTargets(FeatNAWS, 0) {
		p.Send(msg)
	}
}

// ReportedCols is the width last handed to the game for a session (0 = unknown).
func (s *Squatter) ReportedCols(session string) int {
	s.nmu.Lock()
	defer s.nmu.Unlock()
	return s.nsent[session]
}

// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package cmcpui

import "time"

// The notice line: one visible line at the top of a CMCP screen, in every theme. A pure queue with
// timestamps (no clock of its own), so it is testable. Rules:
//   - a notice with the key of the visible (or a queued) one REPLACES it in place: a "downloaded"
//     replaces its own "downloading";
//   - another notice is shown at once when the visible one has been up at least MinShow, otherwise
//     it waits its turn (a short queue: when it overflows the oldest waiting ones are dropped), so
//     quick events each get to be read;
//   - a notice lingers: info 5 s, done 5.5 s, failure 8 s; progress stays until replaced (or
//     ProgressMax, so a lost "done" cannot leave it up forever).
const (
	MinShow     = 1200 * time.Millisecond
	LingerInfo  = 5 * time.Second
	LingerDone  = 5500 * time.Millisecond
	LingerFail  = 8 * time.Second
	ProgressMax = 60 * time.Second
	maxWaiting  = 3
)

// NoticeKind picks the linger time and the look.
type NoticeKind int

const (
	NoticeInfo NoticeKind = iota
	NoticeProgress
	NoticeDone
	NoticeFail
)

type notice struct {
	key  string
	text string
	kind NoticeKind
	at   time.Time // when it became (or will be) the visible one
	end  time.Time
}

func (n *notice) linger() time.Duration {
	switch n.kind {
	case NoticeProgress:
		return ProgressMax
	case NoticeDone:
		return LingerDone
	case NoticeFail:
		return LingerFail
	}
	return LingerInfo
}

// Notices is the queue.
type Notices struct {
	vis  *notice
	wait []*notice
}

// Post adds a notice at time now. key may be "" (never replaced in place).
func (q *Notices) Post(now time.Time, key, text string, kind NoticeKind) {
	q.advance(now)
	n := &notice{key: key, text: text, kind: kind}
	if key != "" {
		if q.vis != nil && q.vis.key == key {
			n.at = now
			n.end = now.Add(n.linger())
			q.vis = n
			return
		}
		for i, w := range q.wait {
			if w.key == key {
				q.wait[i] = n
				return
			}
		}
	}
	if q.vis == nil || (len(q.wait) == 0 && now.Sub(q.vis.at) >= MinShow) {
		n.at = now
		n.end = now.Add(n.linger())
		q.vis = n
		return
	}
	q.wait = append(q.wait, n)
	if len(q.wait) > maxWaiting {
		q.wait = q.wait[len(q.wait)-maxWaiting:]
	}
}

// advance moves the queue to time now: expired notices go, waiting ones take over once the visible
// one has had MinShow (their time starts then, not when the poll happens).
func (q *Notices) advance(now time.Time) {
	for {
		if q.vis != nil && !now.Before(q.vis.end) {
			// it ran out; the next waiting one starts when it did (or at its minimum, never sooner)
			if len(q.wait) == 0 {
				q.vis = nil
				return
			}
			start := q.vis.end
			q.vis = q.take(start)
			continue
		}
		if q.vis == nil {
			if len(q.wait) == 0 {
				return
			}
			q.vis = q.take(now)
			continue
		}
		if len(q.wait) > 0 {
			if at := q.vis.at.Add(MinShow); !now.Before(at) {
				q.vis = q.take(at)
				continue
			}
		}
		return
	}
}

func (q *Notices) take(at time.Time) *notice {
	n := q.wait[0]
	q.wait = q.wait[1:]
	n.at = at
	n.end = at.Add(n.linger())
	return n
}

// Current is the text to show at time now ("" when there is none) and its kind.
func (q *Notices) Current(now time.Time) (string, NoticeKind) {
	q.advance(now)
	if q.vis == nil {
		return "", NoticeInfo
	}
	return q.vis.text, q.vis.kind
}

// NextChange is when the picture will next change on its own (zero when nothing is pending), so the
// caller can schedule one redraw.
func (q *Notices) NextChange(now time.Time) time.Time {
	q.advance(now)
	if q.vis == nil {
		return time.Time{}
	}
	t := q.vis.end
	if len(q.wait) > 0 {
		if at := q.vis.at.Add(MinShow); at.Before(t) {
			t = at
		}
	}
	return t
}

// Drop removes the notices whose key has the prefix (a question that no longer applies).
func (q *Notices) Drop(prefix string) {
	if q.vis != nil && len(q.vis.key) >= len(prefix) && q.vis.key[:len(prefix)] == prefix {
		q.vis = nil
	}
	var w []*notice
	for _, n := range q.wait {
		if !(len(n.key) >= len(prefix) && n.key[:len(prefix)] == prefix) {
			w = append(w, n)
		}
	}
	q.wait = w
}

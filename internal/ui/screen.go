// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package ui

import "github.com/itsnotyoutoday/gtf/internal/layout"

// Screen is one world's output: its scrollback plus the more-pager state
// (tf keeps one per connection and shows the foreground one).
type Screen struct {
	Name     string
	SB       *Scrollback
	More     bool // pause output after a screenful (tf's "more")
	Paused   bool
	vis      int // physical lines [0,vis) are shown; the rest are held
	outcount int // lines that may still print before pausing
	armed    bool
	Unseen   int // lines received while this screen was in the background
	// OnPause is called when the pager engages (the MORE hook).
	OnPause func()
}

// NewScreen makes an empty screen.
func NewScreen(name string, maxLines int) *Screen {
	return &Screen{Name: name, SB: NewScrollback(maxLines), More: true}
}

// Held is the number of lines waiting below the visible window.
func (s *Screen) Held() int { return s.SB.Len() - s.vis }

// Vis is the exclusive end of the displayed physical lines.
func (s *Screen) Vis() int { return s.vis }

// ResetCount re-arms the pager for a full window (the user typed something).
func (s *Screen) ResetCount(winlines int) {
	if !s.Paused {
		s.outcount = winlines
	}
}

// Add appends a logical line. It returns the number of physical lines that
// became visible immediately.
func (s *Screen) Add(cells []layout.Cell, winlines int) int {
	if !s.armed {
		s.armed, s.outcount = true, winlines
	}
	before := s.SB.Len()
	oldVis := s.vis
	added, dropped := s.SB.Add(cells)
	if dropped > 0 {
		s.vis = max(0, s.vis-dropped)
		oldVis = max(0, oldVis-dropped)
		before -= dropped
	}
	shown := 0
	for i := 0; i < added; i++ {
		if !s.Paused && s.More && s.outcount <= 0 {
			s.Paused = true
			if s.OnPause != nil {
				s.OnPause()
			}
		} else if !s.Paused {
			s.outcount--
		}
		if !s.Paused {
			s.vis = before + i + 1
			shown++
		}
	}
	return shown
}

// Scroll implements morescroll(n): n>0 pages forward through held lines, n<0
// scrolls back into history.
func (s *Screen) Scroll(n, winlines int) {
	switch {
	case n > 0:
		if !s.Paused {
			return
		}
		avail := s.SB.Len() - s.vis
		k := min(n, avail)
		s.vis += k
		if k < n {
			s.Paused = false
			s.outcount = n - k
		}
	case n < 0:
		floor := min(winlines, s.SB.Len())
		s.vis = max(s.vis+n, floor)
		s.Paused = true
		s.outcount = 0
	}
}

// Flush jumps to the end of the scrollback (dokey flush).
func (s *Screen) Flush(winlines int) {
	s.vis = s.SB.Len()
	s.Paused = false
	s.outcount = winlines
}

// Pause stops the screen where it is (dokey pause).
func (s *Screen) Pause() {
	if !s.Paused {
		s.Paused = true
		s.outcount = 0
		if s.OnPause != nil {
			s.OnPause()
		}
	}
}

// remap adjusts the displayed boundary after a rewrap.
func (s *Screen) rewrap(width int, cfg WrapConfig) {
	remap := s.SB.Rewrap(width, cfg)
	if s.Paused {
		s.vis = min(remap(s.vis), s.SB.Len())
	} else {
		s.vis = s.SB.Len()
	}
}

// Refilter re-applies the Keep filter (a /limit changed) and returns the
// number of visible physical lines.
func (s *Screen) Refilter() int {
	s.SB.Relayout()
	s.vis = s.SB.Len()
	s.Paused = false
	return s.SB.Len()
}

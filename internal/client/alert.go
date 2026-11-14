// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package client

import (
	"strconv"
	"time"

	"github.com/itsnotyoutoday/gtf/internal/layout"
	"github.com/itsnotyoutoday/gtfparser"
)

// The alert is tf's status-line message ("% Activity in world X"): it overwrites
// part of the first status row for %alert_time seconds, or until the world it
// is about is brought to the foreground. Its text is drawn with %alert_attr.

type alertState struct {
	cells    []layout.Cell
	pos, len int
}

// raiseAlert is tf's alert(): place msg on the status row, starting at the
// field that follows @world when it fits there.
func (c *Client) raiseAlert(msg string) {
	c.alertSeq++
	w := c.ui.Wrap.Size
	if w <= 0 {
		w = c.ui.W
	}
	n := len([]rune(msg))
	pos, ln := 0, min(n, w)
	if n < w {
		rows := c.in.StatusRows(c.ui.W)
		if len(rows) > 0 {
			sp := rows[0].Spans
			for i, f := range sp {
				if f.Field.Kind == gtfparser.StatusInternal && f.Field.Name == "world" && i+1 < len(sp) {
					pos = sp[i+1].Col
					if pos+ln > w {
						pos = 0
					}
					break
				}
			}
		}
	}
	cells := c.attrsCells(msg, c.in.Var("alert_attr"))
	if len(cells) > ln {
		cells = cells[:ln]
	}
	c.alert = &alertState{cells: cells, pos: pos, len: len(cells)}
	id := c.alertSeq
	secs, err := strconv.ParseFloat(c.in.Var("alert_time"), 64)
	if err != nil || secs <= 0 {
		secs = 5
	}
	d := time.Duration(secs * float64(time.Second))
	c.alertUntil = c.cfg.Now().Add(d)
	time.AfterFunc(d, func() {
		c.Post(func(*gtfparser.Interp) {
			if c.alertSeq == id && c.alert != nil {
				c.clearAlert()
				c.scheduleRender()
			}
		})
	})
	c.scheduleRender()
}

func (c *Client) clearAlert() {
	c.alert = nil
	c.alertSeq = 0
	c.scheduleRender()
}

// applyAlert overlays the alert on the first status row.
func (c *Client) applyAlert(rows [][]layout.Cell) {
	a := c.alert
	if a == nil || len(rows) == 0 {
		return
	}
	row := rows[0]
	for i, cell := range a.cells {
		if a.pos+i < len(row) {
			row[a.pos+i] = cell
		}
	}
}

// alertRemaining is how long the current alert has left to show (for /restart).
func (c *Client) alertRemaining() time.Duration {
	if c.alert == nil {
		return 0
	}
	left := c.alertUntil.Sub(c.cfg.Now())
	return max(left, 0)
}

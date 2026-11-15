// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package cmcpui

import (
	"testing"
	"time"
)

func TestNoticeDownloadDoneExpire(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	at := func(ms int) time.Time { return t0.Add(time.Duration(ms) * time.Millisecond) }
	var q Notices
	q.Post(at(0), "theme:romulan", "Downloading Romulan theme pack...", NoticeProgress)
	if s, _ := q.Current(at(10000)); s != "Downloading Romulan theme pack..." {
		t.Fatalf("progress should stay: %q", s)
	}
	q.Post(at(12000), "theme:romulan", "Downloaded Romulan theme pack", NoticeDone) // replaced in place, at once
	if s, k := q.Current(at(12000)); s != "Downloaded Romulan theme pack" || k != NoticeDone {
		t.Fatalf("done did not replace: %q", s)
	}
	if s, _ := q.Current(at(12000 + 5400)); s == "" {
		t.Fatal("done expired early")
	}
	if s, _ := q.Current(at(12000 + 5500)); s != "" {
		t.Fatalf("done lingered too long: %q", s)
	}
}

func TestNoticeReplacementAndMinShow(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	at := func(ms int) time.Time { return t0.Add(time.Duration(ms) * time.Millisecond) }
	var q Notices
	q.Post(at(0), "", "one", NoticeInfo)
	q.Post(at(100), "", "two", NoticeInfo)   // too soon: waits
	q.Post(at(200), "", "three", NoticeInfo) // waits behind two
	if s, _ := q.Current(at(1100)); s != "one" {
		t.Fatalf("one must show its minimum: %q", s)
	}
	if s, _ := q.Current(at(1200)); s != "two" {
		t.Fatalf("two at 1.2 s: %q", s)
	}
	if s, _ := q.Current(at(2399)); s != "two" {
		t.Fatalf("two must show 1.2 s: %q", s)
	}
	if s, _ := q.Current(at(2400)); s != "three" {
		t.Fatalf("three after two: %q", s)
	}
	// after the minimum a newer one replaces the visible one at once
	q.Post(at(4000), "", "four", NoticeInfo)
	if s, _ := q.Current(at(4000)); s != "four" {
		t.Fatalf("a newer one should replace at once: %q", s)
	}
	// failures linger 8 s
	q.Post(at(6000), "x", "failed", NoticeFail)
	if s, _ := q.Current(at(6000 + 7900)); s != "failed" {
		t.Fatalf("fail gone early: %q", s)
	}
	if s, _ := q.Current(at(6000 + 8000)); s != "" {
		t.Fatalf("fail stayed: %q", s)
	}
}

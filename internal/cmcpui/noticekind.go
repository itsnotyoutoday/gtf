// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package cmcpui

// NoticeKind picks the look of the notice line (the queue itself is the core's cmcp/go/notice).
type NoticeKind int

const (
	NoticeInfo NoticeKind = iota
	NoticeProgress
	NoticeDone
	NoticeFail
)

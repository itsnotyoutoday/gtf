// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

// Copied from github.com/itsnotyoutoday/squatClientDaemon (same author and license); see docs/SQUATTER.md.

package wire

import (
	"context"
	"encoding/json"

	"github.com/itsnotyoutoday/gtf/internal/squatter/proto"
)

// Peer is one authenticated client connection as seen by the Backend. All
// methods are safe for concurrent use and never block: Send/SendRaw enqueue to
// a bounded queue and, if it is full, drop the CLIENT (never anything else)
// and return false.
type Peer interface {
	ID() uint64 // unique per connection for the life of the process
	DeviceID() string
	DeviceName() string
	Caps() proto.Caps
	Send(msg any) bool
	SendRaw(frame []byte) bool
	Close(reason string)
}

// Extender is implemented by peers that can report the extensions the client announced in hello.
// Ext returns the raw announcement object of one extension, nil when it was not announced.
type Extender interface {
	Ext(name string) json.RawMessage
	Offer() []string
}

// ExtWelcomer is optionally implemented by a Backend: WelcomeExt is called (after PeerJoined) to
// build the welcome's "ext" answer for p (nil or empty: no "ext" member) and "negotiated" list (the
// accepted names of the client's hello.offer).
type ExtWelcomer interface {
	WelcomeExt(p Peer) (map[string]any, []string)
}

// Backend is what the Server drives (implemented by internal/daemon). Methods
// return *ProtoError for protocol errors; any other error becomes "internal".
type Backend interface {
	// PeerJoined registers p and returns the session list for the welcome.
	PeerJoined(p Peer) []proto.SessionInfo
	// PeerLeft is called exactly once after PeerJoined, when p disconnects.
	PeerLeft(p Peer)
	List(p Peer) []proto.SessionInfo
	Connect(ctx context.Context, p Peer, req ConnectReq) (proto.SessionInfo, error)
	Disconnect(p Peer, session string) error
	Dismiss(p Peer, session string) error
	// Attach enqueues attached, backlog* and later live entries to p itself.
	Attach(p Peer, req AttachReq) error
	Detach(p Peer, session string) error
	Send(p Peer, session, text string) error
	Ack(p Peer, session string, seq uint64) error
	Resize(p Peer, session string, cols, rows int) error
	// CapsChanged is called after p's caps were updated by a caps message.
	CapsChanged(p Peer)
}

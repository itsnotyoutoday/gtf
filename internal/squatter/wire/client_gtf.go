// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package wire

import (
	"context"
	"encoding/json"

	"github.com/itsnotyoutoday/gtf/internal/squatter/proto"
)

// WelcomeExt returns what the server answered for each announced extension in its welcome
// (name -> raw object); empty when the server answered none. gtf addition, docs/SWP-GTF.md.
func (c *Client) WelcomeExt() map[string]json.RawMessage { return c.wext }

// RawSend sends one message of any type (fields are merged into {"v":1,"t":t}); it does not wait
// for an answer. Used for the gtf extension messages.
func (c *Client) RawSend(ctx context.Context, t string, fields map[string]any) error {
	m := map[string]any{"v": proto.Version, "t": t}
	for k, v := range fields {
		m[k] = v
	}
	return c.write(ctx, m)
}

// RawRequest sends one message with a request id and waits for the reply carrying it. A reply of a
// type this library does not know comes back as Ext; a protocol error as *ProtoError (with Data).
func (c *Client) RawRequest(ctx context.Context, t string, fields map[string]any) (any, error) {
	return c.request(ctx, func(id string) any {
		m := map[string]any{"v": proto.Version, "t": t, "id": id}
		for k, v := range fields {
			m[k] = v
		}
		return m
	})
}

// Negotiated returns welcome.negotiated (the answer to ClientOptions.Offer).
func (c *Client) Negotiated() []string { return append([]string(nil), c.negot...) }

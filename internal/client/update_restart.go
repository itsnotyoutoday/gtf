// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package client

// /update -r (and the "newer version installed" case of the package builds) restarts in place with the
// same mechanism as /restart: connections stay open (docs/RESTART.md).
func init() {
	restartHook = func(c *Client, force bool) error { return c.Restart(force) }
}

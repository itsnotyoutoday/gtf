// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

// Package update is how gtf stays current: the GitHub releases lookup, semantic
// version comparison, checksum verification, and the safe replacement of the
// running binary and its library directory. It does no network I/O of its own:
// the HTTP client is injected (Client.HTTP) so tests use httptest.
//
// The design follows squatterd's self-update (same release convention: tags
// vX.Y.Z-rel are final, vX.Y.Z-alphaN / -betaN are pre-releases), with one
// difference: a gtf release ships a binary AND a library directory, which are
// replaced together.
package update

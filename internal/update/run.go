// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package update

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
)

// Run is the shared body of "/update" and "gtf --update": it prints what it finds, installs when
// install is set, and returns the result so the caller can follow up (restart). A development build
// is reported and never touched.
func (u *Updater) Run(ctx context.Context, channel string, install bool, w io.Writer) (Result, error) {
	res, rel, err := u.Check(ctx, channel)
	switch {
	case errors.Is(err, ErrNoRelease):
		fmt.Fprintf(w, "No %s release has been published yet; nothing to update to.\n", channel)
		return res, nil
	case err != nil:
		return res, err
	}
	if !res.Newer {
		fmt.Fprintf(w, "gtf %s is up to date (newest %s release: %s).\n", u.Version, channel, res.Latest)
		return res, nil
	}
	if u.Managed() {
		fmt.Fprintf(w, "Update available: gtf %s -> %s (%s channel).\n", u.Version, res.Latest, channel)
		fmt.Fprintln(w, ManagedAdvice)
		return res, nil
	}
	if !install {
		fmt.Fprintf(w, "Update available: gtf %s -> %s (%s channel).\n", u.Version, res.Latest, channel)
		fmt.Fprintf(w, "Run /update -y to install it (or /update -y -r to install and restart without dropping connections).\n")
		return res, nil
	}
	fmt.Fprintf(w, "Updating gtf %s -> %s ...\n", u.Version, res.Latest)
	res, err = u.Update(ctx, rel)
	if err != nil {
		return res, err
	}
	where := u.ExePath
	if u.LibDir != "" {
		where += " and " + u.LibDir
	}
	fmt.Fprintf(w, "Installed gtf %s at %s (the previous binary is kept as %s).\n", res.Latest, where, res.Kept)
	return res, nil
}

// PermissionHint is the advice when an update cannot write to the install directories.
func PermissionHint(exe string) string {
	return "No permission to replace it. Update as the user who installed it, or run: sudo " + strings.TrimSpace(exe) + " --update -y"
}

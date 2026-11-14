// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package squatter

import "os"

func statMode(p string) (os.FileMode, error) {
	fi, err := os.Stat(p)
	if err != nil {
		return 0, err
	}
	return fi.Mode().Perm(), nil
}

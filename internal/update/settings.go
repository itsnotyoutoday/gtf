// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package update

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Update channels. "stable" ignores pre-releases; "prerelease" takes the newest release of any kind.
const (
	ChannelStable     = "stable"
	ChannelPrerelease = "prerelease"
)

// ConfigFile is the settings file name inside the data directory (~/.gtf).
const ConfigFile = "config.json"

// Settings is the content of <data-dir>/config.json. Unknown fields are ignored; a missing file or
// field means the default.
type Settings struct {
	UpdateCheck   bool   `json:"update_check"`   // look for new versions about once a day and say so at startup
	UpdateChannel string `json:"update_channel"` // stable | prerelease
}

// Defaults are the settings used when there is no config file. The channel is "prerelease" while only
// pre-releases exist; change it to "stable" once a stable release is out.
func Defaults() Settings { return Settings{UpdateCheck: true, UpdateChannel: ChannelPrerelease} }

// ValidChannel reports whether c is a known channel.
func ValidChannel(c string) bool { return c == ChannelStable || c == ChannelPrerelease }

// ConfigPath is the settings file of a data directory.
func ConfigPath(dataDir string) string { return filepath.Join(dataDir, ConfigFile) }

// Load reads the settings file. A missing file gives the defaults and no error. A file that cannot be
// parsed, or names an unknown channel, gives the defaults (for the bad part) together with an error the
// caller should show as a warning.
func Load(path string) (Settings, error) {
	s := Defaults()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	var raw struct {
		UpdateCheck   *bool   `json:"update_check"`
		UpdateChannel *string `json:"update_channel"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return Defaults(), fmt.Errorf("%s: %w", path, err)
	}
	if raw.UpdateCheck != nil {
		s.UpdateCheck = *raw.UpdateCheck
	}
	if raw.UpdateChannel != nil && *raw.UpdateChannel != "" {
		if !ValidChannel(*raw.UpdateChannel) {
			return s, fmt.Errorf("%s: unknown update_channel %q (use %q or %q)", path, *raw.UpdateChannel, ChannelStable, ChannelPrerelease)
		}
		s.UpdateChannel = *raw.UpdateChannel
	}
	return s, nil
}

// Save writes the settings atomically (mode 0600), keeping unknown fields of an existing file.
func (s Settings) Save(path string) error {
	m := map[string]any{}
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &m)
	}
	m["update_check"] = s.UpdateCheck
	m["update_channel"] = s.UpdateChannel
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

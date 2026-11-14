// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package squatter

import (
	"strings"
	"testing"
	"time"
)

func vars(m map[string]string) func(string) string { return func(n string) string { return m[n] } }

func TestConfigDefaults(t *testing.T) {
	c, w := ConfigFromVars(vars(nil), "/home/u")
	if len(w) != 0 {
		t.Fatal(w)
	}
	if c.Enabled || c.Port != 10328 || c.Bind != "127.0.0.1" || c.Dir != "/home/u/.gtf/squatter" || c.Kind != KindTF || !c.TLS ||
		c.PairRole != "admin" || c.PairTTL != 10*time.Minute || !c.Commands || c.MaxClients != 8 || c.Path() != "/tfsp" {
		t.Fatalf("%+v", c)
	}
	if c.BindWarning() != "" {
		t.Fatal("loopback must not warn")
	}
}

func TestConfigValues(t *testing.T) {
	c, w := ConfigFromVars(vars(map[string]string{
		VarEnabled: "on", VarPort: "4444", VarBind: "0.0.0.0", VarName: "box", VarDir: "~/sq", VarKind: "SWP", VarTLS: "off",
		VarScrollback: "500", VarMaxClients: "3", VarMaxSessions: "9", VarPairTTL: "60", VarPairRole: "Operator", VarCommands: "0",
		VarCert: "/c", VarKey: "/k"}), "/h")
	if len(w) != 0 {
		t.Fatal(w)
	}
	if !c.Enabled || c.Port != 4444 || c.Bind != "0.0.0.0" || c.Name != "box" || c.Dir != "/h/sq" || c.Kind != KindSWP || c.TLS ||
		c.Scrollback != 500 || c.MaxClients != 3 || c.MaxSessions != 9 || c.PairTTL != time.Minute || c.PairRole != "operator" ||
		c.Commands || c.CertFile != "/c" || c.KeyFile != "/k" || c.Path() != "/swp" {
		t.Fatalf("%+v", c)
	}
	if !strings.Contains(c.BindWarning(), "0.0.0.0:4444") {
		t.Fatal("public bind needs a warning")
	}
	if c.Validate() == nil {
		t.Fatal("plain ws on a public bind must be refused")
	}
}

func TestConfigBadValuesKeepDefaultsAndWarn(t *testing.T) {
	c, w := ConfigFromVars(vars(map[string]string{VarEnabled: "maybe", VarPort: "99999", VarKind: "x", VarMaxClients: "0",
		VarPairTTL: "5", VarPairRole: "root", VarScrollback: "abc"}), "/h")
	if len(w) != 7 {
		t.Fatalf("%d warnings: %v", len(w), w)
	}
	d := DefaultConfig("/h")
	d.Name = c.Name
	if c != d {
		t.Fatalf("%+v", c)
	}
}

func TestConfigValidateCertPair(t *testing.T) {
	c := DefaultConfig("/h")
	c.CertFile = "/c"
	if c.Validate() == nil {
		t.Fatal("cert without key")
	}
	c.KeyFile = "/k"
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestIsLoopback(t *testing.T) {
	for b, want := range map[string]bool{"127.0.0.1": true, "::1": true, "[::1]": true, "localhost": true, "0.0.0.0": false, "192.168.1.2": false, "::": false} {
		if IsLoopback(b) != want {
			t.Errorf("%s", b)
		}
	}
}

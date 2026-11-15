// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package squatter

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Variable names (ordinary tf variables, set in the rc file or with /set).
const (
	VarEnabled     = "squatter"
	VarPort        = "squatter_port"
	VarBind        = "squatter_bind"
	VarName        = "squatter_name"
	VarDir         = "squatter_dir"
	VarKind        = "squatter_kind"
	VarTLS         = "squatter_tls"
	VarCert        = "squatter_cert"
	VarKey         = "squatter_key"
	VarScrollback  = "squatter_scrollback"
	VarMaxClients  = "squatter_max_clients"
	VarMaxSessions = "squatter_max_sessions"
	VarPairTTL     = "squatter_pair_ttl"
	VarPairRole    = "squatter_pair_role"
	VarCommands    = "squatter_commands"
	VarGTF         = "squatter_gtf"
	VarNAWS        = "squatter_naws"
	VarWorldSave   = "squatter_world_save"
	VarLocal       = "squatter_local"

	// output variables, written by the squatter (never read back)
	VarState       = "squatter_state"
	VarPeers       = "squatter_peers"
	VarFingerprint = "squatter_fingerprint"
	VarAddr        = "squatter_addr"
	VarError       = "squatter_error"
)

// ConfigVars are the configuration variables, in the order /squatter status shows them.
var ConfigVars = []string{VarEnabled, VarPort, VarBind, VarName, VarDir, VarKind, VarTLS, VarCert, VarKey,
	VarScrollback, VarMaxClients, VarMaxSessions, VarPairTTL, VarPairRole, VarCommands, VarGTF, VarNAWS, VarWorldSave, VarLocal}

// Defaults.
const (
	DefaultPort        = 10328     // tf-squatterd's port; squatterd itself uses 10327
	DefaultBind        = "0.0.0.0" // every interface: the public address, a VPN (Tailscale) address and the LAN; plain ws:// (squatter_tls=off) defaults to loopback
	DefaultScrollback  = 20000
	DefaultMaxClients  = 8
	DefaultMaxSessions = 32
	DefaultPairTTL     = 10 * time.Minute
	DefaultPairRole    = "admin"
)

// NAWS policies (squatter_naws): which terminal width the game is told.
const (
	NAWSActive = "active" // the client (peer or local terminal) that last sent input
	NAWSMin    = "min"    // the smallest of all attached clients
	NAWSMax    = "max"    // the largest of all attached clients
	NAWSLocal  = "local"  // only gtf's own terminal (the behaviour before the gtf extension)
)

// Server kinds (squatter_kind).
const (
	KindTF  = "tf"  // announce "tf-squatterd" on /tfsp: SquatClient treats gtf as a TinyFugue bridge
	KindSWP = "swp" // announce plain squatterd on /swp
)

// Config is the effective configuration, read from the variables.
type Config struct {
	Enabled     bool
	Port        int
	Bind        string
	Name        string
	Dir         string
	Kind        string
	TLS         bool
	CertFile    string
	KeyFile     string
	Scrollback  int
	MaxClients  int
	MaxSessions int
	PairTTL     time.Duration
	PairRole    string
	Commands    bool
	GTF         bool   // negotiate the gtf extension (docs/SWP-GTF.md)
	NAWS        string // NAWSActive, NAWSMin, NAWSMax or NAWSLocal
	WorldSave   bool   // save the world file after a peer changed a world
	Local       bool   // also forward the client's own text (messages, script errors, /recall output) as system lines (default off: only game text)
}

// DefaultConfig is the configuration with every variable unset.
func DefaultConfig(home string) Config {
	name, _ := os.Hostname()
	if name == "" {
		name = "gtf"
	}
	return Config{Port: DefaultPort, Bind: DefaultBind, Name: name, Dir: filepath.Join(home, ".gtf", "squatter"),
		Kind: KindTF, TLS: true, Scrollback: DefaultScrollback, MaxClients: DefaultMaxClients,
		MaxSessions: DefaultMaxSessions, PairTTL: DefaultPairTTL, PairRole: DefaultPairRole, Commands: true,
		GTF: true, NAWS: NAWSActive, WorldSave: true}
}

func isOn(s string) (on, ok bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "on", "yes", "true", "y":
		return true, true
	case "0", "off", "no", "false", "n", "":
		return false, true
	}
	return false, false
}

// ConfigFromVars builds a Config from the variable values (get returns "" for an unset variable).
// A bad value keeps the default and yields a warning.
func ConfigFromVars(get func(string) string, home string) (Config, []string) {
	c := DefaultConfig(home)
	var warn []string
	boolVar := func(name string, dst *bool) {
		if v := get(name); strings.TrimSpace(v) != "" {
			if on, ok := isOn(v); ok {
				*dst = on
			} else {
				warn = append(warn, fmt.Sprintf("%s: %q is not on or off; keeping %v", name, v, *dst))
			}
		}
	}
	intVar := func(name string, dst *int, min, max int) {
		if v := strings.TrimSpace(get(name)); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n < min || n > max {
				warn = append(warn, fmt.Sprintf("%s: %q is not a number from %d to %d; keeping %d", name, v, min, max, *dst))
				return
			}
			*dst = n
		}
	}
	strVar := func(name string, dst *string) {
		if v := strings.TrimSpace(get(name)); v != "" {
			*dst = v
		}
	}
	boolVar(VarEnabled, &c.Enabled)
	intVar(VarPort, &c.Port, 0, 65535)
	strVar(VarBind, &c.Bind)
	strVar(VarName, &c.Name)
	if v := strings.TrimSpace(get(VarDir)); v != "" {
		if strings.HasPrefix(v, "~/") {
			v = filepath.Join(home, v[2:])
		}
		c.Dir = v
	}
	if v := strings.ToLower(strings.TrimSpace(get(VarKind))); v != "" {
		if v == KindTF || v == KindSWP {
			c.Kind = v
		} else {
			warn = append(warn, fmt.Sprintf("%s: %q is not tf or swp; keeping %s", VarKind, v, c.Kind))
		}
	}
	boolVar(VarTLS, &c.TLS)
	if strings.TrimSpace(get(VarBind)) == "" && !c.TLS {
		c.Bind = "127.0.0.1" // plain ws:// is only allowed on loopback
	}
	strVar(VarCert, &c.CertFile)
	strVar(VarKey, &c.KeyFile)
	intVar(VarScrollback, &c.Scrollback, 100, 1000000)
	intVar(VarMaxClients, &c.MaxClients, 1, 64)
	intVar(VarMaxSessions, &c.MaxSessions, 1, 1000)
	if v := strings.TrimSpace(get(VarPairTTL)); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 10 || n > 7*24*3600 {
			warn = append(warn, fmt.Sprintf("%s: %q is not a number of seconds from 10 to 604800; keeping %d", VarPairTTL, v, int(c.PairTTL.Seconds())))
		} else {
			c.PairTTL = time.Duration(n) * time.Second
		}
	}
	if v := strings.ToLower(strings.TrimSpace(get(VarPairRole))); v != "" {
		if ValidRole(v) {
			c.PairRole = v
		} else {
			warn = append(warn, fmt.Sprintf("%s: %q is not viewer, operator or admin; keeping %s", VarPairRole, v, c.PairRole))
		}
	}
	boolVar(VarCommands, &c.Commands)
	boolVar(VarGTF, &c.GTF)
	boolVar(VarWorldSave, &c.WorldSave)
	boolVar(VarLocal, &c.Local)
	if v := strings.ToLower(strings.TrimSpace(get(VarNAWS))); v != "" {
		switch v {
		case NAWSActive, NAWSMin, NAWSMax, NAWSLocal:
			c.NAWS = v
		default:
			warn = append(warn, fmt.Sprintf("%s: %q is not active, min, max or local; keeping %s", VarNAWS, v, c.NAWS))
		}
	}
	return c, warn
}

// ValidRole reports whether r is a device role.
func ValidRole(r string) bool { return r == "viewer" || r == "operator" || r == "admin" }

// IsLoopback reports whether a bind address only accepts connections from this machine.
func IsLoopback(bind string) bool {
	if bind == "localhost" {
		return true
	}
	ip := net.ParseIP(strings.Trim(bind, "[]"))
	return ip != nil && ip.IsLoopback()
}

// Path is the WebSocket endpoint of the configured kind.
func (c Config) Path() string {
	if c.Kind == KindSWP {
		return "/swp"
	}
	return "/tfsp"
}

// Validate checks combinations that must not be started.
func (c Config) Validate() error {
	if !c.TLS && !IsLoopback(c.Bind) {
		return fmt.Errorf("%s=off (plain ws://) is only allowed with a loopback %s (now %q)", VarTLS, VarBind, c.Bind)
	}
	if (c.CertFile == "") != (c.KeyFile == "") {
		return fmt.Errorf("%s and %s must be set together", VarCert, VarKey)
	}
	return nil
}

// ConfigFromVarsMap is ConfigFromVars over a map (for tests and tools).
func ConfigFromVarsMap(m map[string]string) (Config, []string) {
	return ConfigFromVars(func(n string) string { return m[n] }, "/home")
}

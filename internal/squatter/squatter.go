// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

// Package squatter makes gtf act as a "squatter" (a session holder that SquatClient
// pairs with) and as the "tf bridge": the listener, TLS identity, pairing and the SWP/TFSP
// wire protocol run inside the gtf process and talk to gtf's sessions through the Host
// interface. There is no helper process and no local IPC; every call into gtf is posted to
// the engine goroutine by the Host.
//
// The wire-level packages next to this one (wire, auth, tlsutil, proto, scrollback) are
// copies of the reference implementation (github.com/itsnotyoutoday/squatClientDaemon,
// same author and licence); this package is the gtf-specific part. It imports no gtf
// package and no engine: internal/client wires it up.
//
// Threading: Apply, Stop, the session taps (Line, SessionOpened, ...) and the command
// helpers are called on the engine goroutine. The wire.Backend methods run on network
// goroutines and reach gtf only through Host.Do.
package squatter

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/itsnotyoutoday/gtf/internal/squatter/auth"
	"github.com/itsnotyoutoday/gtf/internal/squatter/proto"
	"github.com/itsnotyoutoday/gtf/internal/squatter/tlsutil"
	"github.com/itsnotyoutoday/gtf/internal/squatter/wire"
)

// SendOpts says how a remote line is delivered.
type SendOpts struct {
	Origin  string // the device id, for the echo
	Secret  bool   // password line: no echo, no record
	Command bool   // run as a tf command (admin only, squatter_commands on); otherwise raw text to the game
}

// ConnectTarget is what a peer asks for with session.connect.
type ConnectTarget struct {
	Ref  string // a world defined in gtf
	Name string
	Host string
	Port int
	TLS  bool
}

// Host is gtf as the squatter sees it. Do and Post may be called from any goroutine; every
// other method is only called inside a Do or Post closure, i.e. on the engine goroutine.
type Host interface {
	Do(f func())   // run f on the engine goroutine and wait for it (never call it from that goroutine)
	Post(f func()) // queue f on the engine goroutine
	SendLine(session, text string, o SendOpts) error
	Connect(t ConnectTarget) (session string, err error)
	Disconnect(session string) error
	SendGMCP(session, pkg, data string) error
	SendMSDP(session, name string, value any) error
	// Exec runs a tf command line and returns what it printed (ok=false when it reported an error).
	Exec(line string) (ok bool, out []string)
	Worlds() []proto.TFWorld
	Hook(event string, args ...string)
	SetVars(vars map[string]string) // the read-only status variables
	Notice(text string)             // a line for the local user
}

// Options configures New.
type Options struct {
	Version string // shown in the welcome: "gtf/<version>"
	Home    string // home directory (for the default state directory)
	Logger  *slog.Logger
}

// Squatter is the daemon.
type Squatter struct {
	host    Host
	log     *slog.Logger
	version string
	active  atomic.Bool

	mu       sync.Mutex // config, listener, registry
	cfg      Config
	state    string
	stateErr string
	run      *running
	sessions map[string]*session
	order    []string
	serial   uint64
	announce map[string]bool // device ids already announced as paired
	inherit  net.Listener    // an inherited listening socket for the next start (AdoptListener)

	pmu   sync.Mutex // peers
	peers map[uint64]*peerInfo
}

type peerInfo struct {
	p      wire.Peer
	role   string
	joined time.Time
}

// running is one listener generation.
type running struct {
	ln   net.Listener
	hs   *http.Server
	srv  *wire.Server
	as   *auth.Store
	fp   string
	addr string
	cfg  Config
}

const (
	StateOff       = "off"
	StateListening = "listening"
	StateError     = "error"
)

// New builds a stopped squatter.
func New(h Host, o Options) *Squatter {
	l := o.Logger
	if l == nil {
		l = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	v := o.Version
	if v == "" {
		v = "dev"
	}
	return &Squatter{host: h, log: l, version: v, state: StateOff, cfg: DefaultConfig(o.Home),
		sessions: map[string]*session{}, peers: map[uint64]*peerInfo{}, announce: map[string]bool{}}
}

// Active reports whether the squatter is running (cheap; the taps check it first).
func (s *Squatter) Active() bool { return s.active.Load() }

// Config returns the configuration last applied.
func (s *Squatter) Config() Config { s.mu.Lock(); defer s.mu.Unlock(); return s.cfg }

// Apply makes the squatter match cfg: start, stop or restart the listener as needed.
// Engine goroutine. Sessions survive a restart of the listener; they are dropped only when
// the squatter is switched off. seed, called when the squatter goes from off to on, registers
// the sessions that already exist.
func (s *Squatter) Apply(cfg Config, seed func()) {
	s.mu.Lock()
	old := s.cfg
	wasOn := s.active.Load()
	if cfg == old && wasOn == cfg.Enabled && (s.state == StateListening || s.state == StateError || s.state == StateOff) {
		s.mu.Unlock()
		return
	}
	s.cfg = cfg
	r := s.run
	s.run = nil
	s.mu.Unlock()
	if r != nil {
		s.closeRunning(r)
	}
	if !cfg.Enabled {
		s.active.Store(false)
		s.mu.Lock()
		for _, ss := range s.sessions {
			ss.close()
		}
		s.sessions, s.order = map[string]*session{}, nil
		s.state, s.stateErr = StateOff, ""
		s.mu.Unlock()
		s.publish(nil)
		return
	}
	if !wasOn {
		s.active.Store(true)
		if seed != nil {
			seed()
		}
	}
	if err := cfg.Validate(); err != nil {
		s.fail(err)
		return
	}
	nr, err := s.start(cfg)
	if err != nil {
		s.fail(err)
		return
	}
	s.mu.Lock()
	s.run, s.state, s.stateErr = nr, StateListening, ""
	s.mu.Unlock()
	s.publish(nr)
	if w := cfg.BindWarning(); w != "" {
		s.host.Notice("% " + w)
	}
}

func (s *Squatter) fail(err error) {
	s.mu.Lock()
	s.state, s.stateErr = StateError, err.Error()
	s.mu.Unlock()
	s.publish(nil)
	s.host.Notice("% squatter: " + err.Error())
}

// publish writes the status variables.
func (s *Squatter) publish(r *running) {
	s.mu.Lock()
	vars := map[string]string{VarState: s.state, VarError: s.stateErr, VarFingerprint: "", VarAddr: ""}
	if r != nil {
		vars[VarFingerprint], vars[VarAddr] = r.fp, r.addr
	}
	s.mu.Unlock()
	vars[VarPeers] = strconv.Itoa(s.NumPeers())
	s.host.SetVars(vars)
}

func (s *Squatter) start(cfg Config) (*running, error) {
	if err := os.MkdirAll(cfg.Dir, 0o700); err != nil {
		return nil, fmt.Errorf("state directory: %w", err)
	}
	_ = os.Chmod(cfg.Dir, 0o700)
	as, err := auth.Open(cfg.Dir)
	if err != nil {
		return nil, err
	}
	var tcfg *tls.Config
	fp := ""
	if cfg.TLS {
		cert, err := LoadCert(cfg)
		if err != nil {
			return nil, err
		}
		tcfg = tlsutil.ServerConfig(cert)
		fp = tlsutil.CertFingerprint(cert)
	}
	s.mu.Lock()
	raw := s.inherit
	s.inherit = nil
	s.mu.Unlock()
	if raw == nil {
		var err error
		raw, err = net.Listen("tcp", net.JoinHostPort(strings.Trim(cfg.Bind, "[]"), strconv.Itoa(cfg.Port)))
		if err != nil {
			return nil, fmt.Errorf("listen on %s:%d: %w", cfg.Bind, cfg.Port, err)
		}
	}
	return s.serve(cfg, as, tcfg, fp, raw), nil
}

// LoadCert returns the TLS identity: the configured files, or the one in <dir>/tls (made on first use).
func LoadCert(cfg Config) (tls.Certificate, error) {
	if cfg.CertFile != "" {
		return tlsutil.Load(cfg.CertFile, cfg.KeyFile)
	}
	return tlsutil.LoadOrCreate(cfg.Dir + "/tls")
}

// serve starts the HTTP/WebSocket server on an already bound listener (also used to adopt a
// listener inherited across a restart).
func (s *Squatter) serve(cfg Config, as *auth.Store, tcfg *tls.Config, fp string, raw net.Listener) *running {
	feats := []string{wire.FeatureTF, wire.FeatureControl, wire.FeatureWorlds, wire.FeatureState, wire.FeatureGmcp, wire.FeatureMsdp}
	if cfg.Commands {
		feats = append(feats, wire.FeatureExec)
	}
	sc := wire.ServerConfig{Backend: &backend{s: s}, Auth: as, ServerName: "gtf/" + s.version, Logger: s.log,
		MaxClients: cfg.MaxClients, Path: cfg.Path(), Instance: cfg.Name}
	if cfg.Kind == KindTF {
		sc.Kind = proto.KindTFSquatterd
		sc.ExtraFeatures = feats
	} else {
		sc.ExtraFeatures = nil
	}
	srv := wire.NewServer(sc)
	hs := &http.Server{Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second,
		ErrorLog: slog.NewLogLogger(s.log.Handler(), slog.LevelWarn)}
	var l net.Listener = raw
	if tcfg != nil {
		hs.TLSConfig = tcfg
		l = tls.NewListener(raw, tcfg)
	}
	r := &running{ln: raw, hs: hs, srv: srv, as: as, fp: fp, addr: raw.Addr().String(), cfg: cfg}
	go func() {
		if err := hs.Serve(l); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.log.Warn("squatter: server stopped", "err", err)
		}
	}()
	return r
}

// closeRunning stops accepting at once and drops the clients in the background (a client
// goroutine may be waiting for the engine goroutine, which is the caller).
func (s *Squatter) closeRunning(r *running) {
	_ = r.hs.Close()
	go r.srv.Close()
}

// Retry makes the next Apply of an unchanged configuration try again after an error.
func (s *Squatter) Retry() {
	s.mu.Lock()
	if s.state == StateError {
		s.state = ""
	}
	s.mu.Unlock()
}

// Stop shuts everything down (the client is quitting).
func (s *Squatter) Stop() {
	s.Apply(func() Config { c := s.Config(); c.Enabled = false; return c }(), nil)
}

// Addr is the address being listened on ("" when not listening).
func (s *Squatter) Addr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.run == nil {
		return ""
	}
	return s.run.addr
}

// Fingerprint is the TLS certificate fingerprint ("" without TLS or when stopped).
func (s *Squatter) Fingerprint() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.run == nil {
		return ""
	}
	return s.run.fp
}

// State is "off", "listening" or "error", and the error text.
func (s *Squatter) State() (string, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state, s.stateErr
}

// NumPeers is the number of authenticated clients.
func (s *Squatter) NumPeers() int {
	s.pmu.Lock()
	defer s.pmu.Unlock()
	return len(s.peers)
}

func (s *Squatter) peerList() []wire.Peer {
	s.pmu.Lock()
	defer s.pmu.Unlock()
	out := make([]wire.Peer, 0, len(s.peers))
	for _, p := range s.peers {
		out = append(out, p.p)
	}
	return out
}

// ---------------------------------------------------------------- command helpers

// PeerInfo describes a connected client.
type PeerInfo struct {
	DeviceID, Name, Role string
	Since                time.Time
}

// Peers lists the connected clients.
func (s *Squatter) Peers() []PeerInfo {
	s.pmu.Lock()
	defer s.pmu.Unlock()
	var out []PeerInfo
	for _, p := range s.peers {
		out = append(out, PeerInfo{p.p.DeviceID(), p.p.DeviceName(), p.role, p.joined})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Since.Before(out[j].Since) })
	return out
}

// DeviceInfo describes a paired device.
type DeviceInfo struct {
	DeviceID, Name, Role string
	Created, LastSeen    time.Time
	Online               bool
}

func (s *Squatter) store() *auth.Store {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.run != nil {
		return s.run.as
	}
	return nil
}

// Devices lists the paired devices (needs the squatter to be running).
func (s *Squatter) Devices() ([]DeviceInfo, error) {
	as := s.store()
	if as == nil {
		return nil, errors.New("the squatter is not running")
	}
	ds, err := as.Devices()
	if err != nil {
		return nil, err
	}
	online := map[string]bool{}
	for _, p := range s.Peers() {
		online[p.DeviceID] = true
	}
	out := make([]DeviceInfo, 0, len(ds))
	for _, d := range ds {
		role := d.Role
		if role == "" {
			role = "admin"
		}
		out = append(out, DeviceInfo{d.DeviceID, d.Name, role, time.UnixMilli(d.CreatedAt), time.UnixMilli(d.LastSeen), online[d.DeviceID]})
	}
	return out, nil
}

// Kick disconnects the clients whose device name or id (prefix) is name; it returns how many.
func (s *Squatter) Kick(name string) int {
	var victims []wire.Peer
	s.pmu.Lock()
	for _, p := range s.peers {
		if matches(p.p, name) {
			victims = append(victims, p.p)
		}
	}
	s.pmu.Unlock()
	for _, p := range victims {
		p.Close("kicked from gtf")
	}
	return len(victims)
}

func matches(p wire.Peer, name string) bool {
	return name != "" && (strings.EqualFold(p.DeviceName(), name) || strings.HasPrefix(p.DeviceID(), strings.ToLower(name)))
}

// Unpair removes paired devices whose name or id (prefix) is name and disconnects them. It
// returns the names removed.
func (s *Squatter) Unpair(name string) ([]string, error) {
	as := s.store()
	if as == nil {
		return nil, errors.New("the squatter is not running")
	}
	ds, err := as.Devices()
	if err != nil {
		return nil, err
	}
	var removed []string
	for _, d := range ds {
		if strings.EqualFold(d.Name, name) || (len(name) >= 4 && strings.HasPrefix(d.DeviceID, strings.ToLower(name))) {
			if err := as.Revoke(d.DeviceID); err != nil {
				return removed, err
			}
			removed = append(removed, d.Name)
			s.Kick(d.DeviceID)
		}
	}
	return removed, nil
}

// PairInfo is a fresh pairing code with what the client needs to verify the server.
type PairInfo struct {
	Code        string
	Role        string
	Expires     time.Time
	Fingerprint string
	URL         string
}

// Pair creates a single-use pairing code (role "" = squatter_pair_role).
func (s *Squatter) Pair(role string) (PairInfo, error) {
	s.mu.Lock()
	r, cfg := s.run, s.cfg
	s.mu.Unlock()
	if r == nil {
		return PairInfo{}, errors.New("the squatter is not running (/set squatter=on)")
	}
	if role == "" {
		role = cfg.PairRole
	}
	if !ValidRole(role) {
		return PairInfo{}, fmt.Errorf("role must be viewer, operator or admin")
	}
	code, exp, err := r.as.NewPairingRole(cfg.PairTTL, role)
	if err != nil {
		return PairInfo{}, err
	}
	host, _ := os.Hostname()
	if host == "" {
		host = "<this-host>"
	}
	if IsLoopback(cfg.Bind) {
		host = "localhost"
	} else if cfg.Bind != "0.0.0.0" && cfg.Bind != "::" {
		host = cfg.Bind
	}
	scheme := "wss"
	if !cfg.TLS {
		scheme = "ws"
	}
	_, port, _ := net.SplitHostPort(r.addr)
	return PairInfo{Code: code, Role: role, Expires: exp, Fingerprint: r.fp,
		URL: fmt.Sprintf("%s://%s:%s%s", scheme, host, port, cfg.Path())}, nil
}

// Lines formats the pairing information exactly like `squatterd pair` / `tf-squatterd pair`.
func (p PairInfo) Lines() []string {
	l := []string{
		fmt.Sprintf("Pairing code:    %s   (role: %s)", p.Code, p.Role),
		fmt.Sprintf("Valid until:     %s (%s, single use)", p.Expires.Format(time.RFC1123), time.Until(p.Expires).Round(time.Minute)),
	}
	if p.Fingerprint != "" {
		l = append(l, "TLS fingerprint: "+p.Fingerprint)
	}
	return append(l, "Connect to:      "+p.URL)
}

// StatusLines is the text of /squatter status.
func (s *Squatter) StatusLines() []string {
	st, e := s.State()
	cfg := s.Config()
	out := []string{"squatter: " + st}
	if e != "" {
		out = append(out, "  error:       "+e)
	}
	if st == StateListening {
		out = append(out, "  listening:   "+s.Addr()+" ("+cfg.Path()+", "+map[bool]string{true: "TLS", false: "plain ws"}[cfg.TLS]+", "+cfg.Kind+" kind)")
		if fp := s.Fingerprint(); fp != "" {
			out = append(out, "  fingerprint: "+fp)
		}
		out = append(out, "  name:        "+cfg.Name, "  state dir:   "+cfg.Dir)
		out = append(out, fmt.Sprintf("  clients:     %d connected, %d sessions", s.NumPeers(), s.NumSessions()))
	}
	return out
}

// Wait is for tests: it blocks until the listener of the last Apply accepts connections.
func (s *Squatter) Wait(ctx context.Context) error {
	for {
		if a := s.Addr(); a != "" {
			if c, err := net.DialTimeout("tcp", a, 200*time.Millisecond); err == nil {
				c.Close()
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}

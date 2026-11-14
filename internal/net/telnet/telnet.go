// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

// Package telnet implements the telnet protocol state machine (RFC 854/855
// with the RFC 1143 "Q method" for option negotiation) and a registry of
// option handlers. It knows nothing about the interpreter or the UI: it turns
// a byte stream into typed Events and lets protocol extensions (see
// internal/net/proto/...) plug in as Handlers.
package telnet

import (
	"bytes"
	"errors"
	"io"
	"sync"
)

// Command and option bytes.
const (
	SE   = 240
	NOP  = 241
	DM   = 242
	BRK  = 243
	IP   = 244
	AO   = 245
	AYT  = 246
	EC   = 247
	EL   = 248
	GA   = 249
	SB   = 250
	WILL = 251
	WONT = 252
	DO   = 253
	DONT = 254
	IAC  = 255
	EORc = 239 // end-of-record command byte

	OptBinary  = 0
	OptEcho    = 1
	OptSGA     = 3
	OptTType   = 24
	OptEOR     = 25
	OptNAWS    = 31
	OptNewEnv  = 39
	OptCharset = 42
	OptMSDP    = 69
	OptMSSP    = 70
	OptMCCP2   = 86
	OptMXP     = 91
	OptGMCP    = 201
)

// Side says whose option state changed.
type Side int

const (
	Them Side = iota // the server's side (WILL/WONT)
	Us               // our side (DO/DONT)
)

// Event is something that came out of the connection.
type Event interface{}

// Text is a run of application data (still raw bytes; charset decoding is the
// caller's job). It may end mid-line.
type Text struct{ Data []byte }

// Prompt is raised when the server marks the end of a prompt with IAC GA or
// IAC EOR. The text received since the last newline is the prompt.
type Prompt struct{ Kind string } // "GA" or "EOR"

// Subneg is a subnegotiation no handler consumed.
type Subneg struct {
	Opt  byte
	Data []byte
}

// OptionChange reports that an option became enabled or disabled.
type OptionChange struct {
	Opt  byte
	Name string // handler name if known
	Side Side
	On   bool
}

// Message is structured data decoded by an extension protocol handler
// (GMCP, MSDP, MSSP, ...).
type Message struct {
	Proto string // "gmcp", "msdp", "mssp"
	Name  string // GMCP package, MSDP variable, MSSP variable
	Value any    // decoded: string, float64, bool, nil, []any, map[string]any
	Raw   string // raw payload (GMCP JSON text), when meaningful
}

// Policy tells the machine how to react to negotiation for one option.
type Policy struct {
	OfferUs    bool // send WILL when the connection starts
	AskThem    bool // send DO when the connection starts
	AcceptUs   bool // agree when asked DO
	AcceptThem bool // agree when told WILL
}

// Handler is one option plugin.
type Handler interface {
	Option() byte
	Name() string
	Policy() Policy
	// OnNegotiate is called when the option turns on or off for a side.
	OnNegotiate(s *Session, side Side, on bool)
	// OnSubneg is called with the payload of IAC SB <option> ... IAC SE. It
	// returns true if it consumed the subnegotiation.
	OnSubneg(s *Session, data []byte) bool
}

// Sender is implemented by handlers that can send structured data.
type Sender interface {
	Send(s *Session, name string, value any) error
}

var (
	regMu     sync.Mutex
	factories []func() Handler
)

// Register adds a handler factory to the global registry. Protocol packages
// call it from init().
func Register(f func() Handler) {
	regMu.Lock()
	factories = append(factories, f)
	regMu.Unlock()
}

// NewHandlers instantiates every registered handler.
func NewHandlers() []Handler {
	regMu.Lock()
	defer regMu.Unlock()
	out := make([]Handler, 0, len(factories))
	for _, f := range factories {
		out = append(out, f())
	}
	return out
}

// Env is the information handlers need about the local terminal.
type EnvInfo struct {
	TermName   string // e.g. xterm-256color
	ClientName string // advertised first in TTYPE cycling
	ClientVer  string
	Cols, Rows int
	UTF8       bool
	Charset    string // "UTF-8" or "ISO-8859-1"
	Color256   bool
	TrueColor  bool
	TLS        bool
	Extra      map[string]string
}

// Env is a shared, lock-protected EnvInfo.
type Env struct {
	mu sync.Mutex
	EnvInfo
}

// NewEnv returns an Env with tf-like defaults.
func NewEnv() *Env {
	return &Env{EnvInfo: EnvInfo{TermName: "xterm", ClientName: "GTF", ClientVer: "0.1", Cols: 80, Rows: 24, UTF8: true, Charset: "UTF-8", Extra: map[string]string{}}}
}

// Do runs f with the Env locked.
func (e *Env) Do(f func(*EnvInfo)) { e.mu.Lock(); f(&e.EnvInfo); e.mu.Unlock() }

// Snapshot returns a copy of the scalar fields.
func (e *Env) Snapshot() EnvInfo {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.EnvInfo
}

type qs uint8

const (
	qNo qs = iota
	qYes
	qWantNo
	qWantNoOpp
	qWantYes
	qWantYesOpp
)

type optState struct{ us, them qs }

// Machine is the protocol state machine for one connection.
type Machine struct {
	env      *Env
	handlers map[byte]Handler
	order    []Handler

	wmu sync.Mutex
	w   io.Writer

	smu    sync.Mutex
	states [256]optState

	emit     func(Event)
	compress bool

	// scanner state (see the ps* constants) and the text run being collected
	ps    uint8
	cmd   byte // WILL/WONT/DO/DONT waiting for its option byte
	sbOpt byte
	sb    []byte
	txt   []byte
	z     *inflater // the MCCP2 stream being inflated, nil when the stream is raw
}

// NewMachine creates a machine writing to w. If handlers is nil the globally
// registered ones are used.
func NewMachine(w io.Writer, env *Env, handlers []Handler) *Machine {
	if handlers == nil {
		handlers = NewHandlers()
	}
	m := &Machine{env: env, w: w, handlers: map[byte]Handler{}}
	for _, h := range handlers {
		m.handlers[h.Option()] = h
		m.order = append(m.order, h)
	}
	return m
}

// Env returns the machine's environment.
func (m *Machine) Env() *Env { return m.env }

// Handler returns the handler for an option, or nil.
func (m *Machine) Handler(opt byte) Handler { return m.handlers[opt] }

// HandlerByName finds a handler by its Name().
func (m *Machine) HandlerByName(name string) Handler {
	for _, h := range m.order {
		if h.Name() == name {
			return h
		}
	}
	return nil
}

// Session is the narrow view of the machine given to handlers.
type Session struct{ m *Machine }

// Session returns the handler-facing view.
func (m *Machine) Session() *Session { return &Session{m} }

// Env returns the terminal environment.
func (s *Session) Env() *Env { return s.m.env }

// Emit raises an event to the consumer.
func (s *Session) Emit(e Event) { s.m.emitEv(e) }

// Subneg sends IAC SB opt data IAC SE (data is IAC-escaped).
func (s *Session) Subneg(opt byte, data []byte) {
	b := []byte{IAC, SB, opt}
	b = append(b, Escape(data)...)
	b = append(b, IAC, SE)
	s.m.write(b)
}

// Enabled reports the state of an option on a side.
func (s *Session) Enabled(opt byte, side Side) bool { return s.m.enabled(opt, side) }

// Request asks to enable or disable an option on a side (us: WILL/WONT,
// them: DO/DONT) honouring the Q method.
func (s *Session) Request(opt byte, side Side, on bool) { s.m.request(opt, side, on) }

// StartCompression switches the inbound stream to zlib after the current
// subnegotiation (MCCP2).
func (s *Session) StartCompression() { s.m.compress = true }

// Escape doubles IAC bytes.
func Escape(p []byte) []byte {
	if bytes.IndexByte(p, IAC) < 0 {
		return p
	}
	return bytes.ReplaceAll(p, []byte{IAC}, []byte{IAC, IAC})
}

func (m *Machine) emitEv(e Event) {
	if m.emit != nil {
		m.emit(e)
	}
}

func (m *Machine) write(b []byte) error {
	m.wmu.Lock()
	defer m.wmu.Unlock()
	_, err := m.w.Write(b)
	return err
}

// WriteData sends application data, escaping IAC.
func (m *Machine) WriteData(p []byte) error { return m.write(Escape(p)) }

// WriteRaw sends bytes unmodified.
func (m *Machine) WriteRaw(p []byte) error { return m.write(p) }

func (m *Machine) enabled(opt byte, side Side) bool {
	m.smu.Lock()
	defer m.smu.Unlock()
	if side == Us {
		return m.states[opt].us == qYes
	}
	return m.states[opt].them == qYes
}

// Start sends the initial offers/requests every handler's Policy asks for.
func (m *Machine) Start() {
	for _, h := range m.order {
		p := h.Policy()
		if p.OfferUs {
			m.request(h.Option(), Us, true)
		}
		if p.AskThem {
			m.request(h.Option(), Them, true)
		}
	}
}

func (m *Machine) change(opt byte, side Side, on bool) {
	name := ""
	if h := m.handlers[opt]; h != nil {
		name = h.Name()
		defer h.OnNegotiate(&Session{m}, side, on)
	}
	m.emitEv(OptionChange{Opt: opt, Name: name, Side: side, On: on})
}

// request implements "ask for enable/disable" of the Q method.
func (m *Machine) request(opt byte, side Side, on bool) {
	m.smu.Lock()
	st := &m.states[opt].them
	yes, no := byte(DO), byte(DONT)
	if side == Us {
		st = &m.states[opt].us
		yes, no = WILL, WONT
	}
	var send byte
	if on {
		switch *st {
		case qNo:
			*st, send = qWantYes, yes
		case qWantNo:
			*st = qWantNoOpp
		case qWantYesOpp:
			*st = qWantYes
		}
	} else {
		switch *st {
		case qYes:
			*st, send = qWantNo, no
		case qWantNoOpp:
			*st = qWantNo
		case qWantYes:
			*st = qWantYesOpp
		}
	}
	m.smu.Unlock()
	if send != 0 {
		m.write([]byte{IAC, send, opt})
	}
}

// negotiate handles an incoming WILL/WONT/DO/DONT.
func (m *Machine) negotiate(cmd, opt byte) {
	h := m.handlers[opt]
	var pol Policy
	if h != nil {
		pol = h.Policy()
	}
	side := Them
	enable := cmd == WILL
	if cmd == DO || cmd == DONT {
		side = Us
		enable = cmd == DO
	}
	accept := pol.AcceptThem
	yes, no := byte(DO), byte(DONT)
	if side == Us {
		accept = pol.AcceptUs
		yes, no = WILL, WONT
	}
	var send byte
	var changed, newOn bool
	m.smu.Lock()
	st := &m.states[opt].them
	if side == Us {
		st = &m.states[opt].us
	}
	if enable {
		switch *st {
		case qNo:
			if accept {
				*st, send, changed, newOn = qYes, yes, true, true
			} else {
				send = no
			}
		case qWantNo:
			*st = qNo
		case qWantNoOpp:
			*st, changed, newOn = qYes, true, true
		case qWantYes:
			*st, changed, newOn = qYes, true, true
		case qWantYesOpp:
			*st, send = qWantNo, no
		}
	} else {
		switch *st {
		case qYes:
			*st, send, changed, newOn = qNo, no, true, false
		case qWantNo:
			*st, changed, newOn = qNo, true, false
		case qWantNoOpp:
			*st, send = qWantYes, yes
			changed, newOn = true, false
		case qWantYes:
			*st = qNo
		case qWantYesOpp:
			*st = qNo
		}
	}
	m.smu.Unlock()
	if send != 0 {
		m.write([]byte{IAC, send, opt})
	}
	if changed {
		m.change(opt, side, newOn)
	}
}

// ErrSubnegTooLong is returned when a subnegotiation exceeds the sanity limit.
var ErrSubnegTooLong = errors.New("telnet: subnegotiation too long")

const maxSubneg = 1 << 20

// parse states of the byte-stream scanner; they are part of the snapshot, so a
// connection handed to another process can stop in the middle of a sequence.
const (
	psData  uint8 = iota
	psIAC         // after IAC
	psNeg         // after IAC WILL/WONT/DO/DONT: the option byte follows
	psSB          // after IAC SB: the option byte follows
	psSBDat       // inside a subnegotiation
	psSBIAC       // IAC inside a subnegotiation
)

// Run reads r until it fails, calling emit for each Event. It owns the
// reading side; call it from one goroutine. emit is called synchronously.
func (m *Machine) Run(r io.Reader, emit func(Event)) error {
	m.emit = emit
	m.Start()
	buf := make([]byte, 32768)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			if ferr := m.Feed(buf[:n], emit); ferr != nil {
				return ferr
			}
		}
		if err != nil {
			if err == io.EOF && m.z != nil {
				// a compressed stream cut short: nothing more to decode
				m.z = nil
			}
			return err
		}
	}
}

// Feed processes bytes read from the server, calling emit for each event.
// Text is delivered in one event per run (flushed before any other event and
// at the end of the call). A returned error (corrupt zlib stream, oversized
// subnegotiation) ends the connection.
func (m *Machine) Feed(p []byte, emit func(Event)) error {
	m.emit = emit
	defer m.flushText()
	for len(p) > 0 {
		if m.z != nil {
			out, rest, done, err := m.z.feed(p)
			if len(out) > 0 {
				if _, perr := m.scan(out); perr != nil {
					return perr
				}
				m.compress = false // no nested compression
			}
			if err != nil {
				return err
			}
			if !done {
				return nil
			}
			m.z = nil // the compressed stream ended: the raw stream resumes
			p = rest
			continue
		}
		rest, err := m.scan(p)
		if err != nil {
			return err
		}
		if !m.compress {
			return nil
		}
		m.compress = false
		m.z = newInflater()
		p = rest
	}
	return nil
}

func (m *Machine) flushText() {
	if len(m.txt) > 0 {
		t := m.txt
		m.txt = nil
		m.emitEv(Text{Data: t})
	}
}

func (m *Machine) event(e Event) {
	m.flushText()
	m.emitEv(e)
}

// scan runs the scanner over p. It stops early, returning the unread rest,
// when a subnegotiation switched compression on.
func (m *Machine) scan(p []byte) ([]byte, error) {
	for i := 0; i < len(p); i++ {
		c := p[i]
		switch m.ps {
		case psData:
			if c == IAC {
				m.ps = psIAC
			} else if c != 0 {
				m.txt = append(m.txt, c)
			}
		case psIAC:
			m.ps = psData
			switch c {
			case IAC:
				m.txt = append(m.txt, IAC)
			case GA:
				m.event(Prompt{Kind: "GA"})
			case EORc:
				m.event(Prompt{Kind: "EOR"})
			case WILL, WONT, DO, DONT:
				m.cmd, m.ps = c, psNeg
			case SB:
				m.ps = psSB
			default:
				// NOP, DM, BRK, ... ignored
			}
		case psNeg:
			m.ps = psData
			m.flushText()
			m.negotiate(m.cmd, c)
		case psSB:
			m.sbOpt, m.sb, m.ps = c, nil, psSBDat
		case psSBDat:
			if c == IAC {
				m.ps = psSBIAC
				continue
			}
			m.sb = append(m.sb, c)
			if len(m.sb) > maxSubneg {
				return nil, ErrSubnegTooLong
			}
		case psSBIAC:
			switch c {
			case SE:
				m.ps = psData
				data := m.sb
				m.sb = nil
				m.flushText()
				m.subneg(m.sbOpt, data)
				if m.compress {
					return p[i+1:], nil
				}
			case IAC:
				m.sb = append(m.sb, IAC)
				m.ps = psSBDat
			default:
				m.ps = psSBDat
			}
		}
	}
	return nil, nil
}

func (m *Machine) subneg(opt byte, data []byte) {
	if h := m.handlers[opt]; h != nil {
		if h.OnSubneg(&Session{m}, data) {
			return
		}
	}
	m.emitEv(Subneg{Opt: opt, Data: data})
}

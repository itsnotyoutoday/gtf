// Package naws implements Negotiate About Window Size (RFC 1073).
package naws

import "github.com/itsnotyoutoday/gtf/internal/net/telnet"

func init() { telnet.Register(func() telnet.Handler { return &H{} }) }

// H is the handler.
type H struct{}

func (*H) Option() byte          { return telnet.OptNAWS }
func (*H) Name() string          { return "naws" }
func (*H) Policy() telnet.Policy { return telnet.Policy{AcceptUs: true} }

// Payload encodes the NAWS subnegotiation body (IAC bytes are escaped by Subneg).
func Payload(cols, rows int) []byte {
	return []byte{byte(cols >> 8), byte(cols), byte(rows >> 8), byte(rows)}
}

func (*H) OnNegotiate(s *telnet.Session, side telnet.Side, on bool) {
	if side == telnet.Us && on {
		e := s.Env().Snapshot()
		s.Subneg(telnet.OptNAWS, Payload(e.Cols, e.Rows))
	}
}

func (*H) OnSubneg(s *telnet.Session, data []byte) bool { return true }

// Resize is called by the connection when the window changes.
func (*H) Resize(s *telnet.Session, cols, rows int) {
	if s.Enabled(telnet.OptNAWS, telnet.Us) {
		s.Subneg(telnet.OptNAWS, Payload(cols, rows))
	}
}

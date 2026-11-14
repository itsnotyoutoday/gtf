// Package charset implements the TELNET CHARSET option (RFC 2066): when the
// server sends REQUEST with a list of charsets we accept UTF-8 (or ISO-8859-1
// as a fallback) and record the choice in the shared Env.
package charset

import (
	"strings"

	"github.com/itsnotyoutoday/gtf/internal/net/telnet"
)

func init() { telnet.Register(func() telnet.Handler { return &H{} }) }

const (
	request  = 1
	accepted = 2
	rejected = 3
)

// H is the handler.
type H struct{}

func (*H) Option() byte { return telnet.OptCharset }
func (*H) Name() string { return "charset" }
func (*H) Policy() telnet.Policy {
	return telnet.Policy{AcceptUs: true, AcceptThem: true}
}
func (*H) OnNegotiate(s *telnet.Session, side telnet.Side, on bool) {}

// Choose picks a charset from a server offer.
func Choose(offer []string) string {
	for _, want := range []string{"UTF-8", "ISO-8859-1", "US-ASCII"} {
		for _, o := range offer {
			if strings.EqualFold(o, want) {
				return o
			}
		}
	}
	return ""
}

func (*H) OnSubneg(s *telnet.Session, data []byte) bool {
	if len(data) < 1 {
		return true
	}
	switch data[0] {
	case request:
		rest := data[1:]
		if len(rest) >= 1 && strings.HasPrefix(string(rest), "[TTABLE]") { // skip translation table version
			rest = rest[9:]
		}
		if len(rest) == 0 {
			s.Subneg(telnet.OptCharset, []byte{rejected})
			return true
		}
		sep := rest[0]
		offer := strings.Split(string(rest[1:]), string(sep))
		c := Choose(offer)
		if c == "" {
			s.Subneg(telnet.OptCharset, []byte{rejected})
			return true
		}
		s.Subneg(telnet.OptCharset, append([]byte{accepted}, c...))
		s.Env().Do(func(e *telnet.EnvInfo) {
			if strings.EqualFold(c, "UTF-8") {
				e.UTF8, e.Charset = true, "UTF-8"
			} else {
				e.UTF8, e.Charset = false, "ISO-8859-1"
			}
		})
	}
	return true
}

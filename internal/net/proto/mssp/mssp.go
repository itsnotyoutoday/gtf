// Package mssp implements the MUD Server Status Protocol (option 70):
// the server announces VAR/VAL pairs (NAME, PLAYERS, ...). Each pair is
// emitted as telnet.Message{Proto:"mssp"}.
package mssp

import "github.com/itsnotyoutoday/gtf/internal/net/telnet"

func init() { telnet.Register(func() telnet.Handler { return &H{} }) }

// H is the handler.
type H struct{}

func (*H) Option() byte { return telnet.OptMSSP }
func (*H) Name() string { return "mssp" }
func (*H) Policy() telnet.Policy {
	return telnet.Policy{AcceptThem: true}
}
func (*H) OnNegotiate(s *telnet.Session, side telnet.Side, on bool) {}

// Parse decodes VAR name VAL value [VAL value...] into ordered pairs.
func Parse(b []byte) [][2]string {
	var out [][2]string
	const VAR, VAL = 1, 2
	i := 0
	field := func() string {
		j := i
		for i < len(b) && b[i] != VAR && b[i] != VAL {
			i++
		}
		return string(b[j:i])
	}
	for i < len(b) {
		if b[i] != VAR {
			i++
			continue
		}
		i++
		name := field()
		for i < len(b) && b[i] == VAL {
			i++
			out = append(out, [2]string{name, field()})
		}
	}
	return out
}

func (*H) OnSubneg(s *telnet.Session, data []byte) bool {
	for _, kv := range Parse(data) {
		s.Emit(telnet.Message{Proto: "mssp", Name: kv[0], Value: kv[1]})
	}
	return true
}

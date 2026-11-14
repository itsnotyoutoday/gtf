package client

import "strings"

// Help implements gtfparser.HelpHost (the topic text comes from tf-help).
func (c *Client) Help(topic string) { c.showHelp(topic) }

// splitWords splits on blanks, honouring double and single quotes.
func splitWords(s string) []string {
	var out []string
	var cur strings.Builder
	var q byte
	has := false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch {
		case q != 0:
			if ch == q {
				q = 0
			} else {
				cur.WriteByte(ch)
			}
		case ch == '"' || ch == '\'':
			q, has = ch, true
		case ch == ' ' || ch == '\t':
			if cur.Len() > 0 || has {
				out = append(out, cur.String())
				cur.Reset()
				has = false
			}
		default:
			cur.WriteByte(ch)
		}
	}
	if cur.Len() > 0 || has {
		out = append(out, cur.String())
	}
	return out
}

// qw quotes a word for a command line when needed.
func qw(s string) string {
	if s == "" || strings.ContainsAny(s, " \t\"'") {
		return "\"" + strings.ReplaceAll(s, "\"", "\\\"") + "\""
	}
	return s
}

func loginOn(v string) bool { return v != "off" && v != "0" }

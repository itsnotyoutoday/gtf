package client

import (
	"strings"
	"testing"

	"github.com/itsnotyoutoday/gtf/internal/squatter"
	"github.com/itsnotyoutoday/gtfparser"
)

// helpText runs /help and returns what it printed.
func helpText(c *Client, topic string) string {
	var out []string
	c.Do(func(in *gtfparser.Interp) {
		c.capture = &out
		c.showHelp(topic)
		c.capture = nil
	})
	return strings.Join(out, "\n")
}

// /help reads gtf's own help first and the bundled TinyFugue manual second, and says which one a topic came from.
func TestHelpOwnTopicsFirstAndTinyFugueManualLabelled(t *testing.T) {
	c, _, _ := newTestClient(t)
	sq := helpText(c, "squatter")
	if strings.Contains(sq, "bundled TinyFugue manual") || !strings.Contains(sq, "/squatter on") || !strings.Contains(sq, "/pair") {
		t.Fatalf("gtf's own topic, not labelled as TinyFugue's:\n%s", sq)
	}
	aw := helpText(c, "/addworld")
	if !strings.HasPrefix(aw, "% (from the bundled TinyFugue manual") || !strings.Contains(aw, "Defines a new") {
		t.Fatalf("a TinyFugue topic is labelled, then shown:\n%.300s", aw)
	}
	intro := helpText(c, "")
	flat := strings.Join(strings.Fields(intro), " ")
	if !strings.HasPrefix(intro, "What is gtf") || !strings.Contains(flat, "it is not TinyFugue") || strings.Contains(intro, "(from the bundled TinyFugue manual") {
		t.Fatalf("the intro is gtf's own and says gtf is not TinyFugue:\n%s", intro)
	}
	if tf := helpText(c, "tf intro"); !strings.HasPrefix(tf, "% (from the bundled TinyFugue manual") {
		t.Fatalf("/help tf <topic> goes straight to the manual:\n%.200s", tf)
	}
	if cp := helpText(c, "copyright"); !strings.Contains(cp, "Copyright (C) 2026 Rick Bird") || !strings.Contains(cp, "Ken Keys") {
		t.Fatalf("copyright names both:\n%.400s", cp)
	}
	if near := helpText(c, "squatter-"); !strings.Contains(near, "squatter-control") || !strings.Contains(near, "squatter-pairing") {
		t.Fatalf("near matches: %s", near)
	}
	if none := helpText(c, "nosuchtopicxyz"); !strings.Contains(none, "not found") {
		t.Fatal(none)
	}
}

// Every topic the squatter help promises exists and is reachable by the names people type; /help topics lists them by section.
func TestHelpSquatterTopicsExist(t *testing.T) {
	c, _, _ := newTestClient(t)
	h := c.loadHelp()
	if h.own == nil {
		t.Fatal("gtf's help did not load")
	}
	for _, n := range []string{"squatter", "/squatter", "squatter-quickstart", "squatter-address", "squatter-pairing", "/pair", "squatter-forwarding",
		"squatter-control", "squatter-variables", "squatter-security", "squatter-hooks", "squatter-trouble", "tinyfugue", "intro"} {
		if _, ok := h.own.Lookup(n); !ok {
			t.Errorf("gtf help has no topic %q", n)
		}
	}
	// every squatter variable is documented in the settings topic
	st, _ := h.own.Lookup("squatter-variables")
	for _, v := range squatter.ConfigVars {
		if v == "squatter_control" {
			continue // designed, not built yet: documented in "Who types"
		}
		if !strings.Contains(st.Body, "`"+v+"`") {
			t.Errorf("the squatter settings topic does not mention %q", v)
		}
	}
	list := helpText(c, "topics")
	for _, w := range []string{"Remote access", "squatter-quickstart", "tinyfugue"} {
		if !strings.Contains(list, w) {
			t.Errorf("/help topics lacks %q:\n%s", w, list)
		}
	}
}

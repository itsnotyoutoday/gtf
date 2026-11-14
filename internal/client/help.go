package client

import (
	"bufio"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// helpData is the parsed tf-help file (makehelp format: "&topic" header
// lines, a body, repeat).
type helpData struct {
	loaded bool
	path   string
	topics map[string][]string
	order  []string
}

func (c *Client) loadHelp() *helpData {
	h := c.help
	if h.loaded {
		return h
	}
	h.loaded = true
	h.topics = map[string][]string{}
	h.path = c.in.Var("TFHELP")
	if h.path == "" {
		h.path = filepath.Join(c.libDir, "tf-help")
	}
	f, err := os.Open(h.path)
	if err != nil {
		return h
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<16), 1<<20)
	var heads []string
	var body []string
	flush := func() {
		for _, n := range heads {
			h.topics[n] = body
			h.order = append(h.order, n)
		}
		heads, body = nil, nil
	}
	inBody := false
	for sc.Scan() {
		l := sc.Text()
		if strings.HasPrefix(l, "&") {
			if inBody {
				flush()
				inBody = false
			}
			heads = append(heads, strings.ToLower(strings.TrimSpace(l[1:])))
			continue
		}
		inBody = true
		body = append(body, l)
	}
	flush()
	return h
}

// showHelp implements /help.
func (c *Client) showHelp(topic string) {
	h := c.loadHelp()
	topic = strings.ToLower(strings.TrimSpace(topic))
	if len(h.topics) == 0 {
		c.echoLine("% Help file " + h.path + " not found.")
		return
	}
	if topic == "" {
		topic = "intro"
	}
	body, ok := h.topics[topic]
	if !ok {
		for _, alt := range []string{"/" + topic, topic + "()", strings.TrimPrefix(topic, "/")} {
			if b, ok2 := h.topics[alt]; ok2 {
				body, ok = b, true
				break
			}
		}
	}
	if !ok {
		var near []string
		for _, n := range h.order {
			if strings.HasPrefix(n, topic) || strings.HasPrefix(n, "/"+topic) {
				near = append(near, n)
			}
		}
		sort.Strings(near)
		if len(near) > 0 {
			c.echoLine("% Topics matching \"" + topic + "\": " + strings.Join(near, " "))
		} else {
			c.echoLine("% Help on subject " + topic + " not found.")
		}
		return
	}
	for _, l := range body {
		c.echoLine(l)
	}
}

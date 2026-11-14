package gtfparser

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ---------------------------------------------------------------- tfio

type stream struct {
	f  *os.File
	r  *bufio.Reader
	w  bool
	id string
}

func (in *Interp) stream(id string) *stream {
	if in.streams == nil {
		return nil
	}
	return in.streams[id]
}

func fnTfopen(in *Interp, a []Value) (Value, error) {
	path, mode := str(a, 0), str(a, 1)
	if mode == "" {
		mode = "r"
	}
	var f *os.File
	var err error
	if strings.HasPrefix(path, "~/") {
		if h, e := os.UserHomeDir(); e == nil {
			path = filepath.Join(h, path[2:])
		}
	}
	switch mode[0] {
	case 'w':
		f, err = os.Create(path)
	case 'a':
		f, err = os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	default:
		f, err = os.Open(path)
	}
	if err != nil {
		return Int(-1), nil
	}
	if in.streams == nil {
		in.streams = map[string]*stream{}
	}
	in.nextFd++
	id := fmt.Sprint(in.nextFd)
	st := &stream{f: f, w: mode[0] != 'r', id: id}
	if !st.w {
		st.r = bufio.NewReader(f)
	}
	in.streams[id] = st
	return Int(int64(in.nextFd)), nil
}

func fnTfclose(in *Interp, a []Value) (Value, error) {
	id := str(a, 0)
	if id == "o" {
		in.fr.noOut = true
		return Int(1), nil
	}
	if id == "i" || id == "e" {
		return Int(1), nil
	}
	st := in.stream(id)
	if st == nil {
		return Int(0), nil
	}
	st.f.Close()
	delete(in.streams, id)
	return Int(1), nil
}

func fnTfwrite(in *Interp, a []Value) (Value, error) {
	if len(a) == 1 { // tfwrite(text): to tfout
		in.echo(str(a, 0))
		return Int(1), nil
	}
	id, text := str(a, 0), str(a, 1)
	if id == "o" {
		in.echo(text)
		return Int(1), nil
	}
	st := in.stream(id)
	if st == nil || !st.w {
		return Int(0), nil
	}
	_, err := st.f.WriteString(text + "\n")
	return Bool(err == nil), nil
}

func fnTfread(in *Interp, a []Value) (Value, error) {
	if len(a) <= 1 { // tfread([var]): read a line from tfin
		if len(in.tfin) == 0 {
			return Int(-1), nil
		}
		line := in.tfin[0]
		in.tfin = in.tfin[1:]
		if len(a) == 1 {
			in.assign(str(a, 0), Str(line))
			return Int(int64(len(line))), nil
		}
		return Str(line), nil
	}
	st := in.stream(str(a, 0))
	if st == nil || st.r == nil {
		return Int(-1), nil
	}
	line, err := st.r.ReadString('\n')
	if err != nil && line == "" {
		return Int(-1), nil
	}
	line = strings.TrimRight(line, "\r\n")
	if len(a) > 1 { // tfread(fd, var)
		in.assign(str(a, 1), Str(line))
		return Int(int64(len(line))), nil
	}
	return Str(line), nil
}

func fnTfreadable(in *Interp, a []Value) (Value, error) {
	st := in.stream(str(a, 0))
	return Bool(st != nil && st.r != nil), nil
}

// ---------------------------------------------------------------- stubs & small commands

func noop(in *Interp, args string) Value { return Int(1) }

// transform /gag, /hilite, /trig, /hook into /def.
func defShortcut(prefix string) cmdFunc {
	return func(in *Interp, args string) Value {
		w := args
		for strings.HasPrefix(w, "-") {
			_, w = firstWord(w)
		}
		pat, body := w, ""
		if i := strings.Index(w, "="); i >= 0 && prefix != "-h" {
			pat, body = strings.TrimSpace(w[:i]), strings.TrimSpace(w[i+1:])
		}
		spec := prefix + " "
		if prefix == "-h" {
			spec = "-h'" + strings.ReplaceAll(pat, "'", "\\'") + "' = " + body
			return cmdDef(in, spec)
		}
		return cmdDef(in, spec+"-t'"+strings.ReplaceAll(pat, "'", "\\'")+"' = "+body)
	}
}

func cmdList(in *Interp, args string) Value {
	short, inclInvis, onlyInvis := false, false, false
	for {
		w, r := firstWord(args)
		if !strings.HasPrefix(w, "-") || w == "-" {
			break
		}
		args = r
		if w == "--" {
			break
		}
		switch w {
		case "-s":
			short = true
		case "-i":
			inclInvis = true
		case "-I":
			onlyInvis = true
		}
	}
	pat, body := args, ""
	if i := strings.Index(args, "="); i >= 0 {
		pat, body = strings.TrimSpace(args[:i]), strings.TrimSpace(args[i+1:])
	}
	pat = strings.TrimSpace(pat)
	last := 0
	for _, m := range in.Macros() {
		if m.Invisible && !inclInvis && !onlyInvis || !m.Invisible && onlyInvis {
			continue
		}
		if pat != "" && !globMatch(pat, m.Name) || body != "" && !globMatch(body, m.Body) {
			continue
		}
		last = m.ID
		if short {
			in.echo("% /def " + m.Name)
		} else {
			in.echo("% /def " + m.Name + " = " + m.Body)
		}
	}
	return Int(int64(last))
}

func cmdListvar(in *Interp, args string) Value {
	pat := ""
	for strings.HasPrefix(args, "-") {
		_, args = firstWord(args)
	}
	pat = strings.TrimSpace(args)
	var names []string
	for k := range in.globals {
		if pat == "" || globMatch(pat, k) {
			names = append(names, k)
		}
	}
	sort.Strings(names)
	for _, k := range names {
		in.echo("% /set " + k + "=" + in.globals[k].String())
	}
	return Int(int64(len(names)))
}

func cmdPurge(in *Interp, args string) Value {
	n := 0
	pat := strings.TrimSpace(args)
	for _, m := range in.Macros() {
		if pat == "" || globMatch(pat, m.Name) {
			in.Undef(m.Name)
			n++
		}
	}
	return Int(int64(n))
}

func cmdTrigger(in *Interp, args string) Value {
	for strings.HasPrefix(args, "-") {
		_, args = firstWord(args)
	}
	in.ProcessLine(args)
	return Int(1)
}

func cmdVersion(in *Interp, args string) Value {
	in.echo("% " + in.globals["version"].String())
	return Int(1)
}

func init() {
	for _, n := range []string{"beep", "localecho", "listsockets", "listworlds", "liststreams", "status_add", "status_clear",
		"status_edit", "status_rm", "unbind", "bind", "dokey", "histsize", "recordline", "recall", "log", "limit", "unlimit",
		"relimit", "save", "saveworld", "unworld", "renameworld", "suspend", "restrict", "watchdog", "watchname", "input",
		"core", "fg", "connect", "dc", "kill", "ps", "repeat", "quote", "sh", "edit", "reboot", "undefn", "help", "lcd"} {
		builtinCmds[n] = noop
	}
	builtinCmds["list"] = cmdList
	builtinCmds["listvar"] = cmdListvar
	builtinCmds["purge"] = cmdPurge
	builtinCmds["trigger"] = cmdTrigger
	builtinCmds["version"] = cmdVersion
	builtinCmds["gag"] = defShortcut("-ag")
	builtinCmds["hilite"] = defShortcut("-ah")
	builtinCmds["trig"] = defShortcut("")
	builtinCmds["hook"] = defShortcut("-h")
	rawCmds["trigger"] = false

	f := builtinFuncs
	f["tfopen"], f["tfclose"], f["tfwrite"], f["tfread"], f["tfreadable"] = fnTfopen, fnTfclose, fnTfwrite, fnTfread, fnTfreadable
	f["tfflush"] = func(in *Interp, a []Value) (Value, error) { return Int(1), nil }
	f["keycode"] = func(in *Interp, a []Value) (Value, error) { return Str(""), nil }
	f["features"] = func(in *Interp, a []Value) (Value, error) { return Int(0), nil }
	f["world_info"] = func(in *Interp, a []Value) (Value, error) { return Str(""), nil }
	f["nactive"] = func(in *Interp, a []Value) (Value, error) { return Int(0), nil }
	f["nlog"] = f["nactive"]
	f["nmail"] = f["nactive"]
	f["nread"] = f["nactive"]
	f["whatis"] = func(in *Interp, a []Value) (Value, error) { return Str(""), nil }
	f["status_fields"] = func(in *Interp, a []Value) (Value, error) { return Str(""), nil }
	f["status_width"] = func(in *Interp, a []Value) (Value, error) { return Int(0), nil }
	f["winlines"] = func(in *Interp, a []Value) (Value, error) { return Int(24), nil }
	f["moresize"] = func(in *Interp, a []Value) (Value, error) { return Int(0), nil }
	f["morepaused"] = f["moresize"]
	f["morescroll"] = f["moresize"]
	f["log10"] = func(in *Interp, a []Value) (Value, error) { return Real(log10(fnum(a, 0))), nil }
	f["ln"] = f["log"]
	f["tan"] = func(in *Interp, a []Value) (Value, error) { return Real(tan(fnum(a, 0))), nil }
	f["gethostname"] = func(in *Interp, a []Value) (Value, error) { h, _ := os.Hostname(); return Str(h), nil }
	f["filename"] = func(in *Interp, a []Value) (Value, error) {
		p := str(a, 0)
		if strings.HasPrefix(p, "~/") {
			if h, err := os.UserHomeDir(); err == nil {
				p = filepath.Join(h, p[2:])
			}
		}
		return Str(p), nil
	}
	f["pad"] = fnPad
	f["strip_attr"] = func(in *Interp, a []Value) (Value, error) { return Str(str(a, 0)), nil }
	f["ftime"] = func(in *Interp, a []Value) (Value, error) {
		t := time.Now()
		if len(a) > 1 {
			t = time.Unix(int64(fnum(a, 1)), 0)
		}
		return Str(strftime(str(a, 0), t)), nil
	}
	f["mktime"] = func(in *Interp, a []Value) (Value, error) { return Int(time.Now().Unix()), nil }
	f["sidle"] = f["idle"]
	ident := func(in *Interp, a []Value) (Value, error) { return Str(str(a, 0)), nil }
	for _, n := range []string{"decode_attr", "encode_attr", "decode_ansi", "encode_ansi", "strcmpattr"} {
		f[n] = ident
	}
	f["addworld"] = func(in *Interp, a []Value) (Value, error) { return Int(1), nil }
	f["substitute"] = func(in *Interp, a []Value) (Value, error) { return Int(1), nil }
}

// pad(s, n, s, n, ...): each string padded to the width that follows it
// (negative = left-justified).
func fnPad(in *Interp, a []Value) (Value, error) {
	var b strings.Builder
	for i := 0; i+1 < len(a); i += 2 {
		s, w := a[i].String(), int(a[i+1].Int())
		switch {
		case w < 0 && len(s) < -w:
			b.WriteString(s + strings.Repeat(" ", -w-len(s)))
		case w > 0 && len(s) < w:
			b.WriteString(strings.Repeat(" ", w-len(s)) + s)
		default:
			b.WriteString(s)
		}
	}
	if len(a)%2 == 1 {
		b.WriteString(a[len(a)-1].String())
	}
	return Str(b.String()), nil
}

func strftime(f string, t time.Time) string {
	if f == "" {
		f = "%c"
	}
	rep := map[byte]string{'Y': "2006", 'm': "01", 'd': "02", 'H': "15", 'M': "04", 'S': "05", 'y': "06", 'b': "Jan", 'a': "Mon", 'A': "Monday", 'B': "January", 'p': "PM", 'I': "03", 'Z': "MST", 'e': "_2", 'j': "002"}
	var b strings.Builder
	for i := 0; i < len(f); i++ {
		if f[i] == '%' && i+1 < len(f) {
			i++
			if l, ok := rep[f[i]]; ok {
				b.WriteString(t.Format(l))
			} else if f[i] == 'c' {
				b.WriteString(t.Format("Mon Jan _2 15:04:05 2006"))
			} else if f[i] == '%' {
				b.WriteByte('%')
			}
			continue
		}
		b.WriteByte(f[i])
	}
	return b.String()
}

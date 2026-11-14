// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package telnet_test

import (
	"bytes"
	"compress/zlib"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/itsnotyoutoday/gtf/internal/net/proto/mccp2"
	"github.com/itsnotyoutoday/gtf/internal/net/proto/ttype"
	"github.com/itsnotyoutoday/gtf/internal/net/telnet"
)

func newM() *telnet.Machine {
	return telnet.NewMachine(io.Discard, telnet.NewEnv(), []telnet.Handler{&mccp2.H{}, &ttype.H{}})
}

func collect(evs *[]string) func(telnet.Event) {
	return func(e telnet.Event) {
		switch x := e.(type) {
		case telnet.Text:
			// text runs may be cut differently by a handoff: compare the bytes
			if n := len(*evs); n > 0 && strings.HasPrefix((*evs)[n-1], "T:") {
				(*evs)[n-1] += string(x.Data)
				return
			}
			*evs = append(*evs, "T:"+string(x.Data))
		default:
			*evs = append(*evs, fmt.Sprintf("%T:%v", e, e))
		}
	}
}

// stream builds a server's output: negotiation, text, a subnegotiation with an
// escaped IAC, MCCP2 start, a compressed part cut with sync flushes, the end of
// the compressed stream and raw text again.
func stream(t *testing.T) []byte {
	var b bytes.Buffer
	b.Write([]byte{telnet.IAC, telnet.WILL, telnet.OptMCCP2, telnet.IAC, telnet.DO, telnet.OptTType})
	b.WriteString("hello\r\npart")
	b.Write([]byte{telnet.IAC, telnet.SB, telnet.OptTType, 1, telnet.IAC, telnet.IAC, telnet.IAC, telnet.SE})
	b.WriteString("ial line\r\n")
	b.Write([]byte{telnet.IAC, telnet.SB, telnet.OptMCCP2, telnet.IAC, telnet.SE})
	var z bytes.Buffer
	zw := zlib.NewWriter(&z)
	for i := 0; i < 40; i++ {
		fmt.Fprintf(zw, "compressed line %d with some repeated repeated text\r\n", i)
		zw.Flush()
		if i == 20 {
			zw.Write([]byte{telnet.IAC, telnet.WILL, telnet.OptEcho})
		}
	}
	zw.Close()
	b.Write(z.Bytes())
	b.WriteString("raw again\r\n")
	return b.Bytes()
}

func TestSnapshotAtEveryByte(t *testing.T) {
	data := stream(t)
	var want []string
	m := newM()
	if err := m.Feed(data, collect(&want)); err != nil {
		t.Fatal(err)
	}
	if len(want) == 0 {
		t.Fatal("no events")
	}
	for cut := 0; cut <= len(data); cut++ {
		var got []string
		a := newM()
		if err := a.Feed(data[:cut], collect(&got)); err != nil {
			t.Fatalf("cut %d: %v", cut, err)
		}
		js, err := json.Marshal(a.Snapshot())
		if err != nil {
			t.Fatal(err)
		}
		var st telnet.MachineState
		if err := json.Unmarshal(js, &st); err != nil {
			t.Fatal(err)
		}
		b := newM()
		if err := b.Restore(st); err != nil {
			t.Fatalf("cut %d: %v", cut, err)
		}
		if err := b.Feed(data[cut:], collect(&got)); err != nil {
			t.Fatalf("cut %d: %v", cut, err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("cut %d: events differ\n got %q\nwant %q", cut, got, want)
		}
	}
}

func TestRestoreRefusesBadState(t *testing.T) {
	m := newM()
	for name, st := range map[string]telnet.MachineState{
		"too new":  {V: telnet.MachineStateVersion + 1},
		"bad ps":   {V: 1, PS: 99},
		"bad opt":  {V: 1, Opts: []telnet.OptState{{Opt: 1, Us: 9}}},
		"bad zlib": {V: 1, Z: &telnet.ZState{State: 77}},
	} {
		if err := m.Restore(st); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

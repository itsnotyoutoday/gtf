// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package wire

// The server side of the WebSocket protocol (RFC 6455), as much of it as SWP needs: text and binary
// messages, fragmentation, ping/pong, close, no extensions. It replaces coder/websocket on the server
// because a connection that must be carried across a restart of gtf has to give up every byte it has
// read ahead and every half-received message, and a library's private buffers cannot be exported. The
// clients (Dial, and SquatClient) still use the library: both speak the same protocol.

import (
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/coder/websocket"
)

const wsGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

const (
	opCont   = 0x0
	opText   = 0x1
	opBinary = 0x2
	opClose  = 0x8
	opPing   = 0x9
	opPong   = 0xA
)

// errWSPaused is returned by Read when pause stopped it (the connection is intact).
var errWSPaused = errors.New("wire: websocket reader paused")

// CloseError is returned by Read when the peer (or a protocol violation) closed the connection.
type CloseError struct {
	Code   websocket.StatusCode
	Reason string
}

func (e CloseError) Error() string { return fmt.Sprintf("websocket closed: %d %s", e.Code, e.Reason) }

// wsFrag is a fragmented message that has been started and not finished.
type wsFrag struct {
	On   bool   `json:"on,omitempty"`
	Op   byte   `json:"op,omitempty"`
	Data []byte `json:"data,omitempty"`
}

// wsConn is one WebSocket connection (server side).
type wsConn struct {
	nc net.Conn

	rmu    sync.Mutex // one reader at a time; also guards buf and frag
	buf    []byte     // bytes received and not yet consumed
	frag   wsFrag
	limit  atomic.Int64
	paused atomic.Bool

	wmu     sync.Mutex // one frame at a time
	wclosed bool       // a close frame was sent
	dead    atomic.Bool
}

func newWSConn(nc net.Conn, buf []byte, frag wsFrag) *wsConn {
	c := &wsConn{nc: nc, buf: buf, frag: frag}
	c.limit.Store(MaxFrameBytes)
	return c
}

// SetReadLimit bounds the size of one message.
func (c *wsConn) SetReadLimit(n int64) { c.limit.Store(n) }

func headerHas(h http.Header, name, token string) bool {
	for _, v := range h.Values(name) {
		for _, p := range strings.Split(v, ",") {
			if strings.EqualFold(strings.TrimSpace(p), token) {
				return true
			}
		}
	}
	return false
}

// acceptWS answers a WebSocket upgrade request and takes the connection over. On a bad request it
// has already answered and returns an error.
func acceptWS(w http.ResponseWriter, r *http.Request) (*wsConn, error) {
	bad := func(code int, msg string) (*wsConn, error) {
		http.Error(w, msg, code)
		return nil, errors.New("websocket: " + msg)
	}
	if r.Method != http.MethodGet {
		return bad(http.StatusMethodNotAllowed, "method not allowed")
	}
	if !headerHas(r.Header, "Connection", "upgrade") || !headerHas(r.Header, "Upgrade", "websocket") {
		w.Header().Set("Connection", "Upgrade")
		w.Header().Set("Upgrade", "websocket")
		return bad(http.StatusUpgradeRequired, "this is a WebSocket endpoint")
	}
	if r.Header.Get("Sec-WebSocket-Version") != "13" {
		w.Header().Set("Sec-WebSocket-Version", "13")
		return bad(http.StatusUpgradeRequired, "unsupported WebSocket version")
	}
	key := r.Header.Get("Sec-WebSocket-Key")
	if kb, err := base64.StdEncoding.DecodeString(key); err != nil || len(kb) != 16 {
		return bad(http.StatusBadRequest, "bad Sec-WebSocket-Key")
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		return bad(http.StatusInternalServerError, "cannot take the connection over")
	}
	nc, brw, err := hj.Hijack()
	if err != nil {
		return nil, err
	}
	_ = nc.SetDeadline(time.Time{}) // the server's header deadlines are no longer ours
	sum := sha1.Sum([]byte(key + wsGUID))
	resp := "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: " +
		base64.StdEncoding.EncodeToString(sum[:]) + "\r\n\r\n"
	_ = nc.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if _, err := nc.Write([]byte(resp)); err != nil {
		nc.Close()
		return nil, err
	}
	_ = nc.SetWriteDeadline(time.Time{})
	var ahead []byte
	if n := brw.Reader.Buffered(); n > 0 {
		ahead, _ = brw.Reader.Peek(n)
		ahead = append([]byte(nil), ahead...)
	}
	return newWSConn(nc, ahead, wsFrag{}), nil
}

// ---------------------------------------------------------------- reading

func (c *wsConn) fillOnce(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	stop := context.AfterFunc(ctx, func() { _ = c.nc.SetReadDeadline(time.Unix(1, 0)) })
	defer stop()
	tmp := make([]byte, 4096)
	n, err := c.nc.Read(tmp)
	if n > 0 {
		c.buf = append(c.buf, tmp[:n]...)
		return nil
	}
	if err == nil {
		return nil
	}
	if c.paused.Load() {
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() {
			return errWSPaused
		}
	}
	if cerr := ctx.Err(); cerr != nil {
		return cerr
	}
	return err
}

// need makes sure c.buf holds at least n bytes.
func (c *wsConn) need(ctx context.Context, n int) error {
	for len(c.buf) < n {
		if err := c.fillOnce(ctx); err != nil {
			return err
		}
	}
	return nil
}

// protoErr sends a close frame with the code and returns it as the error.
func (c *wsConn) protoErr(code websocket.StatusCode, msg string) error {
	c.sendClose(code, msg)
	return CloseError{code, msg}
}

// readFrame returns the next complete frame. Nothing is consumed from the buffer until the whole
// frame is there, so a pause never leaves half a frame in a place that cannot be exported.
func (c *wsConn) readFrame(ctx context.Context) (fin bool, op byte, payload []byte, err error) {
	if err = c.need(ctx, 2); err != nil {
		return
	}
	b0, b1 := c.buf[0], c.buf[1]
	if b0&0x70 != 0 {
		return false, 0, nil, c.protoErr(websocket.StatusProtocolError, "reserved bits set")
	}
	fin, op = b0&0x80 != 0, b0&0x0f
	if b1&0x80 == 0 {
		return false, 0, nil, c.protoErr(websocket.StatusProtocolError, "client frames must be masked")
	}
	hdr := 2
	plen := uint64(b1 & 0x7f)
	switch plen {
	case 126:
		hdr = 4
	case 127:
		hdr = 10
	}
	if err = c.need(ctx, hdr+4); err != nil {
		return
	}
	switch hdr {
	case 4:
		plen = uint64(binary.BigEndian.Uint16(c.buf[2:4]))
	case 10:
		plen = binary.BigEndian.Uint64(c.buf[2:10])
		if plen>>63 != 0 {
			return false, 0, nil, c.protoErr(websocket.StatusProtocolError, "bad frame length")
		}
	}
	if op >= opClose {
		if !fin || plen > 125 {
			return false, 0, nil, c.protoErr(websocket.StatusProtocolError, "bad control frame")
		}
	} else if plen > uint64(c.limit.Load()) {
		return false, 0, nil, c.protoErr(websocket.StatusMessageTooBig, "message too big")
	}
	total := hdr + 4 + int(plen)
	if err = c.need(ctx, total); err != nil {
		return
	}
	var mask [4]byte
	copy(mask[:], c.buf[hdr:hdr+4])
	payload = make([]byte, plen)
	for i := range payload {
		payload[i] = c.buf[hdr+4+i] ^ mask[i&3]
	}
	c.buf = append(c.buf[:0], c.buf[total:]...)
	return
}

// Read returns the next data message. Control frames are answered here. After errWSPaused the
// connection is intact and Read may be called again (after resumeRead).
func (c *wsConn) Read(ctx context.Context) (websocket.MessageType, []byte, error) {
	c.rmu.Lock()
	defer c.rmu.Unlock()
	for {
		fin, op, p, err := c.readFrame(ctx)
		if err != nil {
			return 0, nil, err
		}
		switch op {
		case opPing:
			if err := c.writeFrame(ctx, opPong, p); err != nil {
				return 0, nil, err
			}
		case opPong:
		case opClose:
			code, reason := websocket.StatusNoStatusRcvd, ""
			if len(p) >= 2 {
				code, reason = websocket.StatusCode(binary.BigEndian.Uint16(p)), string(p[2:])
			}
			c.sendClose(code, "")
			return 0, nil, CloseError{code, reason}
		case opText, opBinary:
			if c.frag.On {
				return 0, nil, c.protoErr(websocket.StatusProtocolError, "new message inside a fragmented one")
			}
			if fin {
				return c.message(op, p)
			}
			c.frag = wsFrag{On: true, Op: op, Data: p}
		case opCont:
			if !c.frag.On {
				return 0, nil, c.protoErr(websocket.StatusProtocolError, "unexpected continuation")
			}
			if int64(len(c.frag.Data))+int64(len(p)) > c.limit.Load() {
				return 0, nil, c.protoErr(websocket.StatusMessageTooBig, "message too big")
			}
			c.frag.Data = append(c.frag.Data, p...)
			if fin {
				m := c.frag
				c.frag = wsFrag{}
				return c.message(m.Op, m.Data)
			}
		default:
			return 0, nil, c.protoErr(websocket.StatusProtocolError, "unknown opcode")
		}
	}
}

func (c *wsConn) message(op byte, p []byte) (websocket.MessageType, []byte, error) {
	if op == opText {
		if !utf8.Valid(p) {
			return 0, nil, c.protoErr(websocket.StatusInvalidFramePayloadData, "text is not UTF-8")
		}
		return websocket.MessageText, p, nil
	}
	return websocket.MessageBinary, p, nil
}

// pauseRead makes a blocked or future Read return errWSPaused without harming the connection.
func (c *wsConn) pauseRead() {
	c.paused.Store(true)
	_ = c.nc.SetReadDeadline(time.Unix(1, 0))
}

// resumeRead undoes pauseRead.
func (c *wsConn) resumeRead() {
	c.paused.Store(false)
	_ = c.nc.SetReadDeadline(time.Time{})
}

// readState is what a reader that has stopped (parked or never started) holds: the bytes received
// and not consumed, and a started fragmented message.
func (c *wsConn) readState() ([]byte, wsFrag) {
	c.rmu.Lock()
	defer c.rmu.Unlock()
	f := c.frag
	f.Data = append([]byte(nil), f.Data...)
	return append([]byte(nil), c.buf...), f
}

// ---------------------------------------------------------------- writing

func (c *wsConn) writeFrame(ctx context.Context, op byte, p []byte) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	if c.wclosed || c.dead.Load() {
		return net.ErrClosed
	}
	return c.writeLocked(ctx, op, p)
}

func (c *wsConn) writeLocked(ctx context.Context, op byte, p []byte) error {
	var hdr [10]byte
	hdr[0] = 0x80 | op
	n := 2
	switch {
	case len(p) < 126:
		hdr[1] = byte(len(p))
	case len(p) <= 0xffff:
		hdr[1] = 126
		binary.BigEndian.PutUint16(hdr[2:], uint16(len(p)))
		n = 4
	default:
		hdr[1] = 127
		binary.BigEndian.PutUint64(hdr[2:], uint64(len(p)))
		n = 10
	}
	frame := make([]byte, 0, n+len(p))
	frame = append(append(frame, hdr[:n]...), p...)
	if d, ok := ctx.Deadline(); ok {
		_ = c.nc.SetWriteDeadline(d)
	} else {
		_ = c.nc.SetWriteDeadline(time.Time{})
	}
	stop := context.AfterFunc(ctx, func() { _ = c.nc.SetWriteDeadline(time.Unix(1, 0)) })
	defer stop()
	_, err := c.nc.Write(frame)
	if err != nil {
		if cerr := ctx.Err(); cerr != nil {
			return cerr
		}
	}
	return err
}

// Write sends one message.
func (c *wsConn) Write(ctx context.Context, typ websocket.MessageType, p []byte) error {
	op := byte(opText)
	if typ == websocket.MessageBinary {
		op = opBinary
	}
	return c.writeFrame(ctx, op, p)
}

func closePayload(code websocket.StatusCode, reason string) []byte {
	if len(reason) > 123 {
		reason = reason[:123]
	}
	b := make([]byte, 2+len(reason))
	binary.BigEndian.PutUint16(b, uint16(code))
	copy(b[2:], reason)
	return b
}

// sendClose writes a close frame once (best effort, short deadline).
func (c *wsConn) sendClose(code websocket.StatusCode, reason string) {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	if c.wclosed || c.dead.Load() {
		return
	}
	c.wclosed = true
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if code == websocket.StatusNoStatusRcvd {
		_ = c.writeLocked(ctx, opClose, nil)
		return
	}
	_ = c.writeLocked(ctx, opClose, closePayload(code, reason))
}

// Close does the closing handshake: it sends the close frame, then waits (briefly) for the peer to
// hang up so that what was sent before reaches it, and closes the connection.
func (c *wsConn) Close(code websocket.StatusCode, reason string) error {
	if c.dead.Load() {
		return nil
	}
	c.sendClose(code, reason)
	_ = c.nc.SetReadDeadline(time.Now().Add(2 * time.Second))
	tmp := make([]byte, 512)
	for {
		if _, err := c.nc.Read(tmp); err != nil {
			break
		}
	}
	return c.CloseNow()
}

// CloseNow drops the connection without a handshake.
func (c *wsConn) CloseNow() error {
	if c.dead.Swap(true) {
		return nil
	}
	return c.nc.Close()
}

// abandon forgets the connection without closing or telling the peer anything: it has been handed to
// another process, which owns the socket now. The caller discards the transport.
func (c *wsConn) abandon() { c.dead.Store(true) }

// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package client

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/itsnotyoutoday/gtf/internal/net/tlsmigrate"
	ftls "github.com/itsnotyoutoday/gtf/internal/net/tlsmigrate/tls"
	"github.com/itsnotyoutoday/gtf/internal/squatter"
)

// squatTLS gives the squatter TLS connections that /restart can carry: the vendored crypto/tls of
// internal/net/tlsmigrate (the squatter itself imports no gtf package, see internal/archtest).
type squatTLS struct{}

var _ squatter.TLSMigrator = squatTLS{}

func (squatTLS) Listen(raw net.Listener, cfg *tls.Config) (net.Listener, error) {
	fc, err := tlsmigrate.ConfigFromStd(cfg)
	if err != nil {
		return nil, err // a configuration it cannot express: the standard library serves it, nothing is carried
	}
	return ftls.NewListener(raw, fc), nil
}

func (squatTLS) Check(nc net.Conn) (bool, error) {
	c, ok := nc.(*ftls.Conn)
	if !ok {
		return false, nil
	}
	if _, ok := c.NetConn().(*net.TCPConn); !ok {
		return true, errors.New("TLS over something other than TCP")
	}
	// the reader is blocked in Read, so ExportState would say "busy": look at what was negotiated
	cs := c.ConnectionState()
	if !cs.HandshakeComplete {
		return true, errors.New("the TLS handshake is not finished")
	}
	if cs.Version == ftls.VersionTLS12 {
		n := ftls.CipherSuiteName(cs.CipherSuite)
		if !strings.Contains(n, "GCM") && !strings.Contains(n, "CHACHA20_POLY1305") {
			return true, fmt.Errorf("TLS 1.2 with %s cannot be migrated (only AEAD suites)", n)
		}
	}
	st, err := c.ExportState() // non-destructive; catches whatever else could never work
	st.Zero()
	if err != nil && !errors.Is(err, ftls.ErrBusy) && !errors.Is(err, ftls.ErrNotQuiescent) {
		return true, err
	}
	return true, nil
}

func (squatTLS) Export(nc net.Conn) ([]byte, *os.File, error) {
	c, ok := nc.(*ftls.Conn)
	if !ok {
		return nil, nil, errors.New("not a migratable TLS connection")
	}
	tcp, ok := c.NetConn().(*net.TCPConn)
	if !ok {
		return nil, nil, errors.New("TLS over something other than TCP")
	}
	st, err := c.ExportState()
	if err != nil {
		if errors.Is(err, ftls.ErrBusy) || errors.Is(err, ftls.ErrNotQuiescent) {
			err = fmt.Errorf("%w: %v", squatter.ErrNotNow, err)
		}
		return nil, nil, err
	}
	blob, err := st.MarshalBinary()
	st.Zero()
	if err != nil {
		return nil, nil, err
	}
	f, err := tcp.File()
	if err != nil {
		clear(blob)
		return nil, nil, err
	}
	return blob, f, nil
}

func (squatTLS) Import(sock net.Conn, blob []byte) (net.Conn, error) {
	st, err := ftls.UnmarshalMigrationState(blob)
	if err != nil {
		return nil, err
	}
	c, err := ftls.ImportState(sock, nil, st)
	st.Zero()
	if err != nil {
		return nil, err
	}
	return c, nil
}

func (squatTLS) Discard(nc net.Conn) error {
	if c, ok := nc.(*ftls.Conn); ok {
		return c.Discard()
	}
	return nc.Close()
}

// Copyright 2026 Andrija Panic and the OpenVDDK contributors
// SPDX-License-Identifier: Apache-2.0

package core

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/openvddk/openvddk/internal/config"
	"github.com/openvddk/openvddk/internal/nfc"
	"github.com/openvddk/openvddk/internal/transport"
	"github.com/openvddk/openvddk/internal/vix"
)

// OpenVix.nbd.sessions is 1 unless the file sets 1 to 16 (D-0045); other
// values keep 1. The other NBD settings are read as before.
func TestNBDConfigSessions(t *testing.T) {
	for _, c := range []struct {
		file     string
		sessions int
	}{
		{"", 1},
		{"OpenVix.nbd.sessions = 4\n", 4},
		{"OpenVDDK.nbd.sessions = 16\n", 16},
		{"OpenVix.nbd.sessions = 1\n", 1},
		{"OpenVix.nbd.sessions = 0\n", 1},
		{"OpenVix.nbd.sessions = 17\n", 1},
		{"OpenVix.nbd.sessions = four\n", 1},
	} {
		if got := nbdConfig(config.Parse([]byte(c.file))).sessions; got != c.sessions {
			t.Errorf("%q: %d sessions, want %d", c.file, got, c.sessions)
		}
	}
	st := nbdConfig(config.Parse([]byte("OpenVix.nbd.inFlight = 32\nOpenVix.nbd.readAheadKB = 0\nOpenVix.nbd.sessions = 2\n")))
	if st.inFlight != 32 || st.raOn || st.sessions != 2 || st.ra != nbdDefaults.ra {
		t.Errorf("settings %+v", st)
	}
}

// VMware's documented vixDiskLib.nfcAio.Session keys are read under their
// own names, in any case (D-0048); out-of-range values leave the host's
// default (0).
func TestNBDConfigAIOBuffers(t *testing.T) {
	for _, c := range []struct {
		file        string
		size, count uint32
	}{
		{"", 0, 0},
		{"vixDiskLib.nfcAio.Session.BufSizeIn64KB = 32\nvixDiskLib.nfcAio.Session.BufCount = 4\n", 2 << 20, 4},
		{"VIXDISKLIB.NFCAIO.SESSION.BUFSIZEIN64KB = \"1\"\n", 64 << 10, 0},
		{"vixDiskLib.nfcAio.Session.BufSizeIn64KB = 33\nvixDiskLib.nfcAio.Session.BufCount = 17\n", 0, 0},
		{"vixDiskLib.nfcAio.Session.BufSizeIn64KB = 0\nvixDiskLib.nfcAio.Session.BufCount = x\n", 0, 0},
	} {
		st := nbdConfig(config.Parse([]byte(c.file)))
		if st.aioBuf != c.size || st.aioCount != c.count {
			t.Errorf("%q: buffers %d x %d, want %d x %d", c.file, st.aioBuf, st.aioCount, c.size, c.count)
		}
	}
}

// VMware's vixDiskLib.nfc.ReadTimeoutMs and vixDiskLib.nfcAio.SocketOption
// keys are read under their own names; invalid values keep the defaults
// (D-0049).
func TestNBDConfigTimeoutAndSockets(t *testing.T) {
	st := nbdConfig(config.Parse([]byte("vixDiskLib.nfc.ReadTimeoutMs = 10000\nvixDiskLib.nfcAio.SocketOption.SndBufSize = 262144\nvixDiskLib.nfcAio.SocketOption.RcvBufSize = 393216\nvixDiskLib.nfcAio.SocketOption.ServerSndBufSize = 524288\nvixDiskLib.nfcAio.SocketOption.ServerRcvBufSize = 786432\n")))
	if st.readTimeout != 10*time.Second || st.sndBuf != 262144 || st.rcvBuf != 393216 || st.serverSndBuf != 524288 || st.serverRcvBuf != 786432 {
		t.Errorf("settings %+v", st)
	}
	st = nbdConfig(config.Parse([]byte("vixDiskLib.nfc.ReadTimeoutMs = 0\nvixDiskLib.nfcAio.SocketOption.SndBufSize = -1\nvixDiskLib.nfcAio.SocketOption.ServerRcvBufSize = x\n")))
	if st.readTimeout != 0 || st.sndBuf != 0 || st.serverRcvBuf != 0 {
		t.Errorf("invalid values: %+v", st)
	}
}

// No connection to the host's NFC port is 14009 at Open, as the reference
// answers whether the port refuses or stays silent (NBD-0011, NBD-0028).
func TestNBDOpenCodeNoConnection(t *testing.T) {
	err := fmt.Errorf("NFC session with h: %w", fmt.Errorf("nfc: connect h:902: %w: %w", nfc.ErrConnect, errors.New("i/o timeout")))
	if c := nbdOpenCode(err); c != vix.EHostConnRefused {
		t.Fatalf("code %d, want 14009", int(c))
	}
}

// A read whose session the host ended (SessionComplete, seen when its NFC
// memory was full) is 1, as the reference answers; a lost connection stays
// 14009 and a refusal keeps its own mapping (NBD-0002, NBD-0033). The note
// naming the likely cause is logged once per disk.
func TestNBDReadCodeSessionEnded(t *testing.T) {
	_, _, _, perr := nfc.ParseAIOHeader([]byte{4, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0})
	if perr == nil {
		t.Fatal("SessionComplete parsed as an AIO header")
	}
	ended := fmt.Errorf("read piece 0+1048576: %w", perr)
	for _, c := range []struct {
		err  error
		want vix.Code
	}{
		{ended, vix.EFail},
		{fmt.Errorf("nfc: reply header: %w", errors.New("read tcp: i/o timeout")), vix.EHostConnRefused},
		{fmt.Errorf("%w: bad frame magic 0x7", nfc.ErrProtocol), vix.EHostConnRefused},
	} {
		if got := nbdReadCode(c.err); got != c.want {
			t.Errorf("nbdReadCode(%v) = %d, want %d", c.err, int(got), int(c.want))
		}
	}
	n := &nbdDisk{}
	n.noteSessionEnded(errors.New("other"))
	if n.ended.Load() {
		t.Fatal("note logged for another error")
	}
	n.noteSessionEnded(ended)
	n.noteSessionEnded(ended)
	if !n.ended.Load() {
		t.Fatal("note not logged")
	}
	var none *nbdDisk
	none.noteSessionEnded(ended) // no disk: nothing to log, no panic
}

// An encrypted disk asked over NBD is read over an encrypted NFC stream, as
// the reference does while it reports NBD; an unencrypted disk over NBD
// stays clear text, and NBDSSL is always encrypted (NBD-0034).
func TestNBDStreamEncrypted(t *testing.T) {
	for _, c := range []struct {
		mode      transport.Mode
		encrypted bool
		want      bool
	}{
		{transport.NBD, false, false},
		{transport.NBD, true, true},
		{transport.NBDSSL, false, true},
		{transport.NBDSSL, true, true},
		{transport.HotAdd, true, false},
	} {
		if got := nbdStreamEncrypted(c.mode, c.encrypted); got != c.want {
			t.Errorf("nbdStreamEncrypted(%s, %v) = %v, want %v", c.mode, c.encrypted, got, c.want)
		}
	}
}

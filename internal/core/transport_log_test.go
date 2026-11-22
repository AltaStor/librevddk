// Copyright 2026 Andrija Panic and the OpenVDDK contributors
// SPDX-License-Identifier: Apache-2.0

package core

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/openvddk/openvddk/internal/hotadd"
	"github.com/openvddk/openvddk/internal/logx"
	"github.com/openvddk/openvddk/internal/transport"
)

type leveled struct {
	level logx.Level
	text  string
}

func captureLevels(t *testing.T) *[]leveled {
	t.Helper()
	var got []leveled
	logx.SetSink(func(l logx.Level, m string) logx.Delivery {
		got = append(got, leveled{l, strings.TrimSuffix(m, "\n")})
		return logx.Delivered
	})
	t.Cleanup(func() { logx.SetSink(nil) })
	return &got
}

// A skipped HotAdd is a quiet note naming the next listed transport (an
// Oracle Linux conversion host under virt-v2v's default list showed it as
// two error lines per disk), and stays a warning as the last listed
// transport; other failures stay warnings. The test checks the exact lines.
func TestTransportFailureLogLevel(t *testing.T) {
	const p = "OpenVDDK: VixDiskLib: VixDiskLib_OpenEx: "
	net := []transport.Mode{transport.NBDSSL, transport.NBD}
	for _, c := range []struct {
		m    transport.Mode
		err  error
		next []transport.Mode
		want logx.Level
		line string
	}{
		{transport.HotAdd, errNotVMwareVM, net, logx.Info,
			p + "HotAdd works only from a VMware virtual machine; this machine is not one. Trying the next listed transport: nbdssl."},
		{transport.HotAdd, errHotAddNeedsVCenter, net, logx.Info,
			p + "HotAdd is available only through vCenter, not on a direct connection to an ESXi host. Trying the next listed transport: nbdssl."},
		{transport.HotAdd, errHotAddNeedsVCenter, net[1:], logx.Info,
			p + "HotAdd is available only through vCenter, not on a direct connection to an ESXi host. Trying the next listed transport: nbd."},
		{transport.HotAdd, errNotVMwareVM, nil, logx.Warn,
			p + "HotAdd works only from a VMware virtual machine; this machine is not one. No other transport is listed."},
		{transport.HotAdd, errHotAddNeedsVCenter, nil, logx.Warn,
			p + "HotAdd is available only through vCenter, not on a direct connection to an ESXi host. No other transport is listed."},
		{transport.HotAdd, fmt.Errorf("%w: no free SCSI slot", hotadd.ErrUnavailable), net, logx.Warn,
			p + "transport hotadd: hotadd unavailable: no free SCSI slot"},
		{transport.NBDSSL, errors.New("host refused TLS"), net[1:], logx.Warn,
			p + "transport nbdssl: host refused TLS"},
	} {
		got := captureLevels(t)
		logTransportFailure(c.m, c.err, c.next)
		if len(*got) != 1 {
			t.Fatalf("%s %v next=%v: %d lines logged, want 1", c.m, c.err, c.next, len(*got))
		}
		if l := (*got)[0]; l.level != c.want || l.text != c.line {
			t.Errorf("%s %v next=%v:\n got level %v %q\nwant level %v %q", c.m, c.err, c.next, l.level, l.text, c.want, c.line)
		}
		logx.SetSink(nil)
	}
}

// The direct-ESXi reason still counts as "HotAdd unavailable", so Open goes
// on to the next listed transport and answers 13 when none is left; the
// not-a-VM reason does not (the reference answers 1 there, NBD-0021).
func TestHotAddSkipReasonsKeepTheirCodes(t *testing.T) {
	if !errors.Is(errHotAddNeedsVCenter, hotadd.ErrUnavailable) {
		t.Error("errHotAddNeedsVCenter no longer counts as hotadd.ErrUnavailable")
	}
	if errors.Is(errNotVMwareVM, hotadd.ErrUnavailable) {
		t.Error("errNotVMwareVM counts as hotadd.ErrUnavailable")
	}
	for _, e := range []error{errHotAddNeedsVCenter, errNotVMwareVM} {
		if s := e.Error(); strings.Contains(strings.ToLower(s), "version") || strings.Contains(s, "D-00") || strings.Contains(s, "yet") {
			t.Errorf("%q: names a version, a record or a promise", s)
		}
	}
}

// An explicit list without hotadd never tries HotAdd, so no HotAdd line can
// be logged; with no list (NULL) HotAdd comes first, followed by the
// network transports, so its skip is a quiet note.
func TestTransportListHotAddPlace(t *testing.T) {
	use, _, err := ParseModes("nbdssl:nbd", true)
	if err != nil || slices.Contains(use, transport.HotAdd) {
		t.Fatalf("explicit nbdssl:nbd: %v %v", use, err)
	}
	use, _, err = ParseModes("", false)
	if err != nil || len(use) < 2 || use[0] != transport.HotAdd {
		t.Fatalf("default list: %v %v", use, err)
	}
}

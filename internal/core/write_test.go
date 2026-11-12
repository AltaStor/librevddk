// Copyright 2026 Norvitex OU (AltaStor.com)
// SPDX-License-Identifier: Apache-2.0

package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unsafe"

	"github.com/openvddk/openvddk/internal/config"
	"github.com/openvddk/openvddk/internal/hotadd"
	"github.com/openvddk/openvddk/internal/lifecycle"
	"github.com/openvddk/openvddk/internal/nfc"
	"github.com/openvddk/openvddk/internal/transport"
	"github.com/openvddk/openvddk/internal/vix"
	"github.com/openvddk/openvddk/internal/vsphere"
)

// withConfig initializes the library with the given configuration file content for the test. The environment of
// the machine running the tests does not switch writes on.
func withConfig(t *testing.T, content string) {
	t.Helper()
	for _, sw := range lifecycle.Switches {
		t.Setenv(sw.Env, "")
	}
	f := filepath.Join(t.TempDir(), "vddk.conf")
	if err := os.WriteFile(f, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if c := lifecycle.Global.Init(lifecycle.Options{Major: 8, ConfigFile: f}); c != vix.OK {
		t.Fatalf("Init: %v", c)
	}
	t.Cleanup(lifecycle.Global.Exit)
}

// An Open for writing is refused before anything is opened on a read-only connection and while writes are off.
func TestOpenForWritingRefusals(t *testing.T) {
	conn := func(readOnly bool, modes ...transport.Mode) uint64 {
		h, err := conns.Add(&Connection{readOnly: readOnly, modes: modes, vm: "vm-1", disks: map[uint64]*Disk{}})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { conns.Remove(h) })
		return h
	}
	withConfig(t, "")
	if _, err := Open(conn(true, transport.NBDSSL), "[ds] a/a.vmdk", 0); vix.ToCode(err) != vix.EInvalidArg {
		t.Fatalf("write open on a read-only connection: %v", err)
	}
	if _, err := Open(conn(false, transport.NBDSSL), "[ds] a/a.vmdk", 0); vix.ToCode(err) != vix.ENotSupported ||
		!strings.Contains(err.Error(), "OpenVDDK.nbd.write") || !strings.Contains(err.Error(), "OPENVIX_NBD_WRITE") {
		t.Fatalf("write open with writes off: %v", err)
	}
	withConfig(t, "OpenVDDK.nbd.write = true\n")
	if _, err := Open(conn(false, transport.HotAdd), "[ds] a/a.vmdk", 0); vix.ToCode(err) != vix.ENotSupported ||
		!strings.Contains(err.Error(), "OpenVDDK.hotadd.write") || !strings.Contains(err.Error(), "OPENVIX_HOTADD_WRITE") {
		t.Fatalf("write open over HotAdd with only NBD writes on: %v", err)
	}
	withConfig(t, "OpenVDDK.hotadd.write = true\n")
	if _, err := Open(conn(false, transport.NBDSSL, transport.NBD), "[ds] a/a.vmdk", 0); vix.ToCode(err) != vix.ENotSupported ||
		!strings.Contains(err.Error(), "OpenVDDK.nbd.write") {
		t.Fatalf("write open over NBDSSL and NBD with only HotAdd writes on: %v", err)
	}
}

// Each family has its own switch, and the transports whose switch is on keep the program's order (D-A001).
func TestWriteModes(t *testing.T) {
	all := []transport.Mode{transport.NBD, transport.HotAdd, transport.NBDSSL}
	for _, c := range []struct{ file, want string }{
		{"OpenVDDK.hotadd.write = true\n", "[hotadd]"},
		{"OpenVDDK.nbd.write = true\n", "[nbd nbdssl]"},
		{"OpenVDDK.nbd.write = true\nOpenVDDK.hotadd.write = true\n", "[nbd hotadd nbdssl]"},
	} {
		withConfig(t, c.file)
		got, err := writeModes(all)
		if err != nil || fmt.Sprint(got) != c.want {
			t.Errorf("%q: transports %v, %v; want %s", c.file, got, err, c.want)
		}
	}
}

// Only a HotAdd that cannot run here moves a write on, and only to a later transport writes go over; NBDSSL never
// moves on to NBD (D-A002 point 3, D-A003).
func TestWriteMovesOn(t *testing.T) {
	hot, ssl, nbd := transport.HotAdd, transport.NBDSSL, transport.NBD
	unavailable := fmt.Errorf("%w: no proxy", hotadd.ErrUnavailable)
	streamTLS := fmt.Errorf("%w: refused", nfc.ErrStreamTLS)
	for _, c := range []struct {
		name     string
		m        transport.Mode
		err      error
		writable []transport.Mode
		rest     []transport.Mode
		want     bool
	}{
		{"HotAdd unavailable, NBDSSL next", hot, unavailable, []transport.Mode{hot, ssl}, []transport.Mode{ssl}, true},
		{"not a VMware VM", hot, errNotVMwareVM, []transport.Mode{hot, nbd}, []transport.Mode{nbd}, true},
		{"connection to an ESXi host", hot, errHotAddNeedsVCenter, []transport.Mode{hot, ssl}, []transport.Mode{ssl}, true},
		{"next transport not writable", hot, unavailable, []transport.Mode{hot}, []transport.Mode{ssl}, false},
		{"nothing listed after HotAdd", hot, unavailable, []transport.Mode{hot, ssl}, nil, false},
		{"HotAdd refused the disk", hot, fmt.Errorf("%w: no disk ID", hotadd.ErrUnsupported), []transport.Mode{hot, ssl}, []transport.Mode{ssl}, false},
		{"NBDSSL refused TLS", ssl, streamTLS, []transport.Mode{ssl, nbd}, []transport.Mode{nbd}, false},
		{"datastore may not hold the write", hot, fmt.Errorf("%w: short", errNoRoom), []transport.Mode{hot, ssl}, []transport.Mode{ssl}, true},
		{"datastore short, nothing after HotAdd", hot, fmt.Errorf("%w: short", errNoRoom), []transport.Mode{hot}, nil, false},
	} {
		if got := writeMovesOn(c.m, c.err, c.writable, c.rest); got != c.want {
			t.Errorf("%s: moves on %v, want %v", c.name, got, c.want)
		}
	}
}

// The scope rules of D-A001 hold for every transport and are checked before any is tried.
func TestWriteScope(t *testing.T) {
	ok := vsphere.DiskDetails{Top: true}
	ok.PoweredOff, ok.BackingKind = true, "flat"
	if err := writeScope(ok, "[ds] a/a.vmdk"); err != nil {
		t.Fatalf("a powered-off VM's top file refused: %v", err)
	}
	for _, c := range []struct {
		name string
		edit func(*vsphere.DiskDetails)
		want string
	}{
		{"snapshot named", func(d *vsphere.DiskDetails) { d.Snapshot = "snapshot-1" }, "without a snapshot named"},
		{"running VM", func(d *vsphere.DiskDetails) { d.PoweredOff = false }, "powered off"},
		{"raw device mapping", func(d *vsphere.DiskDetails) { d.RDM, d.BackingKind = true, "rdm" }, "raw device mapping"},
		{"encrypted", func(d *vsphere.DiskDetails) { d.Encrypted = true }, "encrypted"},
		{"independent", func(d *vsphere.DiskDetails) { d.Independent = true }, "independent"},
		{"other backing", func(d *vsphere.DiskDetails) { d.BackingKind = "vvol" }, "backing"},
		{"below the top", func(d *vsphere.DiskDetails) { d.Top = false }, "not the current top file"},
	} {
		d := ok
		c.edit(&d)
		if err := writeScope(d, "[ds] a/a.vmdk"); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want a refusal naming %q", c.name, err, c.want)
		}
	}
}

// A write open that skips HotAdd goes on only over a transport writes go over, so the log names the next of those
// and not simply the next one listed.
func TestWritableAfter(t *testing.T) {
	hot, ssl, nbd := transport.HotAdd, transport.NBDSSL, transport.NBD
	for _, c := range []struct {
		name     string
		writable []transport.Mode
		m        transport.Mode
		want     string
	}{
		{"NBD listed after HotAdd but not writable", []transport.Mode{hot, ssl}, hot, "[nbdssl]"},
		{"two writable transports after HotAdd", []transport.Mode{hot, ssl, nbd}, hot, "[nbdssl nbd]"},
		{"nothing writable after HotAdd", []transport.Mode{hot}, hot, "[]"},
		{"a transport writes do not go over", []transport.Mode{ssl}, hot, "[]"},
	} {
		if got := fmt.Sprint(writableAfter(c.writable, c.m)); got != c.want {
			t.Errorf("%s: %s, want %s", c.name, got, c.want)
		}
	}
}

// A read-only handle answers writes as the reference answers them (A15) and has nothing to flush.
func TestWriteOnReadOnlyHandle(t *testing.T) {
	d := &Disk{readOnly: true, sectors: 64, path: "[ds] t/t.vmdk", opened: time.Now()}
	h, err := disks.Add(d)
	if err != nil {
		t.Fatal(err)
	}
	defer disks.Remove(h)
	buf := make([]byte, 512)
	if err := Write(h, 0, 1, unsafe.Pointer(&buf[0]), false); vix.ToCode(err) != vix.EFileReadOnly {
		t.Fatalf("Write: %v", err)
	}
	if err := Write(h, 0, 1, unsafe.Pointer(&buf[0]), true); vix.ToCode(err) != vix.EAsyncMixedMode {
		t.Fatalf("WriteAsync after Write: %v", err)
	}
	if err := Flush(h); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if IsWritable(h) {
		t.Fatal("a read-only handle reported writable")
	}
	if err := Write(h+1, 0, 1, unsafe.Pointer(&buf[0]), false); vix.ToCode(err) != vix.EInvalidHandle {
		t.Fatalf("Write on an unknown handle: %v", err)
	}
	if err := Flush(h + 1); vix.ToCode(err) != vix.EInvalidHandle {
		t.Fatalf("Flush on an unknown handle: %v", err)
	}
}

// OpenVDDK.hotadd.write.spaceCheck: refuse by default, warn or off; anything else keeps refuse (D-A004).
func TestSpaceCheckMode(t *testing.T) {
	for _, c := range []struct{ file, want string }{
		{"", "refuse"},
		{"OpenVDDK.hotadd.write.spaceCheck = warn\n", "warn"},
		{"OpenVDDK.hotadd.write.spaceCheck = \"OFF\"\n", "off"},
		{"OpenVDDK.hotadd.write.spaceCheck = refuse\n", "refuse"},
		{"OpenVDDK.hotadd.write.spaceCheck = maybe\n", "refuse"},
	} {
		cfg := config.Parse([]byte(c.file))
		if got := spaceCheckMode(cfg); got != c.want {
			t.Errorf("%q: mode %s, want %s", c.file, got, c.want)
		}
		if len(cfg.Warnings) != 0 {
			t.Errorf("%q: warnings %v", c.file, cfg.Warnings)
		}
	}
}

// Only a file that grows as it is written is checked: thin flat, sparse and SE sparse (D-A004 point 1).
func TestGrowable(t *testing.T) {
	disk := func(kind string, thin bool) vsphere.DiskDetails {
		var d vsphere.DiskDetails
		d.BackingKind, d.Thin, d.Links = kind, thin, 1
		return d
	}
	delta := disk("flat", false)
	delta.Links = 2 // a VMFS 6 delta reported with a flat backing
	for _, c := range []struct {
		d    vsphere.DiskDetails
		want bool
	}{
		{disk("flat", true), true},
		{disk("flat", false), false},
		{disk("sesparse", false), true},
		{disk("sparse", false), true},
		{disk("rdm", false), false},
		{delta, true},
	} {
		if got := growable(c.d); got != c.want {
			t.Errorf("%s thin=%v: growable %v, want %v", c.d.BackingKind, c.d.Thin, got, c.want)
		}
	}
}

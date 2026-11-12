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

	"github.com/openvddk/openvddk/internal/lifecycle"
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

// An Open for writing is refused before anything is opened on a read-only connection, while writes are off, and
// when no transport writes go over is listed.
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
		!strings.Contains(err.Error(), "NBDSSL or NBD") {
		t.Fatalf("write open with only HotAdd listed: %v", err)
	}
}

// Writes go over NBDSSL and NBD once their switch is on, in the program's order; other listed transports are passed
// over.
func TestWriteModes(t *testing.T) {
	withConfig(t, "OpenVDDK.nbd.write = true\n")
	got, err := writeModes([]transport.Mode{transport.NBD, transport.HotAdd, transport.NBDSSL})
	if err != nil || fmt.Sprint(got) != "[nbd nbdssl]" {
		t.Fatalf("transports %v, %v", got, err)
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

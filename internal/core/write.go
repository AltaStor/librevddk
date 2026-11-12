// Copyright 2026 Norvitex OU (AltaStor.com)
// SPDX-License-Identifier: Apache-2.0

package core

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"github.com/openvddk/openvddk/internal/hotadd"
	"github.com/openvddk/openvddk/internal/lifecycle"
	"github.com/openvddk/openvddk/internal/logx"
	"github.com/openvddk/openvddk/internal/transport"
	"github.com/openvddk/openvddk/internal/vix"
	"github.com/openvddk/openvddk/internal/vsphere"
)

// Writes (D-A001): off until a switch outside the program turns them on, and then only to the current disk of a
// powered-off VM opened without a snapshot. Every other handle is read-only and answers as the reference answers on
// a read-only handle.

// writeFamily returns the family whose switch turns writes over m on (D-A001): HotAdd (D-A003), or NBDSSL and NBD
// (D-A002).
func writeFamily(m transport.Mode) (lifecycle.Family, bool) {
	switch m {
	case transport.HotAdd:
		return lifecycle.HotAdd, true
	case transport.NBDSSL, transport.NBD:
		return lifecycle.NBD, true
	}
	return "", false
}

// writeModes returns the listed transports a disk may be written over, in the list's order: those whose family's
// switch is on. When there is none, the reason names the switches of the listed families that are off.
func writeModes(ms []transport.Mode) ([]transport.Mode, error) {
	var out []transport.Mode
	var off []lifecycle.Family
	for _, m := range ms {
		f, ok := writeFamily(m)
		if !ok {
			continue
		}
		if on, _ := lifecycle.Global.Writes(f); on {
			out = append(out, m)
		} else if !slices.Contains(off, f) {
			off = append(off, f)
		}
	}
	switch {
	case len(out) > 0:
		return out, nil
	case len(off) == 0:
		return nil, codeErr(vix.ENotSupported, "writes go over HotAdd, NBDSSL or NBD; the transport list %v has none of them", ms)
	}
	reasons := make([]string, len(off))
	for i, f := range off {
		reasons[i] = lifecycle.OffReason(f)
	}
	return nil, codeErr(vix.ENotSupported, "%s", strings.Join(reasons, "; "))
}

// writeScope applies the rules of D-A001 to an open for writing, before any transport is tried: no snapshot named,
// the VM powered off, the disk's current top file, a dependent flat, sparse or SE sparse disk that is not encrypted.
// Writing a snapshot point, a running VM's disk or a file below the top of the chain corrupts the chain or the VM
// (HOTADD-A001).
func writeScope(det vsphere.DiskDetails, path string) error {
	switch {
	case det.Snapshot != "":
		return fmt.Errorf("a disk is written only without a snapshot named")
	case !det.PoweredOff:
		return fmt.Errorf("a disk is written only while its virtual machine is powered off")
	case det.RDM:
		return fmt.Errorf("a raw device mapping is never written")
	case det.Encrypted:
		return fmt.Errorf("an encrypted disk is never written")
	case det.Independent:
		return fmt.Errorf("an independent disk is never written")
	case det.BackingKind != "flat" && det.BackingKind != "sparse" && det.BackingKind != "sesparse":
		return fmt.Errorf("a disk with %q backing is never written", det.BackingKind)
	case !det.Top:
		return fmt.Errorf("%s is not the current top file of its disk; only that file is written", path)
	}
	return nil
}

// writeMovesOn reports whether a failed open for writing over m goes on to the next listed transport (D-A003): only a
// HotAdd that cannot run on this machine (not a VMware VM, a connection to an ESXi host, no proxy), and only when a
// later listed transport is one writes go over. A write never moves from NBDSSL to NBD, whatever the reason.
func writeMovesOn(m transport.Mode, err error, writable, rest []transport.Mode) bool {
	if m != transport.HotAdd || !(errors.Is(err, hotadd.ErrUnavailable) || errors.Is(err, errNotVMwareVM)) {
		return false
	}
	for _, r := range rest {
		if hasMode(writable, r) {
			return true
		}
	}
	return false
}

// writableAfter returns the transports writes go over that are listed after m, in the list's order: the ones an open
// for writing tries after m, so a skipped HotAdd names the right next one.
func writableAfter(writable []transport.Mode, m transport.Mode) []transport.Mode {
	if i := slices.Index(writable, m); i >= 0 {
		return writable[i+1:]
	}
	return nil
}

// IsWritable reports whether h is a disk handle open for writing.
func IsWritable(h uint64) bool {
	d, ok := disks.Get(h)
	return ok && !d.readOnly
}

// Write implements VixDiskLib_Write and, with async set, the write of VixDiskLib_WriteAsync, which completes on the
// calling thread: as the reference completes it over HotAdd (HOTADD-A001), and on purpose over NBDSSL and NBD, where
// the reference calls back on a thread of its own (NET-A002). The checks run
// in the reference's order: the handle, the I/O mode of the handle's earlier writes, read-only, then the arguments. A
// write past the end is refused before anything is sent, its part inside the disk included (NET-A002).
func Write(h uint64, start, count uint64, buf unsafe.Pointer, async bool) error {
	d, err := getDisk(h, "Write")
	if err != nil {
		return err
	}
	if WriteModeMixed(h, async) {
		return vix.New(vix.EAsyncMixedMode, "Write", nil)
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	switch {
	case d.closed:
		return vix.New(vix.EInvalidHandle, "Write", nil)
	case d.readOnly || (d.att == nil && (d.nbd == nil || d.nbd.w == nil)):
		return vix.New(vix.EFileReadOnly, "Write", nil)
	case count == 0:
		return nil
	case buf == nil:
		return codeErr(vix.EInvalidArg, "NULL buffer")
	case start >= d.sectors || count > d.sectors-start:
		return vix.New(vix.EDiskOutOfRange, "Write", fmt.Errorf("sectors %d+%d outside %d", start, count, d.sectors))
	}
	p := unsafe.Slice((*byte)(buf), count*512)
	t := time.Now()
	if d.att != nil {
		if err := d.att.WriteFull(p, int64(start*512)); err != nil {
			code := vix.ToCode(err)
			verr := vix.New(code, "Write", err)
			why := ""
			if errors.Is(err, syscall.ENOSPC) {
				// The kernel also answers a write to a device removed from the guest this way (HOTADD-A001).
				why = "; no space on the device: the datastore is full, or the device was removed from the worker"
			}
			logx.Write.Warnf("Write %d sectors at %d of %s failed: error %d (%s): %v%s", count, start, d.path, int(code), code.Text(), err, why)
			d.conn.noteFailure(logx.Write, verr)
			return verr
		}
		d.accountWrite(int64(count*512), time.Since(t))
		return nil
	}
	if err := d.nbd.w.write(p, int64(start*512)); err != nil {
		code := nbdWriteCode(err)
		verr := vix.New(code, "Write", err)
		logx.Write.NFC().Warnf("Write %d sectors at %d of %s failed: error %d (%s): %v", count, start, d.path, int(code), code.Text(), err)
		d.conn.noteFailure(logx.Write, verr)
		return verr
	}
	d.accountWrite(int64(count*512), time.Since(t))
	return nil
}

// Flush implements VixDiskLib_Flush: on a disk open for writing it makes what was written durable (fdatasync over
// HotAdd, the flush message over NBDSSL and NBD); a read-only handle has nothing to flush. A buffered write that
// failed on the device is reported here, as the reference reports it (7, HOTADD-A001). Over NBDSSL and NBD, after a
// failed write or flush it returns that error, where the reference returns 0 (NET-A002, D-A002).
func Flush(h uint64) error {
	d, err := getDisk(h, "Flush")
	if err != nil {
		return err
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	if d.closed {
		return vix.New(vix.EInvalidHandle, "Flush", nil)
	}
	if d.readOnly || (d.att == nil && (d.nbd == nil || d.nbd.w == nil)) {
		return nil
	}
	if d.att != nil {
		if err := d.att.Flush(); err != nil {
			code := vix.ToCode(err)
			verr := vix.New(code, "Flush", err)
			logx.Flush.Warnf("Flush of %s failed: error %d (%s): %v; the data written may not be on the datastore", d.path, int(code), code.Text(), err)
			d.conn.noteFailure(logx.Flush, verr)
			return verr
		}
		return nil
	}
	if err := d.nbd.w.flush(); err != nil {
		code := nbdWriteCode(err)
		verr := vix.New(code, "Flush", err)
		logx.Flush.NFC().Warnf("Flush of %s failed: error %d (%s): %v; the data written since the last successful Flush is undefined", d.path, int(code), code.Text(), err)
		d.conn.noteFailure(logx.Flush, verr)
		return verr
	}
	return nil
}

// accountWrite counts a completed write for the statistics logged at Close.
func (d *Disk) accountWrite(n int64, took time.Duration) {
	atomic.AddInt64(&d.stats.writes, 1)
	atomic.AddInt64(&d.stats.wbytes, n)
	atomic.AddInt64(&d.stats.wnanos, int64(took))
}

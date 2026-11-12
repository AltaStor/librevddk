// Copyright 2026 Norvitex OU (AltaStor.com)
// SPDX-License-Identifier: Apache-2.0

package hotadd

import (
	"fmt"

	"github.com/openvddk/openvddk/internal/iogate"
	"github.com/openvddk/openvddk/internal/linuxdev"
	"github.com/openvddk/openvddk/internal/readahead"
	"github.com/openvddk/openvddk/internal/steptime"
)

// Writes over HotAdd (D-A003): only the current disk of a powered-off VM without a snapshot, attached
// independent_persistent, identified by its disk ID, with the host safety check always enforced and no read-ahead.

// Device is a local device opened for writing. Sync makes what was written durable.
type Device interface {
	Reader
	WriteFull(p []byte, off int64) error
	Sync() error
}

// gatedDevice counts each read, write and sync of a device opened for writing with the process's device gate.
type gatedDevice struct {
	Device
	d *iogate.Device
}

func (g gatedDevice) ReadFull(p []byte, off int64) error {
	g.d.Enter()
	defer g.d.Leave()
	return g.Device.ReadFull(p, off)
}

func (g gatedDevice) WriteFull(p []byte, off int64) error {
	g.d.Enter()
	defer g.d.Leave()
	return g.Device.WriteFull(p, off)
}

func (g gatedDevice) Sync() error {
	g.d.Enter()
	defer g.d.Leave()
	return g.Device.Sync()
}

func (g gatedDevice) Close() error {
	g.d.Close()
	return g.Device.Close()
}

// writeRules refuses a write open before anything else is checked, so a refusal names the rule for writes, not the
// one for reads. Writing a snapshot point or a running VM's disk corrupts the chain or the VM (HOTADD-A001).
func writeRules(src SourceDisk) error {
	switch {
	case src.Snapshot != "":
		return fmt.Errorf("%w: a disk is written only without a snapshot named", ErrUnsupported)
	case !src.PoweredOff:
		return fmt.Errorf("%w: a disk is written only while its virtual machine is powered off", ErrUnsupported)
	case src.RDM:
		return fmt.Errorf("%w: a raw device mapping is never written", ErrUnsupported)
	}
	return nil
}

// openWritable opens the verified device for writing, counted with the device gate; reads of a disk being written
// go to the device, without read-ahead.
func (a *Attachment) openWritable(disk linuxdev.Disk, src SourceDisk) (Reader, readahead.Config, error) {
	dev, err := a.h.OpenWritable(disk, disk.WWID, src.CapacityBytes, a.opts.Direct)
	if err != nil {
		return nil, readahead.Config{}, err
	}
	g := gatedDevice{Device: dev, d: devices.Device()}
	a.dev = g
	return g, readahead.Config{}, nil
}

// Writable reports whether the disk is open for writing.
func (a *Attachment) Writable() bool { return a.dev != nil }

// WriteFull writes p at byte offset off, both whole sectors.
func (a *Attachment) WriteFull(p []byte, off int64) error {
	if a.dev == nil {
		return fmt.Errorf("%w: the disk is attached read-only", linuxdev.ErrWriteRefused)
	}
	return a.dev.WriteFull(p, off)
}

// Flush makes what was written durable. A read-only attachment has nothing to flush.
func (a *Attachment) Flush() error {
	if a.dev == nil {
		return nil
	}
	return a.dev.Sync()
}

// flushBeforeClose flushes a disk open for writing before its device is closed. A failed flush does not stop the
// detach; Close reports it.
func (a *Attachment) flushBeforeClose(t *steptime.Timer) error {
	if a.dev == nil {
		return nil
	}
	defer t.Mark("flush")
	if err := a.dev.Sync(); err != nil {
		err = fmt.Errorf("flush before close: %w; the data written may not be on the datastore", err)
		a.opts.warnf("HotAdd: %s: %v", a.entry.Rec.SourcePath, err)
		return err
	}
	return nil
}

// keepIfClaimed checks a written disk after its device was closed: closing it lets udev probe what was written,
// and a disk then claimed on the proxy (LVM, multipath, a mount) is left attached for review, with the record kept,
// instead of being pulled away.
func (a *Attachment) keepIfClaimed() error {
	if a.dev == nil || a.disk.Name == "" {
		return nil
	}
	err := a.claimedAfterWriting()
	if err == nil {
		return nil
	}
	e := a.entry
	e.Rec.LastError = err.Error()
	e.Save()
	e.Release()
	return fmt.Errorf("%w; the disk stays attached and journal %s is kept for review", err, e.Rec.ID)
}

// claimedAfterWriting waits for udev to probe the written device and reports it when something on the proxy has
// claimed it (holders or a mount).
func (a *Attachment) claimedAfterWriting() error {
	a.h.Settle()
	disks, err := a.h.Disks()
	if err != nil {
		return nil // the detach does not depend on the guest's view
	}
	for _, d := range disks {
		if d.Name == a.disk.Name && sameDevice(d, a.disk) &&
			(len(d.Holders) > 0 || a.h.Mounted()[[2]uint32{d.Major, d.Minor}]) {
			return fmt.Errorf("%w: %s was claimed on the proxy after writing (holders %v)", ErrUnsafe, d.Name, d.Holders)
		}
	}
	return nil
}

// warnWritable says, after a writable attachment was recovered, that the process writing the disk did not finish:
// what it wrote may be incomplete. Recovery only detaches; it never touches the disk's files.
func warnWritable(o Options, r Record) {
	if r.Writable {
		o.warnf("HotAdd: %s of VM %s was open for writing when its process ended; the writes may be incomplete and the disk's content is undefined until the copy is run again",
			r.SourcePath, r.SourceVM)
	}
}

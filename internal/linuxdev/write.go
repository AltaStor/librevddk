// Copyright 2026 Norvitex OU (AltaStor.com)
// SPDX-License-Identifier: Apache-2.0

package linuxdev

import (
	"errors"
	"fmt"
	"io"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

// A HotAdd-attached disk opened for writing (D-A003): the stricter open, the writes and the sync.

// ErrWriteRefused reports a write the device does not accept: a read-only open, or a range that is not whole
// logical blocks.
var ErrWriteRefused = errors.New("write refused")

// OpenWritable opens /dev/<d.Name> for writing (D-A003) after the same proof of identity as OpenVerified.
// Writing needs more than reading (CONTRACT.md section 57): the disk must carry the expected disk ID (no
// identification by position) and have 512-byte logical blocks, as there is no read-modify-write. The kernel
// read-only flag is cleared on the device.
func (h Host) OpenWritable(d Disk, wantWWID string, wantBytes int64, direct bool) (*Device, error) {
	if wantWWID == "" {
		return nil, fmt.Errorf("%w: %s carries no disk ID; a disk is written only when identified by its ID", ErrIdentity, d.Name)
	}
	if d.LogicalBlock != 512 {
		return nil, fmt.Errorf("%w: %s has %d-byte logical blocks; writes need 512", ErrWriteRefused, d.Name, d.LogicalBlock)
	}
	return h.open(d, wantWWID, wantBytes, direct, true)
}

// WriteFull writes p at offset off, both whole logical blocks. It never reports a short write as success.
// Without O_DIRECT the data is acknowledged once the kernel has it; Sync makes it durable.
func (dev *Device) WriteFull(p []byte, off int64) error {
	if !dev.writable {
		return fmt.Errorf("%w: %s is open read-only", ErrWriteRefused, dev.path)
	}
	if off < 0 || off > dev.size || int64(len(p)) > dev.size-off {
		return fmt.Errorf("write %d bytes at %d outside device of %d bytes", len(p), off, dev.size)
	}
	b := int64(dev.block)
	if off%b != 0 || int64(len(p))%b != 0 {
		return fmt.Errorf("%w: %d bytes at %d are not whole %d-byte blocks", ErrWriteRefused, len(p), off, b)
	}
	if !dev.direct || dev.aligned(p, off) {
		return pwriteFull(dev.f, p, off)
	}
	return dev.writeBounced(p, off)
}

// writeBounced writes an O_DIRECT request whose buffer is not aligned through the aligned bounce buffer.
func (dev *Device) writeBounced(p []byte, off int64) error {
	dev.bounceMu.Lock()
	defer dev.bounceMu.Unlock()
	if dev.bounce == nil {
		dev.bounce = alignedBuffer(bounceSize, 4096)
	}
	for len(p) > 0 {
		n := copy(dev.bounce, p)
		if err := pwriteFull(dev.f, dev.bounce[:n], off); err != nil {
			return err
		}
		p = p[n:]
		off += int64(n)
	}
	return nil
}

// Sync makes the data written so far durable: fdatasync, which also asks the disk to flush its write cache.
func (dev *Device) Sync() error {
	if !dev.writable {
		return nil
	}
	if err := unix.Fdatasync(int(dev.f.Fd())); err != nil {
		return &os.PathError{Op: "fdatasync", Path: dev.path, Err: err}
	}
	return nil
}

func pwriteFull(f *os.File, p []byte, off int64) error {
	for len(p) > 0 {
		n, err := syscall.Pwrite(int(f.Fd()), p, off)
		if err == syscall.EINTR {
			continue
		}
		if err != nil {
			return &os.PathError{Op: "pwrite", Path: f.Name(), Err: err}
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		p = p[n:]
		off += int64(n)
	}
	return nil
}

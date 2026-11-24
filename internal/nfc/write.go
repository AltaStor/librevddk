// Copyright 2026 Norvitex OU (AltaStor.com)
// SPDX-License-Identifier: Apache-2.0

package nfc

import (
	"encoding/binary"
	"fmt"
	"io"
	"strings"
	"time"
)

// Writes over NBDSSL and NBD (D-A002): the messages VMware's library sends to write a disk (NET-A001) and the host's
// answers when a write fails (NET-A002). Only the requests of a write open use the handling here; reads keep theirs.

const (
	aioDDBSet = 10 // descriptor key set, before the first write (NET-A001)

	openFlagsWrite = 0x1a // VMware's library's flags for a write open (NET-A001)

	// The direction of an I/O request (u32 at offset 8, NET-A001).
	ioDirWrite = 0
	ioDirFlush = 2

	maxErrorText = 64 * 1024
	// maxErrorFrames bounds the ERROR frames one request may get: the host sends one per failed piece, 60 for a
	// 4 MiB write in NET-A002.
	maxErrorFrames = 1024

	// MaxWriteRequest is the largest write request sent, the largest the reference was seen to send; WritePiece is
	// the largest piece of it, the host's default AIO buffer (NET-A001, D-A002).
	MaxWriteRequest = 4 << 20
	WritePiece      = 64 << 10
)

// ErrHostEnded: the host sent the end-of-session frame on its own, as it does after a failed write request
// (NET-A002). It matches ErrSessionEnded, so a read that meets it answers as a read of a read-only open does.
var ErrHostEnded = fmt.Errorf("nfc: %w", ErrSessionEnded)

// writeOp is what a request of a write open adds to an Op.
type writeOp struct {
	// ctl: a write or flush request (direction dir), whose reply carries no data; off and n are a write's offset
	// and length in bytes.
	ctl bool
	dir uint32
	off uint64
	n   uint32

	// The request stays pending after ERROR frames until the reply that completes it (NET-A001, NET-A002); hostErr
	// is the first of those frames, and the request's result whatever the reply says.
	hostErr *HostError
	frames  int // ERROR frames received for the request
}

// HostError is an ERROR frame from the host (NET-A001, NET-A002).
type HostError struct {
	// Kind is the first u32. Seen: 10 in a failed write, 14 in a flush that ran out of space, 13 at an open refused
	// for a lock.
	Kind uint32
	// Status is the second u32; its bits 8 to 23 are the host's disk-library code (13: no space; 16392: a file it
	// cannot lock).
	Status  uint32
	Text    string // the host's own text, made printable
	ReqType uint32 // the type of the request refused
}

func (e *HostError) Error() string {
	msg := fmt.Sprintf("nfc: the host refused the request (type %d): error kind %d, disk-library code %d", e.ReqType, e.Kind, e.DiskLibCode())
	if e.Text != "" {
		msg += ": " + e.Text
	}
	return msg
}

// Unwrap makes a HostError match ErrRefused.
func (e *HostError) Unwrap() error { return ErrRefused }

// DiskLibCode is the host's disk-library code in Status.
func (e *HostError) DiskLibCode() uint32 { return e.Status >> 8 & 0xFFFF }

// NoSpace reports a refusal for lack of space on the datastore: the disk-library code 13 of a failed write, or the
// kind 14 of a flush (NET-A002).
func (e *HostError) NoSpace() bool { return e.DiskLibCode() == 13 || e.Kind == 14 }

// ParseErrorReply reads an ERROR frame's payload: kind, status, a zero word and the length of the host's text that
// follows the payload. A payload shorter than 16 bytes carries no text.
func ParseErrorReply(r []byte) (*HostError, uint32, error) {
	e := &HostError{}
	if len(r) >= 8 {
		e.Kind, e.Status = binary.LittleEndian.Uint32(r[0:]), binary.LittleEndian.Uint32(r[4:])
	}
	if len(r) < 16 {
		return e, 0, nil
	}
	n := binary.LittleEndian.Uint32(r[12:])
	if n > maxErrorText {
		return nil, 0, fmt.Errorf("%w: error text of %d bytes", ErrProtocol, n)
	}
	return e, n, nil
}

// ParseWriteReply checks the reply to a write or flush request: its direction, and for a write the request's offset
// and length (NET-A001). The u32 at offset 28 is not checked: the reference's replies carry the low bits of the
// offset there.
func ParseWriteReply(r []byte, dir uint32, off uint64, n uint32) error {
	if len(r) < ioPayloadSize {
		return fmt.Errorf("%w: write reply of %d bytes", ErrProtocol, len(r))
	}
	if d := binary.LittleEndian.Uint32(r[8:]); d != dir {
		return fmt.Errorf("%w: reply of direction %d to a request of direction %d", ErrProtocol, d, dir)
	}
	if dir != ioDirWrite {
		return nil
	}
	if o := binary.LittleEndian.Uint64(r[16:]); o != off {
		return fmt.Errorf("%w: write reply for offset %d, sent %d", ErrProtocol, o, off)
	}
	if l := binary.LittleEndian.Uint32(r[24:]); l != n {
		return fmt.Errorf("%w: write reply for %d bytes, sent %d", ErrProtocol, l, n)
	}
	return nil
}

// printable returns the host's text with every byte outside printable ASCII replaced, at most 512 bytes of it.
func printable(b []byte) string {
	b = b[:min(len(b), 512)]
	out := make([]byte, len(b))
	for i, x := range b {
		if x < 0x20 || x > 0x7e {
			x = '?'
		}
		out[i] = x
	}
	return strings.TrimRight(string(out), "?")
}

// hostEnded reports the classic end-of-session frame in place of an AIO header, which the host sends after a failed
// write request (NET-A002). Only a session that opened a disk for writing looks for it.
func (c *Conn) hostEnded(h []byte) bool {
	return c.writing.Load() && binary.LittleEndian.Uint32(h) == msgSessionComplete
}

// writeRefused handles an ERROR frame for request o of a write open: it reads the host's text and keeps the first
// refusal as the request's result. The request stays pending for its reply, unless the session took it already.
// It returns false when the session failed.
func (c *Conn) writeRefused(op uint32, o *Op, p []byte) bool {
	e, textLen, err := ParseErrorReply(p)
	if err != nil {
		c.fail(err)
		return false
	}
	e.ReqType = o.typ
	if textLen > 0 {
		t := make([]byte, textLen)
		if _, err := io.ReadFull(c.br, t); err != nil {
			c.fail(fmt.Errorf("nfc: error text: %w", err))
			return false
		}
		e.Text = printable(t)
	}
	c.mu.Lock()
	pending := c.pending[op] == o
	if pending && o.w.hostErr == nil {
		o.w.hostErr = e
	}
	o.w.frames++
	flood := o.w.frames > maxErrorFrames
	c.mu.Unlock()
	if flood {
		// Each frame restarts the wait for the reply: a host that never stops sending them would hold the call
		// for ever.
		c.fail(fmt.Errorf("%w: more than %d error frames for one request (op %d)", ErrProtocol, maxErrorFrames, op))
		return false
	}
	if !pending {
		c.complete(op, o, e)
	}
	return true
}

// OpenDiskWritable opens a disk file for writing by its datastore path, with the flags VMware's library sends
// (NET-A001), and returns the host's handle for it. A host that cannot lock the file answers with an ERROR frame and
// then a zero reply; both are read, and the open fails with the host's refusal.
func (c *Conn) OpenDiskWritable(path string) (uint64, error) {
	if path == "" || len(path) > maxPathLen {
		return 0, fmt.Errorf("nfc: bad path length %d", len(path))
	}
	c.writing.Store(true)
	p := make([]byte, 60)
	binary.LittleEndian.PutUint32(p[0:], uint32(len(path)))
	binary.LittleEndian.PutUint32(p[16:], fileTypeDisk)
	binary.LittleEndian.PutUint32(p[20:], openFlagsWrite)
	o := Op{w: &writeOp{}}
	if err := c.roundTrip(&o, aioOpenFile, p, []byte(path)); err != nil {
		return 0, err
	}
	return ParseOpenReply(o.payload)
}

// DDBSet sets a descriptor key of a disk open for writing (type 10, NET-A001): the key and its value follow the
// payload, without separators.
func (c *Conn) DDBSet(handle uint64, key, value string) error {
	if key == "" || len(key) > maxKeyLen || len(value) > maxDDBValue {
		return fmt.Errorf("nfc: bad key of %d bytes or value of %d bytes", len(key), len(value))
	}
	p := make([]byte, ddbPayloadSize)
	binary.LittleEndian.PutUint64(p[0:], handle)
	binary.LittleEndian.PutUint32(p[8:], uint32(len(key)))
	binary.LittleEndian.PutUint32(p[12:], uint32(len(value)))
	o := Op{w: &writeOp{}}
	return c.roundTrip(&o, aioDDBSet, p, []byte(key+value))
}

// Write sends one write request of len(data) bytes at byte offset off (both whole sectors, at most MaxWriteRequest
// bytes) in pieces of at most WritePiece bytes, each with its own header and the request's operation number, and
// waits for the request's one reply (NET-A001). The request has failed when the host sent an ERROR frame for it,
// whatever the reply says (NET-A002).
func (c *Conn) Write(handle, off uint64, data []byte) error {
	n := len(data)
	if n == 0 || n > MaxWriteRequest || n%sectorSize != 0 || off%sectorSize != 0 {
		return fmt.Errorf("nfc: bad write of %d bytes at %d", n, off)
	}
	o := &Op{w: &writeOp{ctl: true, dir: ioDirWrite, off: off, n: uint32(n)}}
	err := c.submitWith(o, aioIO, func(op uint32) error {
		for po := 0; po < n; po += WritePiece {
			pl := min(WritePiece, n-po)
			p := make([]byte, ioPayloadSize)
			binary.LittleEndian.PutUint64(p[0:], handle)
			binary.LittleEndian.PutUint32(p[8:], ioDirWrite)
			binary.LittleEndian.PutUint64(p[16:], off)
			binary.LittleEndian.PutUint32(p[24:], uint32(n))
			binary.LittleEndian.PutUint32(p[28:], uint32(po))
			binary.LittleEndian.PutUint32(p[32:], uint32(pl))
			binary.LittleEndian.PutUint32(p[36:], uint32(pl))
			_ = c.c.SetWriteDeadline(time.Now().Add(c.timeout))
			// The piece header, then its data in a separate write.
			if _, err := c.c.Write(append(aioHeader(aioIO, ioPayloadSize, op), p...)); err != nil {
				return err
			}
			if _, err := c.c.Write(data[po : po+pl]); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	return o.Wait()
}

// Flush asks the host to make the writes to the file durable (type 7, direction 2, NET-A001) and waits for its
// reply. A flush the host refuses leaves the session working (NET-A002).
func (c *Conn) Flush(handle uint64) error {
	p := make([]byte, ioPayloadSize)
	binary.LittleEndian.PutUint64(p[0:], handle)
	binary.LittleEndian.PutUint32(p[8:], ioDirFlush)
	o := &Op{w: &writeOp{ctl: true, dir: ioDirFlush}}
	return c.roundTrip(o, aioIO, p, nil)
}

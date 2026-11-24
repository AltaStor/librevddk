// Copyright 2026 Norvitex OU (AltaStor.com)
// SPDX-License-Identifier: Apache-2.0

package nfc

import (
	"encoding/binary"
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

// Fuzz targets for what a host sends to a session that opened a disk for writing (D-A002, §75).

// FuzzParseErrorReply: an ERROR frame's payload is either a protocol error or a refusal whose text is within the
// bound, and the host's text is made printable whatever it holds.
func FuzzParseErrorReply(f *testing.F) {
	p, text := hostError(10, 0x00000d0a, "no space on the file system")
	f.Add(append(p, text...))
	f.Add(make([]byte, 8))
	f.Add([]byte{1, 2, 3})
	f.Fuzz(func(t *testing.T, b []byte) {
		e, n, err := ParseErrorReply(b)
		if err != nil {
			if !errors.Is(err, ErrProtocol) {
				t.Fatalf("error %v is not a protocol error", err)
			}
		} else if e == nil || n > maxErrorText || (len(b) < 16 && n != 0) {
			t.Fatalf("accepted %x: refusal %v, text of %d bytes", b, e, n)
		} else {
			_ = e.Error()
		}
		s := printable(b)
		if len(s) > 512 {
			t.Fatalf("text of %d bytes kept", len(s))
		}
		for i := 0; i < len(s); i++ {
			if s[i] < 0x20 || s[i] > 0x7e {
				t.Fatalf("byte %#x kept in %q", s[i], s)
			}
		}
	})
}

// FuzzParseWriteReply: a reply is accepted only when it is whole and has the request's direction, and for a write
// the request's offset and length.
func FuzzParseWriteReply(f *testing.F) {
	r := make([]byte, ioPayloadSize)
	binary.LittleEndian.PutUint32(r[24:], 512)
	f.Add(r, uint32(ioDirWrite), uint64(0), uint32(512))
	f.Add(r, uint32(ioDirFlush), uint64(0), uint32(0))
	f.Add(r[:20], uint32(ioDirWrite), uint64(0), uint32(512))
	f.Fuzz(func(t *testing.T, b []byte, dir uint32, off uint64, n uint32) {
		err := ParseWriteReply(b, dir, off, n)
		if err != nil {
			if !errors.Is(err, ErrProtocol) {
				t.Fatalf("error %v is not a protocol error", err)
			}
			return
		}
		if len(b) < ioPayloadSize || binary.LittleEndian.Uint32(b[8:]) != dir {
			t.Fatalf("accepted %x for direction %d", b, dir)
		}
		if dir == ioDirWrite && (binary.LittleEndian.Uint64(b[16:]) != off || binary.LittleEndian.Uint32(b[24:]) != n) {
			t.Fatalf("accepted %x for a write of %d bytes at %d", b, n, off)
		}
	})
}

// writeReply builds the host's reply to write or flush request op: the AIO header and the I/O payload.
func writeReply(op, dir uint32, off uint64, n uint32) []byte {
	p := make([]byte, ioPayloadSize)
	binary.LittleEndian.PutUint32(p[8:], dir)
	binary.LittleEndian.PutUint64(p[16:], off)
	binary.LittleEndian.PutUint32(p[24:], n)
	return append(aioHeader(aioIO, len(p), op), p...)
}

// errorFrame builds an ERROR frame for request op, followed by the host's text.
func errorFrame(op, kind, status uint32, text string) []byte {
	p, tx := hostError(kind, status, text)
	return append(append(aioHeader(aioError, len(p), op), p...), tx...)
}

// cat joins frames into what the host sends.
func cat(frames ...[]byte) []byte {
	var b []byte
	for _, f := range frames {
		b = append(b, f...)
	}
	return b
}

// FuzzEngineWrite: whatever the host sends in reply to a write and a flush of a disk open for writing (ERROR frames
// and their text, replies, the end-of-session frame), the session never panics, both calls return within the
// session's timeout, and the receiver ends.
func FuzzEngineWrite(f *testing.F) {
	const n = 4096
	nospace := errorFrame(0, 10, 0x00000d0a, "no space on the file system")
	f.Add(cat(writeReply(0, ioDirWrite, 0, n), writeReply(1, ioDirFlush, 0, 0)))
	f.Add(cat(nospace, nospace, writeReply(0, ioDirWrite, 0, n), classic(msgSessionComplete))) // NET-A002
	f.Add(cat(writeReply(0, ioDirWrite, 0, n), errorFrame(1, 14, 0x00000d0a, "no space"), writeReply(1, ioDirFlush, 0, 0)))
	f.Add(writeReply(0, ioDirWrite, 512, n))          // a reply for another offset
	f.Add(writeReply(0, ioDirFlush, 0, 0))            // a reply of another direction
	f.Add(errorFrame(0, 10, 0x400, "failed")[:40])    // cut short in the text
	f.Add(cat(errorFrame(0, 10, 0x400, ""), nospace)) // an ERROR frame without text
	f.Add(classic(msgSessionComplete))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		local, peer := net.Pipe()
		defer peer.Close()
		c := &Conn{c: local, timeout: 2 * time.Second}
		c.writing.Store(true)
		c.start(1)
		go func() {
			// The host answers once the write's first frame has arrived, so that the write is pending.
			if _, err := io.ReadFull(peer, make([]byte, aioHeaderSize)); err != nil {
				return
			}
			go io.Copy(io.Discard, peer) // the rest of the requests
			_, _ = peer.Write(data)
			peer.Close()
		}()
		calls := make(chan struct{})
		go func() {
			_ = c.Write(1, 0, make([]byte, n))
			_ = c.Flush(1)
			close(calls)
		}()
		select {
		case <-calls:
		case <-time.After(10 * time.Second):
			t.Fatal("a write or a flush never returned")
		}
		local.Close()
		select {
		case <-c.recvd:
		case <-time.After(10 * time.Second):
			t.Fatal("the receiver did not end")
		}
	})
}

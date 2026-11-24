// Copyright 2026 Norvitex OU (AltaStor.com)
// SPDX-License-Identifier: Apache-2.0

package nfc

import (
	"errors"
	"testing"
)

// openWrite opens a session to f and the fake disk for writing.
func openWrite(t *testing.T, f *fakeHost) (*Conn, uint64, chan error) {
	t.Helper()
	tk, addr, done := startFake(t, f)
	c, err := Dial(t.Context(), addr, tk, Options{SSL: f.ssl, Operation: "nbdmode"})
	if err != nil {
		t.Fatal(err)
	}
	h, err := c.OpenDiskWritable("[ds] vm/vm.vmdk")
	if err != nil {
		t.Fatal(err)
	}
	if f.gotFlags != openFlagsWrite {
		t.Fatalf("open flags %#x, want %#x", f.gotFlags, openFlagsWrite)
	}
	return c, h, done
}

// pattern is the data written in the tests: different in every byte position.
func pattern(n int, seed byte) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i>>9) ^ byte(i) ^ seed
	}
	return b
}

func testWriteSession(t *testing.T, ssl bool) {
	f := newFakeHost(t)
	f.ssl = ssl
	c, h, done := openWrite(t, f)
	if err := c.DDBSet(h, "longContentID", "0123456789abcdef0123456789abcdef"); err != nil {
		t.Fatal(err)
	}
	big, small := pattern(MaxWriteRequest, 1), pattern(64<<10, 2)
	if err := c.Write(h, 1<<20, big); err != nil {
		t.Fatal(err)
	}
	if err := c.Write(h, 9<<30, small); err != nil {
		t.Fatal(err)
	}
	if err := c.Flush(h); err != nil {
		t.Fatal(err)
	}
	if err := c.CloseDisk(h); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if got := f.set["longContentID"]; got != "0123456789abcdef0123456789abcdef" {
		t.Errorf("content ID set to %q", got)
	}
	if f.pieces != 65 || f.flushes != 1 {
		t.Errorf("%d pieces and %d flushes, want 65 and 1", f.pieces, f.flushes)
	}
	for i, b := range big {
		if f.written[1<<20+uint64(i)] != b {
			t.Fatalf("byte %d of the 4 MiB write differs on the host", i)
		}
	}
	for i, b := range small {
		if f.written[9<<30+uint64(i)] != b {
			t.Fatalf("byte %d of the 64 KiB write differs on the host", i)
		}
	}
}

func TestWriteNBD(t *testing.T)    { testWriteSession(t, false) }
func TestWriteNBDSSL(t *testing.T) { testWriteSession(t, true) }

// A write the host cannot store: ERROR frames for the request, its ordinary
// reply, then the end of the session (NET-A002).
func TestWriteNoSpace(t *testing.T) {
	f := newFakeHost(t)
	f.writeMode = "nospace"
	c, h, done := openWrite(t, f)
	err := c.Write(h, 0, pattern(256<<10, 3))
	var he *HostError
	if !errors.As(err, &he) || !he.NoSpace() || !errors.Is(err, ErrRefused) {
		t.Fatalf("write: %v, want a refusal for lack of space", err)
	}
	if he.Kind != 10 || he.DiskLibCode() != 13 || he.Text != "no space on the file system" {
		t.Errorf("refusal %+v", he)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := c.Write(h, 0, pattern(512, 3)); err == nil {
		t.Fatal("a write after the host ended the session succeeded")
	}
	if !errors.Is(c.Err(), ErrHostEnded) || !errors.Is(c.Err(), ErrSessionEnded) {
		t.Errorf("session error %v, want ErrHostEnded, which matches ErrSessionEnded", c.Err())
	}
	c.Close()
}

// A flush the host refuses for lack of space leaves the session working (NET-A002).
func TestFlushNoSpace(t *testing.T) {
	f := newFakeHost(t)
	f.flushMode = "nospace"
	c, h, done := openWrite(t, f)
	var he *HostError
	if err := c.Flush(h); !errors.As(err, &he) || !he.NoSpace() || he.Kind != 14 {
		t.Fatalf("flush: %v, want a refusal of kind 14", err)
	}
	if err := c.Write(h, 0, pattern(4096, 4)); err != nil {
		t.Fatalf("write after a refused flush: %v", err)
	}
	c.CloseDisk(h)
	c.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

// A write open of a file the host cannot lock: an ERROR frame and then a zero
// reply, after which the session still works (NET-A001, nbdw4).
func TestWriteOpenLocked(t *testing.T) {
	f := newFakeHost(t)
	f.lockedOpen = true
	f.ddb = map[string]string{"adapterType": "lsilogic"}
	tk, addr, done := startFake(t, f)
	c, err := Dial(t.Context(), addr, tk, Options{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.OpenDiskWritable("[ds] vm/vm.vmdk")
	var he *HostError
	if !errors.As(err, &he) || he.DiskLibCode() != 16392 || he.Kind != 13 {
		t.Fatalf("open: %v, want the lock refusal", err)
	}
	h, err := c.OpenDisk("[ds] vm/vm.vmdk")
	if err != nil {
		t.Fatalf("a read-only open after the refusal: %v", err)
	}
	if v, ok, err := c.DDBGet(h, "adapterType"); err != nil || !ok || v != "lsilogic" {
		t.Fatalf("descriptor key after the refusal: %q %v %v", v, ok, err)
	}
	c.CloseDisk(h)
	c.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

// A write reply for another offset ends the session.
func TestWriteBadReply(t *testing.T) {
	f := newFakeHost(t)
	f.writeMode = "badreply"
	c, h, _ := openWrite(t, f)
	if err := c.Write(h, 0, pattern(512, 5)); !errors.Is(err, ErrProtocol) {
		t.Fatalf("write: %v, want ErrProtocol", err)
	}
	if c.Err() == nil {
		t.Error("the session still works after a wrong reply")
	}
	c.Close()
}

func TestBadWriteArguments(t *testing.T) {
	f := newFakeHost(t)
	c, h, done := openWrite(t, f)
	for _, tc := range []struct {
		off uint64
		n   int
	}{{0, 0}, {1, 512}, {0, 100}, {0, MaxWriteRequest + 512}} {
		if err := c.Write(h, tc.off, make([]byte, tc.n)); err == nil {
			t.Errorf("a write of %d bytes at %d was sent", tc.n, tc.off)
		}
	}
	c.CloseDisk(h)
	c.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if f.pieces != 0 {
		t.Errorf("%d pieces reached the host", f.pieces)
	}
}

func TestParseWriteReply(t *testing.T) {
	r := make([]byte, ioPayloadSize)
	r[16], r[25] = 0, 2 // offset 0, length 512
	if err := ParseWriteReply(r, ioDirWrite, 0, 512); err != nil {
		t.Error(err)
	}
	for _, tc := range []struct {
		r   []byte
		dir uint32
		off uint64
		n   uint32
	}{{r[:40], ioDirWrite, 0, 512}, {r, ioDirFlush, 0, 0}, {r, ioDirWrite, 512, 512}, {r, ioDirWrite, 0, 1024}} {
		if err := ParseWriteReply(tc.r, tc.dir, tc.off, tc.n); !errors.Is(err, ErrProtocol) {
			t.Errorf("reply %x accepted for direction %d, %d+%d", tc.r[:28], tc.dir, tc.off, tc.n)
		}
	}
}

// A host that sends error frames without end for a request ends the session instead of holding the call.
func TestWriteErrorFlood(t *testing.T) {
	f := newFakeHost(t)
	f.writeMode = "flood"
	c, h, _ := openWrite(t, f)
	var he *HostError
	err := c.Write(h, 0, pattern(512, 6))
	if !errors.As(err, &he) {
		t.Fatalf("write: %v, want the host's first refusal", err)
	}
	if !errors.Is(c.Err(), ErrProtocol) {
		t.Errorf("session error %v, want ErrProtocol", c.Err())
	}
	c.Close()
}

// Reads keep their handling of the host's answers: in a session that opened no disk for writing, the end-of-session
// frame is a protocol error as before, and an error reply is the plain refusal it always was.
func TestReadSessionIgnoresWriteHandling(t *testing.T) {
	f := newFakeHost(t)
	f.readMode = "end"
	c, h, done := openRead(t, f, Options{})
	err := c.Read(h, 0, make([]byte, 4096))
	if !errors.Is(err, ErrProtocol) || errors.Is(err, ErrHostEnded) {
		t.Fatalf("read: %v, want a protocol error", err)
	}
	c.Close()
	<-done

	f = newFakeHost(t)
	f.readMode = "error"
	c, h, _ = openRead(t, f, Options{})
	err = c.Read(h, 0, make([]byte, 4096))
	var he *HostError
	if !errors.Is(err, ErrRefused) || errors.As(err, &he) {
		t.Fatalf("read: %v, want the plain refusal", err)
	}
	c.Close()
}

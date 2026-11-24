// Copyright 2026 Norvitex OU (AltaStor.com)
// SPDX-License-Identifier: Apache-2.0

package core

import (
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"
	"unsafe"

	"github.com/openvddk/openvddk/internal/nfc"
	"github.com/openvddk/openvddk/internal/transport"
	"github.com/openvddk/openvddk/internal/vix"
)

// fakeWriter plays the host's side of a writable NFC session for the core's write path, with the failures NET-A002
// recorded: a write refused for lack of space (after which the host ends the session), a refused flush, a lost
// session.
type fakeWriter struct {
	set      map[string]string
	writes   [][2]uint64 // offset and length of every request
	flushes  int
	closed   []string // "file", "session"
	failNext error    // the next write fails with this
	failFl   error    // the next flush fails with this
	err      error    // the session's error once it ended
}

func (f *fakeWriter) DDBSet(_ uint64, key, value string) error {
	if f.err != nil {
		return f.err
	}
	if f.set == nil {
		f.set = map[string]string{}
	}
	f.set[key] = value
	return nil
}

func (f *fakeWriter) Write(_ uint64, off uint64, data []byte) error {
	if f.err != nil {
		return f.err
	}
	f.writes = append(f.writes, [2]uint64{off, uint64(len(data))})
	if err := f.failNext; err != nil {
		f.failNext = nil
		var he *nfc.HostError
		if errors.As(err, &he) {
			f.err = nfc.ErrHostEnded // the host ends the session after a refused write
		} else {
			f.err = err
		}
		return err
	}
	return nil
}

func (f *fakeWriter) Flush(uint64) error {
	if f.err != nil {
		return f.err
	}
	f.flushes++
	if err := f.failFl; err != nil {
		f.failFl = nil
		return err
	}
	return nil
}

func (f *fakeWriter) CloseDisk(uint64) error { f.closed = append(f.closed, "file"); return f.err }
func (f *fakeWriter) Close() error           { f.closed = append(f.closed, "session"); return nil }
func (f *fakeWriter) Err() error             { return f.err }

var noSpace = &nfc.HostError{Kind: 10, Status: 0x0d0a}

// writableNBD registers a writable NBD handle of 64 MiB over a fake session.
func writableNBD(t *testing.T, f *fakeWriter) uint64 {
	t.Helper()
	c := &Connection{disks: map[uint64]*Disk{}}
	d := &Disk{conn: c, mode: transport.NBDSSL, path: "[ds] t/t.vmdk", nbd: &nbdDisk{w: &nbdWrite{s: f}}, sectors: 64 << 11, opened: time.Now()}
	h, err := disks.Add(d)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { disks.Remove(h) })
	return h
}

func write(h, start, count uint64) error {
	buf := make([]byte, count*512)
	return Write(h, start, count, unsafe.Pointer(&buf[0]), false)
}

// The first write sets a new content ID; a call larger than 4 MiB becomes several requests, in order; Close flushes,
// then closes the file and the session (D-A002).
func TestNBDWriteRequests(t *testing.T) {
	f := &fakeWriter{}
	h := writableNBD(t, f)
	if err := write(h, 2048, 20480); err != nil { // 10 MiB
		t.Fatal(err)
	}
	if err := write(h, 0, 8); err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(f.set["longContentID"]) || len(f.set) != 1 {
		t.Errorf("descriptor keys set: %v", f.set)
	}
	want := [][2]uint64{{1 << 20, 4 << 20}, {5 << 20, 4 << 20}, {9 << 20, 2 << 20}, {0, 4096}}
	if len(f.writes) != len(want) {
		t.Fatalf("requests %v, want %v", f.writes, want)
	}
	for i := range want {
		if f.writes[i] != want[i] {
			t.Fatalf("requests %v, want %v", f.writes, want)
		}
	}
	if err := write(h, 64<<11-4, 8); vix.ToCode(err) != vix.EDiskOutOfRange || len(f.writes) != 4 {
		t.Fatalf("a write past the end: %v, %d requests", err, len(f.writes))
	}
	if err := Close(h); err != nil {
		t.Fatal(err)
	}
	if f.flushes != 1 || len(f.closed) != 2 || f.closed[0] != "file" {
		t.Errorf("Close: %d flushes, closed %v", f.flushes, f.closed)
	}
}

// An open that writes nothing keeps the disk's content ID.
func TestNBDNoWriteKeepsContentID(t *testing.T) {
	f := &fakeWriter{}
	h := writableNBD(t, f)
	if err := Flush(h); err != nil {
		t.Fatal(err)
	}
	if err := Close(h); err != nil {
		t.Fatal(err)
	}
	if len(f.set) != 0 {
		t.Errorf("keys set without a write: %v", f.set)
	}
}

// A write refused for lack of space answers 8; the host then ends the session, so later writes answer 14009 and
// reads 1, as a read of a read-only open answers when the host ends its session (NBD-0033). Flush and Close return
// the first error, where the reference returns 0 (NET-A002, D-A002). The note on the host's NFC memory is left out:
// the failed write is the reason.
func TestNBDWriteNoSpace(t *testing.T) {
	f := &fakeWriter{failNext: noSpace}
	h := writableNBD(t, f)
	if err := write(h, 0, 8); vix.ToCode(err) != vix.EDiskFull {
		t.Fatalf("Write: %v, want 8", err)
	}
	if err := write(h, 8, 8); vix.ToCode(err) != vix.EHostConnRefused {
		t.Fatalf("Write after the failed write: %v, want 14009", err)
	}
	logged := captureLevels(t)
	buf := make([]byte, 512)
	if err := Read(h, 0, 1, unsafe.Pointer(&buf[0])); vix.ToCode(err) != vix.EFail {
		t.Fatalf("Read after the failed write: %v, want 1", err)
	}
	for _, l := range *logged {
		if strings.Contains(l.text, "NFC memory") {
			t.Errorf("note logged after a failed write: %s", l.text)
		}
	}
	if err := Flush(h); vix.ToCode(err) != vix.EDiskFull {
		t.Fatalf("Flush after the failed write: %v, want 8", err)
	}
	if err := Close(h); vix.ToCode(err) != vix.EDiskFull {
		t.Fatalf("Close after the failed write: %v, want 8", err)
	}
	if len(f.closed) != 1 || f.closed[0] != "session" {
		t.Errorf("Close closed %v; the host had ended the session", f.closed)
	}
}

// A refused flush leaves the session working: later writes are sent, and Flush and Close keep returning the
// refusal, as the data written before it may not be stored.
func TestNBDFlushNoSpace(t *testing.T) {
	f := &fakeWriter{failFl: &nfc.HostError{Kind: 14, Status: 0x0d0a}}
	h := writableNBD(t, f)
	if err := write(h, 0, 8); err != nil {
		t.Fatal(err)
	}
	if err := Flush(h); vix.ToCode(err) != vix.EDiskFull {
		t.Fatalf("Flush: %v, want 8", err)
	}
	if err := write(h, 8, 8); err != nil {
		t.Fatalf("Write after the refused flush: %v", err)
	}
	if err := Flush(h); vix.ToCode(err) != vix.EDiskFull {
		t.Fatalf("a later Flush: %v, want the first error", err)
	}
	if err := Close(h); vix.ToCode(err) != vix.EDiskFull {
		t.Fatalf("Close: %v, want the first error", err)
	}
	if len(f.closed) != 2 {
		t.Errorf("Close closed %v; the session was still working", f.closed)
	}
}

// Another refusal answers 7; a lost session answers 14009 for the call in flight and every later call.
func TestNBDWriteOtherFailures(t *testing.T) {
	f := &fakeWriter{failNext: &nfc.HostError{Kind: 10, Status: 0x0400}}
	h := writableNBD(t, f)
	if err := write(h, 0, 8); vix.ToCode(err) != vix.EFileError {
		t.Fatalf("Write refused by the host: %v, want 7", err)
	}
	Close(h)

	f = &fakeWriter{failNext: errors.New("nfc: reply header: connection reset by peer")}
	h = writableNBD(t, f)
	if err := write(h, 0, 8); vix.ToCode(err) != vix.EHostConnRefused {
		t.Fatalf("Write on a lost session: %v, want 14009", err)
	}
	buf := make([]byte, 512)
	if err := Read(h, 0, 1, unsafe.Pointer(&buf[0])); vix.ToCode(err) != vix.EHostConnRefused {
		t.Fatalf("Read on a lost session: %v, want 14009", err)
	}
	if err := Flush(h); vix.ToCode(err) != vix.EHostConnRefused {
		t.Fatalf("Flush on a lost session: %v, want 14009", err)
	}
	if err := Close(h); vix.ToCode(err) != vix.EHostConnRefused {
		t.Fatalf("Close on a lost session: %v, want 14009", err)
	}
}

// A read of a disk open for writing whose session the host ends on its own answers 1, as a read of a read-only open
// does (NBD-0033), and the note on the host's NFC memory is logged once.
func TestNBDReadWhenHostEnds(t *testing.T) {
	if code := nbdReadCode(nfc.ErrHostEnded); code != vix.EFail {
		t.Fatalf("a read the host ended: %d, want 1", code)
	}
	logged := captureLevels(t)
	n := &nbdDisk{w: &nbdWrite{s: &fakeWriter{err: nfc.ErrHostEnded}}}
	n.noteSessionEnded(nfc.ErrHostEnded)
	n.noteSessionEnded(nfc.ErrHostEnded)
	notes := 0
	for _, l := range *logged {
		if strings.Contains(l.text, "NFC memory") {
			notes++
		}
	}
	if notes != 1 {
		t.Errorf("%d notes, want 1: %v", notes, *logged)
	}
}

// A content ID that cannot be set fails the write with nothing written.
func TestNBDContentIDFailure(t *testing.T) {
	f := &fakeWriter{err: errors.New("nfc: send: broken pipe")}
	h := writableNBD(t, f)
	if err := write(h, 0, 8); vix.ToCode(err) != vix.EHostConnRefused || len(f.writes) != 0 {
		t.Fatalf("Write: %v with %d requests", err, len(f.writes))
	}
	Close(h)
}

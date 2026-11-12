// Copyright 2026 Norvitex OU (AltaStor.com)
// SPDX-License-Identifier: Apache-2.0

package core

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"

	"github.com/openvddk/openvddk/internal/logx"
	"github.com/openvddk/openvddk/internal/nfc"
	"github.com/openvddk/openvddk/internal/vix"
)

// Writes over NBDSSL and NBD (D-A002): one NFC session, no read-ahead, one request in flight on the handle, reads
// included. The first write sets a new content ID; the first failed write or flush is what Flush and Close return.

// nfcWriter is what a disk open for writing uses of its NFC session.
type nfcWriter interface {
	DDBSet(handle uint64, key, value string) error
	Write(handle, off uint64, data []byte) error
	Flush(handle uint64) error
	CloseDisk(handle uint64) error
	Close() error
	Err() error
}

// errSessionEnded answers every call after the host ended the session of a disk open for writing (D-A002).
var errSessionEnded = errors.New("the NFC session ended after a failed write; the data written since the last successful Flush is undefined")

// nbdWrite is the state of a disk open for writing over NBDSSL or NBD.
type nbdWrite struct {
	s        nfcWriter
	handle   uint64
	mu       sync.Mutex // one request in flight
	cidSet   bool       // the new content ID is set (before the first write)
	ended    bool       // the host ended the session after a failed write, or the session was lost
	firstErr error      // the first failed write or flush; Flush and Close return it
}

// read runs a read of the disk with no write in flight.
func (w *nbdWrite) read(read func() error) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.ended {
		return errSessionEnded
	}
	return read()
}

// write sends len(p) bytes at byte offset off in requests of at most nfc.MaxWriteRequest bytes, one at a time; the
// first write of the open sets a new content ID first.
func (w *nbdWrite) write(p []byte, off int64) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.ended {
		return errSessionEnded
	}
	if !w.cidSet {
		id, err := newContentID()
		if err == nil {
			err = w.s.DDBSet(w.handle, "longContentID", id)
		}
		if err != nil {
			return w.failed(fmt.Errorf("setting a new content ID before the first write: %w", err))
		}
		w.cidSet = true
	}
	for done := 0; done < len(p); done += nfc.MaxWriteRequest {
		end := min(done+nfc.MaxWriteRequest, len(p))
		if err := w.s.Write(w.handle, uint64(off)+uint64(done), p[done:end]); err != nil {
			return w.failed(err)
		}
	}
	return nil
}

// failed records a failed write: the first error is kept for Flush and Close, and after a refused write the host
// ends the session (NET-A002).
func (w *nbdWrite) failed(err error) error {
	if w.firstErr == nil {
		w.firstErr = err
	}
	var he *nfc.HostError
	if errors.As(err, &he) || w.s.Err() != nil {
		w.ended = true
	}
	return err
}

// flush sends the flush message and waits for its reply. After a failed write or flush it returns that error: the
// data written before it may not be stored, whatever later flushes answer.
func (w *nbdWrite) flush() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.flushLocked()
}

func (w *nbdWrite) flushLocked() error {
	if w.ended {
		if w.firstErr != nil {
			return w.firstErr
		}
		return errSessionEnded
	}
	if err := w.s.Flush(w.handle); err != nil {
		var he *nfc.HostError
		if w.firstErr == nil {
			w.firstErr = err
		}
		if !errors.As(err, &he) {
			w.ended = true // the session was lost
		}
		return err
	}
	return w.firstErr
}

// close flushes, closes the file and ends the session, where VMware's library sends no flush at Close. A failed
// flush still closes the file; Close then returns the first error of the handle.
func (w *nbdWrite) close(path string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	var ferr error
	if !w.ended {
		ferr = w.flushLocked()
	}
	if w.ended || w.s.Err() != nil {
		w.s.Close()
		if w.firstErr != nil {
			return w.firstErr
		}
		return errSessionEnded
	}
	err := w.s.CloseDisk(w.handle)
	if cerr := w.s.Close(); err == nil {
		err = cerr
	}
	if ferr != nil {
		return ferr
	}
	if err != nil {
		logx.Close.NFC().Warnf("%s: closing the file after the writes: %v", path, err)
	}
	return err
}

// newContentID is a new longContentID: 32 lower-case hexadecimal digits from the system's random source.
func newContentID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// nbdWriteCode is the code of a failed write, flush or Close of a disk open for writing: 8 when the host ran out of
// space, as VMware's library answers (NET-A002); 7 for any other refusal; 14009 when the session is lost or was
// ended by the host.
func nbdWriteCode(err error) vix.Code {
	var he *nfc.HostError
	switch {
	case errors.As(err, &he) && he.NoSpace():
		return vix.EDiskFull
	case errors.As(err, &he):
		return vix.EFileError
	}
	return vix.EHostConnRefused
}

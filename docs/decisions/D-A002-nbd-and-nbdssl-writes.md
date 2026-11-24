# D-A002 — Writes over NBDSSL and NBD

- Date: 2026-11-05, revised 2026-11-12
- Contract: §19, §21, §37, §38
- Status: proposed. Writes over NBDSSL and NBD under the scope and the
  switch of D-A001. The reference observations NW1 to NW4 below were
  recorded on 2026-11-06 (NET-A002), and points 6 to 8 match them. Point 4
  holds the ticket a write open needs (NET-A003).
- Reference for writes: VMware VDDK 9.1, as for D-A001.

## Context

D-A001 lets the current disk of a powered-off VM be written once writes are
switched on. Over NBDSSL and NBD a write needs no worker VM: a restore or a
migration into vSphere from a machine outside vSphere can use them, where
HotAdd (D-A003) needs a worker VM in the same vCenter. VMware's library
writes over NBDSSL and NBD (HOTADD-A001, hw3b to hw3e).

NET-A001 recorded the NFC messages VMware's library uses to write over NBD:
- the open flags;
- the content ID set before the first write;
- the write request in 64 KiB pieces with one reply;
- the flush message;
- the error at a locked open.

NBDSSL carries the same frames inside TLS. A write over NFC needs no worker
VM, attaches nothing and leaves nothing to recover: when the session ends,
the host closes the file.

The risks VMware's library shows over HotAdd show over NFC too:
- **Chains:** it wrote into a parent of a snapshot chain over NBDSSL and
  broke the chain (hw3d).
- **Fallback:** it falls back between transports without telling the
  program (hw3e).

Two risks are new:
- **Clear text:** NBD carries the data in clear text, so on the network it
  can be read and also changed.
- **Content ID:** VMware's library sets a new content ID in every write
  session, and a writer that does not leaves the disk's changes invisible to
  the chain's checks.

NET-A002 then recorded how the host fails writes:
- On a datastore that fills, the host acknowledged a write request, lost
  part of it, and reported the loss only at the next request or flush.
- A failed request gets one `ERROR` frame per failed piece and then its
  ordinary reply, after which the host ends the session.
- VMware's library then returns 0 from `Flush` and `Close` without sending
  anything.

## Decision

1. **Scope.** The rules of D-A001 point 1:
   - the VM is powered off;
   - no snapshot is named;
   - the path is the disk's current top file;
   - the disk is a dependent flat, sparse or SE sparse disk.

   Encrypted disks, raw device mappings and independent disks are refused.
   The checks move from the HotAdd transaction into Open, before any
   transport is tried, so a refusal is the same over every transport (13).
   A running VM's disk is also refused by the host (NET-A001, nbdw4).

2. **Opt-in, per transport family.** `OpenVDDK.nbd.write = "true"`, or
   `OPENVIX_NBD_WRITE=1`, from the places of D-A001 point 2, enables
   writes over NBDSSL and NBD. It is off by default, and independent of
   `OpenVDDK.hotadd.write` (D-A003).

3. **Transports.**
   - **Order.** A write open tries the listed transports whose switch is
     on, in order (D-A001 point 3). Without HotAdd writes (D-A003), HotAdd
     is passed over.
   - **Writes are never relaxed into clear text.** A write open never moves
     from NBDSSL to clear-text NBD, whatever the reason: when NBDSSL is
     tried and fails, the open fails. The D-0039 move after a refused TLS
     stream applies to reads only. So NBD carries writes only when the
     program's list does not name `nbdssl` before `nbd`.
   - **Warning.** Each write open over NBD logs a warning that the data
     crosses the network unencrypted and unprotected against change.
   - **Reference.** VMware's library falls back from HotAdd to NBDSSL
     without telling the program (hw3e); the warning and the refusal to
     move to NBD are deliberate differences.

4. **Open.**
   - **Ticket.** A write open asks vCenter for its NFC ticket with
     `NfcRandomAccessOpenDisk`, giving the VM and the disk's device key, as
     VMware's library does. The host refuses a write open made with the
     ticket of a read, from `NfcGetVmFiles` (NET-A003).
   - **Flags.** The file is opened with the flags VMware's library sends,
     `0x1a` (NET-A001).
   - **One session.** A writable handle uses one NFC session, whatever
     `OpenVDDK.nbd.sessions` says. With several sessions, writes and reads
     in other sessions could cross. It also has no read-ahead.
   - **Same rules.** Allocation queries and descriptor keys work as for
     reads.

5. **Content ID.**
   - Before the first write of an open, the library sets the descriptor key
     `longContentID` to a new value of 32 lower-case hexadecimal digits from
     the system's random source. It uses the type 10 message, as VMware's
     library does.
   - An open that writes nothing does not change it.
   - A failure to set it fails the write, with nothing written.

6. **Writes.**
   - **Requests.** A `Write` call becomes one NFC write request of up to
     4 MiB, the largest single request observed. A larger call becomes
     several, in order.
   - **Pieces.** The data goes in pieces of at most 64 KiB, each with the
     piece header of NET-A001. Bigger pieces wait for an observation with
     larger AIO buffers.
   - **One at a time.** One write request is in flight per handle. Reads on
     the handle, `ReadAsync` included, wait for the write in flight, and a
     write waits for the read in flight. The host accepts several and
     completes them out of order (NET-A002, NW1). Several in flight would
     need replies matched by operation number and is left to a later change.
   - **Reply checks.** The reply must name the request's offset and length.
     The u32 at offset 28, the low bits of the offset in the reference's
     replies, is not relied on.
   - **Past the end.** A call that reaches past the disk's capacity is
     refused with 16007 (`VIX_E_DISK_OUTOFRANGE`) before anything is sent,
     including its part inside the disk, as VMware's library does
     (NET-A002).
   - **WriteAsync.** It completes on the calling thread, as `ReadAsync`
     over HotAdd does (HOTADD-0004). VMware's library returns 25000 and
     calls back on a thread of its own (NET-A002, NW3); the difference is
     deliberate, and harmless for a program that waits for the callback.

7. **Durability** (§38).
   - **Flush.** `Flush` sends the flush message (type 7, direction 2) and
     waits for its reply, as VMware's library does. A reply means *flushed*
     as the host understands it.
   - **Acknowledged is not stored.** The reply to a write request does not
     mean its data will be stored: on a datastore that fills, the host lost
     part of an acknowledged request (NET-A002). Only a successful `Flush`
     says the data written before it is stored. The guides tell programs
     to call `Flush` and check it before treating a copy as complete.
   - **Close.** It flushes before closing the file, where VMware's library
     does not (HOTADD-A001, HW5). A failed flush at Close still closes the
     file and ends the session, and Close returns an error.
   - **After an error.** `Flush` and `Close` on a handle whose write or flush
     failed, or whose session was lost, return that error. VMware's library
     returns 0 from both without sending anything (NET-A002); this
     difference is deliberate.

8. **Errors.**
   - **Error frames.** A request has failed when any `ERROR` frame came for
     its operation number, whatever its reply says. The engine accepts any
     number of `ERROR` frames for an operation in flight, then the reply
     that completes it: the zero reply at an open (NET-A001, nbdw4), the
     ordinary reply of a write or flush (NET-A002). An `ERROR` frame for an
     operation not in flight still ends the session.
   - **Codes.**
     - A refused open answers 13, as reads do (D-0032).
     - A write or flush that runs out of space answers 8
       (`VIX_E_DISK_FULL`), as VMware's library does (NET-A002). The engine
       knows it by the host's disk-library code 13 in the `ERROR` payload,
       or by the error kind 14 of a flush.
     - Any other write or flush the host refuses answers
       `VIX_E_FILE_ERROR` (7).
   - **After a failed write.** The host ends the session after a failed
     write request (NET-A002). The call answers the code above. Every later
     write or flush on the handle answers 14009, and Close answers the
     first error. A read answers 1, as a read of a read-only open does when
     the host ends its session (NBD-0033), and so does a read during which
     the host ends the session. A failed flush leaves the session working,
     as on the host.
   - **Lost session.** A session lost on the network answers 14009 for the
     call in flight and every later call, as for reads (NBD-0002). Nothing
     reconnects, as in VMware's library.
   - **Undefined data.** After any write or flush error, and after a lost
     session, the data written since the last successful `Flush` is
     undefined.

9. **Partial failure.** Nothing is attached and nothing is journaled. A
   process that dies or a session that drops leaves the disk with some
   writes applied and its new content ID: the state of an interrupted copy.
   The program runs the copy again.

10. **Logs.**
    - Open logs, at warning level, that the disk is open for writing, with
      the transport and the host.
    - Close logs the writes and the bytes written, apart from the reads.

## Reference observations before acceptance

| Row | What is observed |
|---|---|
| NW1 | Several writes in flight on one session (WriteAsync from several threads, or one consumer with a queue): the order of pieces and replies, and whether the host accepts it |
| NW2 | Host errors during a write and a flush: a thin target on a datastore that fills (as HW6, with the same care for the shared datastore), a write beyond the capacity, a session cut during a write |
| NW3 | `WriteAsync` over NBDSSL and NBD: the thread of the callback, the return value |
| NW4 | NBDSSL: the same writes as NET-A001, verified by reading back, and a write to the current delta of a VM with a snapshot (the content ID and the chain afterwards) |

All four were recorded on 2026-11-06 in NET-A002. They confirm points 4 and
5. They changed points 6 to 8 as follows:
- several writes in flight are allowed by the host but stay out of scope;
- writes past the end are refused before the wire;
- acknowledged data can be lost before an error;
- many `ERROR` frames come per request, and the session ends after a
  failed write;
- the code for a full datastore is 8.

## How each protocol detail was learned

Every detail above comes from observing VMware's library in our own lab,
through its public functions, and from OpenVDDK's own runs:
- **The NFC write messages** (the open flags, the content ID set, the write
  request and its pieces, the flush, the error at a locked open) come from
  tcpdump captures on the worker VM of the clear-text NFC stream of NBD, on
  port 902, decoded by a lab script (NET-A001). The authentication
  service's TLS records were skipped, not decrypted.
- **The host's failures** (several writes in flight, a datastore that
  fills, a write past the end, a lost session) come from the same captures
  over NBD and from the return codes VMware's library gave the test
  program (NET-A002).
- **NBDSSL** carries the same frames inside TLS, as it carries the read
  frames (NET-0003). Its traffic was never decrypted. NBDSSL writes were checked by their results only: return
  codes, and data read back through a separate read-only open (NET-A002,
  NW4; NBD-A001).
- **The write ticket** comes from the vSphere API calls of VMware's library
  to vCenter, recorded through a TLS relay in front of our own vCenter, the
  method of NET-0001 (NET-A003). Only operation names and argument
  structure were kept; no session token.
- **The reference package** is described in D-A001 point 5. VMware's
  library was examined only through its dynamic tables and by running it.
  No VMware code was used, and nothing was decompiled or disassembled.

## Proposed contract text

For the maintainer to apply with the contract's own section numbers, as a
new section after the one for HotAdd decisions:

> **NBDSSL and NBD writes (D-A002).** The evidence is NET-A001, NET-A002,
> NET-A003 and HOTADD-A001.
>
> - **Switch and scope:** `OpenVDDK.nbd.write` or `OPENVIX_NBD_WRITE`
>   (§37, D-A001); the scope checked in Open before any transport is tried.
> - **Transports:** a write open tries the listed transports whose switch
>   is on, in the program's order. It never moves from NBDSSL to NBD, for
>   any reason. Every write open over NBD logs that the data crosses the
>   network unencrypted and unprotected against change.
> - **Open:** the NFC ticket comes from `NfcRandomAccessOpenDisk` with the
>   VM and the disk's device key. The file is opened with flags `0x1a`, in
>   one NFC session, with no read-ahead.
> - **Content ID:** before the first write of an open, a new
>   `longContentID` (32 lower-case hexadecimal digits from the system's
>   random source) is set with the type 10 message. A failure fails the
>   write before any data is sent.
> - **Writes:** one request of up to 4 MiB per call, or several in order,
>   sent in pieces of at most 64 KiB. One request is in flight per handle.
>   The reply must name the request's offset and length. A call that
>   reaches past the capacity answers 16007 before anything is sent.
> - **Durability (§38):** a write reply does not mean the data is stored;
>   only a successful Flush does. Close flushes before it closes the file.
>   On a handle whose write or flush failed, or whose session was lost,
>   Flush and Close return that error.
> - **Errors:** a request has failed when any `ERROR` frame came for it.
>   A refused open answers 13; out of space 8; another refusal 7; after a
>   failed write request, and on a lost session, 14009, apart from a read
>   after the host ended the session, which answers 1. After any of these
>   the data written since the last successful Flush is undefined.
> - **Partial failure:** nothing is attached or journaled; an interrupted
>   write leaves some writes applied, and the copy is to be run again.

## Consequences

- **Ledger and guides:** `Write`, `WriteAsync` and `Flush` in the ledger
  gain NBDSSL and NBD. KNOWN-DIFFERENCES, the guides and SECURITY say:
  - that NBD writes cross the network unprotected;
  - that the reference falls back silently and this library does not move
    writes to NBD;
  - that only a successful `Flush` means the data is stored, and that the
    reference's `Flush` and `Close` return 0 after a failed write.
- **A direct connection to an ESXi host:** the write asks the host's own
  NFC service for the same ticket, as VMware's library does (NET-A003), and
  works over NBDSSL and NBD (NBD-A002). HotAdd stays vCenter-only (D-0052).
- **Guides:** the documentation pull request carries the wording above.
- **Not planned:** pieces larger than 64 KiB, writes spread over several
  sessions, and writes at snapshots or to running VMs.

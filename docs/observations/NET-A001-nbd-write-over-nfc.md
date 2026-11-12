# NET-A001 — VMware's library writing a disk over NBD: the NFC messages

- **Why:** writes over NBD and NBDSSL are proposed in D-A002.
  OpenVDDK knows the NFC messages for reading (NET-0002, NET-0003) and for
  descriptor keys. This record adds those VMware's library sends to write a
  disk, from our own captures.
- **Reference:** VMware VDDK 9.1 (`libvixDiskLib.so.9.1.0.0`, SHA-256
  `c955e08f…`), the reference for writes (D-A001).
- **Environment:** as HOTADD-A001:
  - vCenter 8.0.3 build 24022515, ESXi 8.0.3 build 24022510;
  - the target VM (one 10 GiB thick flat disk on VMFS, powered off
    unless stated, no snapshot);
  - the program on the worker VM (Debian 13).
- **Runs:** `vixwrite` (HOTADD-A001) with the transport list `nbd` alone,
  a read-write connection and an open without the read-only flag. It writes
  vixwrite's sector pattern (seed per run) and waits 3 s after Open, before
  `Flush` (nbdw2) and before `Close`, so each phase is apart in time:

  | Capture | Writes |
  |---|---|
  | nbdw1 | two 64 KiB `Write` calls at 1 MiB, then `Flush` |
  | nbdw2 | one 4 MiB `Write` at 9 GiB, one 64 KiB `Write` after it, `Flush` |
  | nbdw3 | one 64 KiB `Write` at 9 GiB, no `Flush` |
  | nbdw4 | the target powered on: the write open fails |
  | nbdr1 | read-only open, the nbdw1 region read back (for comparison) |

  The nbdr1 read-back found 0 differing sectors.
- **Capture:** tcpdump on the worker, port 902, full packets; decoded by a
  lab script that reassembles both directions, skips the authentication
  service's TLS records and splits the clear-text NFC stream into classic
  and AIO frames. In nbdw2 the capture missed some packets of the 4 MiB
  write (the kernel dropped them for tcpdump, not on the wire). The pieces
  captured each carry the pattern of exactly the sectors at their offset.
- **Date:** 2026-11-05. **Evidence:** evidence kept privately (the five
  captures, their decodes, the program's output).

## Session

As NET-0003:
- the same classic messages (client name `vddk`, operation `nbdmode`);
- open session, socket options and resource pool;
- after the writes, close file (5), close session (3) and the classic
  message 4.

Writing changes nothing in the set-up.

## Open (AIO type 4)

- **Request:** as for reads, with flags 26 (`0x1a`) in the u32 at offset
  20. VDDK 9.1's read-only open of the same disk sent 30 (`0x1e`): the
  difference is bit `0x04`, the value of `VIXDISKLIB_FLAG_OPEN_READ_ONLY`.
  OpenVDDK's reads send `0x800e`, as VDDK 8.0.3 did (NET-0002).
- **Reply:** as for reads: the 64-bit handle at offset 8, the flags echoed
  at 20.
- **Descriptor keys:** after the open the program asked for the same keys
  as a reader (NET-0003).

## The content ID (AIO type 10)

Before the first write of a session, and only then, the program sent one
type 10 request:
- **Request:** u64 file handle; u32 key length (13); u32 value length (32).
  The key `longContentID` and a value of 32 lower-case hexadecimal digits
  follow, without separators.
- **Reply:** 16 zero bytes.

The value was different in each write session (nbdw1, nbdw2, nbdw3). No
type 10 request was sent in the read-only session.

This is the disk's content ID, which a child disk records as its parent's.
A writer that changed the data without changing it would leave a later
check of the chain unable to see the change.

## Data (AIO type 7)

The same 44-byte structure as reads (NET-0003), little-endian.

- **Direction (u32 at 8):** 0 for a write, 1 for a read, 2 for a flush.
- **Encoding code (u32 at 12):** 0 in every write piece.
- **Request:**
  - one request per `Write` call, whatever its size: the 4 MiB call was one
    request (operation 32), the 64 KiB call after it another (33);
  - the data goes in pieces of at most 64 KiB, each preceded by its own
    44-byte header with the same operation number;
  - in each piece header: u64 at 16 is the request's byte offset, u32 at
    24 the request's total length, u32 at 28 the piece's offset within the
    request, u32 at 32 the piece's length and u32 at 36 the bytes that
    follow (equal to the piece's length).
- **Reply:** one per request, after its last piece. u64 at 16 is the
  request's offset, u32 at 24 its length, u32 at 32 its length again, u32 at
  36 zero. The u32 at 28 carried the low 32 bits of the offset: 1048576 for
  offset 1048576, `0x40000000` for offset `0x240000000`. The reply carries
  no data.
- **One at a time:** the program sent the next request only after the
  reply to the previous one, as for its reads.

## Flush (AIO type 7, direction 2)

`Flush` sent one type 7 request with direction 2 and every other field
zero except the handle, and no data. The reply had direction 2 and zeros
elsewhere. In nbdw2 it came 3 s after the last write reply, when `Flush`
was called.

Without `Flush` (nbdw3) no direction-2 request was sent: `Close` only
asked for the key `isDigest`, then closed the file. The same key request
came before the close in nbdw1 and nbdw2.

## A write open of a running VM's disk (nbdw4)

The host answered the open with:
1. **An `ERROR` frame** (type 1) for the open's operation number. Its
   payload is 16 bytes: u32 13; u32 `0x0040080a`, whose bits 8 to 23 are
   16392, the host's disk-library code for a file it cannot lock; u32 zero;
   u32 125, the length of the host's own error text, which follows.
2. **A second frame** with the same operation number: an `OPEN_FILE`
   reply of 60 zero bytes.

The program failed the open with 1.

OpenVDDK's NFC engine completes a request at its `ERROR` frame. It would
then take the second frame as a reply to no request and end the session.
The open fails either way, but a writer that keeps the session should
expect it.

## What this means for writes over NBD and NBDSSL

- **Messages:** the layouts are known: the open flags, the content-ID key
  write, the write pieces and their reply, the flush and the error at open.
  NBDSSL carries the same frames inside TLS (NET-0003), and was not
  decoded here.
- **Content ID:** a writer sets a new `longContentID` before its first
  write in each session, as the reference does.
- **Pieces:** a write is sent in pieces of at most 64 KiB, the host's
  default AIO buffer size. Larger buffers
  (`vixDiskLib.nfcAio.Session.BufSizeIn64KB`) were not tried with writes.
- **Durability:** a write's reply comes after its last piece. What the
  host has done with the data by then (cached or on the datastore) is not
  visible on the wire. `Flush` is an explicit message, and `Close` does not
  flush.
- **Not observed:** several writes in flight on one session, writes to a
  delta or a thin disk, a host error during a write, and NBDSSL decoded.

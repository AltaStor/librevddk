# NET-A002 — VMware's library writing over NBD and NBDSSL: host errors, several writes in flight, a delta

- **Why:** D-A002 (writes over NBDSSL and NBD) becomes accepted when NW1
  to NW4 are recorded. This record holds them:
  - NW1, several writes in flight on one session;
  - NW2, host errors during a write and a flush;
  - NW3, `WriteAsync`;
  - NW4, NBDSSL writes read back, and a write to the current delta.
- **Reference:** VMware VDDK 9.1 (`libvixDiskLib.so.9.1.0.0`, SHA-256
  `c955e08f…`), the reference for writes (D-A001).
- **Environment:**
  - vCenter 8.0.3 build 24022515, ESXi 8.0.3 build 24022510;
  - the target VM, powered off, moved for these runs to a second
    ESXi host of the same vCenter: its 10 GiB thick flat disk on a VMFS
    datastore of 100 GiB on that host;
  - for NW2 a second disk of the target VM: 1 GiB, thin, alone on a VMFS
    datastore of 1.75 GiB with 347 MiB free, local to the same host;
  - the program on the worker VM (Debian 13); NBD and NBDSSL reach
    the target's host over the network wherever the worker runs.
- **Consumer:** `vixwrite` (HOTADD-A001) with four additions:
  - several `WriteAsync` calls before each `Wait`, each with its own buffer;
  - after a failed `Write`, one more `Write` at the same place and one
    `Read`;
  - optionally a `Flush` after every successful `Write`;
  - a read-back that reports the ranges of differing sectors and whether
    they read as zeros.
- **Capture:** tcpdump on the worker, port 902, full packets, decoded as in
  NET-A001. No capture dropped packets.
- **Date:** 2026-11-06. **Evidence:** evidence kept privately (captures,
  decodes, the program's output for every case, the disk descriptors).

## NW2 — a datastore that fills

**Method.** The thin disk was first filled over NBD from 0 to 300 MiB, which
left 47 MiB free on the datastore. One write open then wrote from 300 MiB to
the end of the disk in 4 MiB `Write` calls. Between cases the thin disk was
deleted and created again.

| Case | Transport, calls | Result |
|---|---|---|
| nw2a | NBD; writes, then `Flush` | `Write` at 348 MiB (sector 712704) returned 8 (`VIX_E_DISK_FULL`); the second `Write` 1; `Read` 1; `Flush` 0; `Close` 0 |
| nw2d | nw2a repeated | the same failure point and the same results |
| nw2e | NBD; `Flush` after every `Write` | `Write` at 344 MiB returned 0 and the `Flush` after it 8; the second `Write` 8; `Read` 1; `Flush` 0; `Close` 0 |
| nw2b | NBDSSL, the datastore full; 4 MiB at 0, a region already allocated | 0 for every call; the data reads back exactly |
| nw2c | NBDSSL, the datastore full; 4 MiB at 900 MiB, not allocated | `Write` 8; then as nw2a |

**Data afterwards** (read-only open over NBD):
- the pre-filled region was intact in every case;
- the region of the failing request read as zeros;
- **data the host had acknowledged was missing.** The last sectors of the
  request before the failing one read as zeros, although that request had
  an ordinary reply:

  | Case | Lost sectors of the acknowledged request |
  |---|---|
  | nw2a | 709488 to 712703 (3216 sectors, 1.6 MiB) |
  | nw2d | 710272 to 712703 (2432 sectors) |
  | nw2e | 710400 to 712703 (2304 sectors); this is the request whose `Flush` returned 8 |

So the host acknowledged a write request and later failed to store part of
it. It reported the loss at the next write request, or at the next flush
when one came first.

### On the wire

**The failing write request** (nw2a; 64 pieces of 64 KiB):
1. The host sent 60 `ERROR` frames with the request's operation number.
   Each payload is 16 bytes:
   - u32 10;
   - u32 `0x00000d0a`, whose bits 8 to 23 are 13, the host's disk-library
     code for no space;
   - u32 zero;
   - u32 143, the length of the host's error text, which follows.
2. Then came an ordinary write reply for the same operation, with the
   request's offset and length, as for a success.
3. Right after the reply, the host sent the classic type 4 frame, the frame a
   client sends to end the session (NET-A001). The host ended the session.

VMware's library then sent nothing more on the session: no flush, no close
file, no close session. Every later call failed inside the library with 1,
except `Flush` and `Close`, which returned 0.

**The failing flush** (nw2e):
1. One `ERROR` frame for the flush's operation, with u32 14 first and
   otherwise as above.
2. Then an ordinary flush reply (direction 2).
3. The session went on: the program's next `Write` was sent, got two
   `ERROR` frames (u32 10 first) and an ordinary reply, and then the host
   sent the classic type 4 frame, whose u32 at offset 4 was 14.

The first u32 of an `ERROR` payload seen so far:
- 10 in a failed write;
- 14 in the flush that ran out of space;
- 13 at an open refused for a lock (NET-A001).

## NW2 — a write past the end of the disk

| Case | What | Result |
|---|---|---|
| nw2f | 128 sectors starting 64 sectors before the end | `Write` 16007 (`VIX_E_DISK_OUTOFRANGE`); the 64 sectors inside the disk were not written |
| nw2g | 128 sectors starting at the end | `Write` 16007 |

In both captures no write request and no content-ID message (type 10) was
sent: the library refused the call before the wire. The `Flush` that
followed was sent, and returned 0.

## NW2 — a session cut during a write

**Method.** One open wrote 1 GiB at 8 GiB in 4 MiB calls. After 4 s the
worker's kernel destroyed the connection to port 902 (`ss -K`).

| Case | Result |
|---|---|
| nw2h, NBD | the `Write` in progress returned 14009; the second `Write` 14009; `Read` 14009; `Flush` 0; `Close` 0 |
| nw2h, NBDSSL | the same |

The library opened no new connection.

Read back afterwards:
- every request acknowledged before the cut was on the disk;
- the request in flight at the cut was applied in part: its first 5120
  sectors were written and the rest read as zeros.

## NW1 — several writes in flight

**Method.** Eight `WriteAsync` calls before each `Wait`, over NBD, captured.

| Case | Calls | On the wire |
|---|---|---|
| nw1a | eight of 64 KiB | all eight requests were sent before the first reply. The host replied in the order 32, 35, 33, 34, 36, 37, 38, 39, not the order sent, and each reply named its own request's offset and length |
| nw1b | eight of 4 MiB | the pieces of one request all went before the first piece of the next, never interleaved. The next request started before the previous reply, so at most two were outstanding |

The flush was sent after the last reply. Both regions read back exactly.

## NW3 — `WriteAsync`

In nw1a, nw1b and nw3a (eight 64 KiB calls over NBDSSL):
- every `WriteAsync` returned 25000 (`VIX_ASYNC`), over NBD and NBDSSL;
- every callback reported 0 and ran on one thread of the library, not on
  the calling thread. The same thread ran every callback of an open;
- `Wait` returned 0 after the last callback.

## NW4 — NBDSSL, and the current delta of a VM with a snapshot

**NBDSSL.** Writes over NBDSSL read back exactly: nw2b (4 MiB), nw3a (eight
asynchronous 64 KiB writes) and nw4a below.

**A delta (nw4a).**
- **Set-up.** A snapshot of the target VM without memory. The disk was then the
  base (VMFS flat) with a current delta (SE sparse).
- **Write.** 1 MiB was written over NBDSSL to the delta's path, with no
  snapshot named, in 64 KiB `Write` calls, then `Flush`.

The descriptors, read from the datastore before and after:

| File | Before | After |
|---|---|---|
| base | `CID` `e0d6ce5c`, `longContentID` ending `e0d6ce5c` | unchanged |
| delta | `CID` `e0d6ce5c`, `parentCID` `e0d6ce5c`, the base's `longContentID` | `CID` `43172adc`, `parentCID` `e0d6ce5c`, a new `longContentID` ending `43172adc` |

- **Read back.** The data reads back through the delta's path. A read-only
  open of the base's path does not have it.
- **Snapshot removed.** The base then had `CID` `43172adc` and the delta's
  `longContentID`. The data read back exactly through the base, and vCenter
  reported no consolidation needed.

In every descriptor seen here the `CID` is the last 8 hexadecimal digits of
the `longContentID`.

## What this means for D-A002

- **Error frames.** One request can get many `ERROR` frames (60 for 64
  pieces), then its ordinary reply. A request has failed when any `ERROR`
  frame came for its operation, whatever the reply says. D-A002 point 8
  expects one zero reply after one `ERROR` frame, which only fits the open.
- **The session after an error.** The host ends the session after a failed
  write request, but not after a failed flush.
- **Acknowledged is not stored.** On a datastore that fills, the host lost
  part of a request it had acknowledged, and reported it only at the next
  request or flush. After any write or flush error, the data written since
  the last successful `Flush` is undefined, not only after a lost session.
- **Codes.** A write or flush that runs out of space answers 8 in the
  reference.
- **Flush and Close after an error.** VMware's library returns 0 from both
  without sending anything. A program that relies on `Flush` to know its
  data is stored is misled.
- **Past the end.** It is refused before anything is sent, with 16007, and
  the part inside the disk is not written either.
- **Several in flight.** The host accepts several write requests at once and
  completes them out of order. A writer with more than one in flight must
  match replies by operation number.
- **`WriteAsync`.** The reference completes it on a library thread, after
  returning 25000.
- **Content ID.** A write to the current delta sets a new content ID on the
  delta only, keeps the parent link, and consolidates normally.

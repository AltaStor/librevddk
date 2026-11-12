# NBD-A001 — OpenVDDK writing a VM's disk over NBDSSL and NBD (D-A002)

- **Why:** the first live runs of the write path of D-A002. They compare
  OpenVDDK with what NET-A001, NET-A002 and NET-A003 recorded for VMware's
  library.
- **Library:** OpenVDDK built on the development host (not the EL8 build
  container) from a development version of this change, before it was
  committed; it also wrote over HotAdd (D-A003). The reference for the read-backs is VMware VDDK 9.1
  (`c955e08f…`).
- **Environment:** as NET-A002:
  - vCenter 8.0.3 build 24022515, ESXi 8.0.3 build 24022510;
  - the target VM, powered off unless stated: its 10 GiB thick flat
    disk on a 100 GiB VMFS datastore, and for the full-datastore cases a
    1 GiB thin disk alone on a datastore with 347 MiB free;
  - the program on the worker VM (Debian 13).
- **Consumer:** `vixwrite` as in NET-A002, built against OpenVDDK's headers.
  The configuration file holds `OpenVDDK.nbd.write = "true"` unless stated.
- **Date:** 2026-11-06. **Evidence:** evidence kept privately (the program's
  output and the library's log for every case, a capture of ow2, the disk
  descriptors).

## Writes

| Case | What | Result |
|---|---|---|
| ow1 | NBDSSL: 8 MiB at 1 MiB in 4 MiB `Write` calls, `Flush`, `Close` | 0 for every call; read back exactly through VMware's library |
| ow2 | NBD, captured: one `Write` of 4 MiB and 64 KiB at 10 MiB | 0 for every call; read back exactly; the warning that NBD carries the data in clear text |
| ow3 | NBDSSL: eight `WriteAsync` of 64 KiB before each `Wait` | each returned 25000; the eight callbacks reported 0 on the calling thread; read back exactly |

**The wire in ow2**, decoded as in NET-A001:
- the open with flags `0x1a`;
- one type 10 message before the first write: key length 13, value
  length 32;
- a 4 MiB request in 64 pieces of 64 KiB, then a 64 KiB request in one
  piece, each with the request's operation number. All 65 pieces carried
  vixwrite's pattern for their sectors;
- one reply per request with its offset and length;
- the flush from `Flush`, a second flush at `Close`, then close file and
  close session.

These are the reference's messages, with two differences: OpenVDDK asks no
descriptor key before the close, and it flushes at `Close`, as D-A002 point
7 decides.

## Refusals

| Case | What | Result |
|---|---|---|
| ow0 | no configuration file | Open 6; the reason names `OpenVDDK.hotadd.write` and `OpenVDDK.nbd.write` |
| ow0b | only `OpenVDDK.hotadd.write`, transport `nbdssl` | Open 6 |
| ow8a | a snapshot named, the snapshot point's delta | Open 13; no NFC session was opened |
| ow8b | no snapshot, the base below the current delta | Open 13 ("not the current top file of its disk") |
| ow9 | the target powered on | Open 13 ("written only while its virtual machine is powered off") |

## Host errors (as NET-A002's NW2)

| Case | What | Result |
|---|---|---|
| ow5 | NBDSSL, the thin disk's datastore fills: from 300 MiB to the end in 4 MiB calls | `Write` at sector 712704 returned 8, where VMware's library failed too; the next `Write` 14009; `Read` 14009; `Flush` 8; `Close` 8 |
| ow6 | as ow5 with a `Flush` after every `Write` | the `Flush` after the write at sector 704512 returned 8; the next `Write` 8; `Read` 14009; `Flush` 8; `Close` 8. Sectors 710528 to 712703 of the request before read as zeros: the loss the flush reported |
| ow7 | the connection destroyed during a 1 GiB write (`ss -K`), NBD and NBDSSL | `Write` 14009, then 14009 for `Write`, `Read`, `Flush` and `Close` |

VMware's library answered `Flush` and `Close` with 0 in the same cases.

## A delta (ow8c)

After a snapshot of the target VM, 1 MiB was written over NBDSSL to the
current delta, with no snapshot named:

| File | Before | After |
|---|---|---|
| base | `CID` `35ad59b4` | unchanged |
| delta | `CID` and `parentCID` `35ad59b4`, the base's `longContentID` | `CID` `a5aedc57`, `parentCID` `35ad59b4`, a new `longContentID` ending `a5aedc57` |

The data read back through the delta. After the snapshot was removed the
base had the delta's content ID, the data read back exactly, and vCenter
reported no consolidation needed. These are VMware's library's results in
NET-A002.

## The ticket

The first runs used the ticket of a read, and the host refused every write
open with 13. The library now asks for the ticket VMware's library asks for
(`NfcRandomAccessOpenDisk`, NET-A003); every case above ran with it.

## Still open

- **Privilege:** the vCenter privilege `NfcRandomAccessOpenDisk` needs, with
  a minimal role.
- **Direct connection to an ESXi host:** run afterwards, in NBD-A002.
- **The build container:** a build from the EL8 container is to be tested
  the same way before a release.

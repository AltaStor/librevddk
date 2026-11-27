# Security review — LibreVDDK 26.11.27 (the write path), 2026-11-27

In short: no high or medium finding in the write path; two low ones, neither of which needs a fix before 26.11.27 is
used, each with a fix proposed for the next version and its limit stated in SECURITY.md.

A review of what LibreVDDK adds to OpenVDDK 0.7.5 (`d99edfb`) up to LibreVDDK 26.11.27 (the tag `v26.11.27`), against
CONTRACT.md §54, §55, §57 and §58 and SECURITY.md, done by reading the code, running the unit tests and the race
detector, and fuzzing what a host sends to a session open for writing. The changes reviewed: the write switches and
the write scope (D-A001), writes over NBDSSL and NBD with their ticket, messages and error handling (D-A002), the
writable HotAdd attachment, its device and its recovery (D-A003), the free-space check before a HotAdd write (D-A004),
the write entry points of the C interface, and the journal lines of `openvddk-admin`. Each finding below was checked
in the code and shown by a test written for the review, before it was accepted; the status says what is proposed.
This review changed nothing.

## Findings

| # | Severity | Finding | Status |
|---|---|---|---|
| 1 | low | **The write scope does not look at a disk's sharing mode.** The scope of D-A001 refuses a snapshot, a running VM, a raw device mapping, an encrypted or independent disk and any file below the top of the chain, but the disk's details carry no sharing mode, so a dependent flat disk set to multi-writer passes. Such a disk of a powered-off VM can be attached to another VM that is running; a write would then change a disk in use. ESXi's file locks are expected to refuse the write open and the attachment, as they refuse a locked file (NET-A001), but this was not observed for a multi-writer disk. Shown by a test: the scope accepts a disk with the details such a disk gets. | **open**, proposed for the next version: read the backing's sharing mode and refuse `sharingMultiWriter` in the write scope, before any transport is tried. Stated in SECURITY.md. |
| 2 | low | **A host can hold one write for up to 1024 reply timeouts.** Every ERROR frame for a write request restarts the wait for its reply, and a request may get up to 1024 of them before the session is ended. A host that sends one just before each timeout holds the call for 1024 times `vixDiskLib.nfc.ReadTimeoutMs`, about 17 hours at the default 60 s, where a silent host holds it for one timeout. Shown by a test: 30 frames, each 40 ms after the last, held one write for 21 reply timeouts of 60 ms. | **open**, proposed for the next version: give the whole request a deadline, for example one reply timeout per piece of the request, or end the session once a request has had more ERROR frames than it has pieces. Stated in SECURITY.md. |

## Notes (no change needed now)

- The write switch is a safety interlock, not an access control. It keeps a program that opens disks writable by
  default from writing after an upgrade of the library, but any program can turn writes on, since it chooses the file
  it passes to `InitEx` and the environment it starts in. Who may write a disk is decided by the vCenter account's
  privileges, which have not yet been checked with a minimal role for writes (the runtime kit's README says so).
- A write over NBD carries the data in clear text: on the network it can be read and changed. The ticket is still
  presented inside pinned TLS, and every write open over NBD logs a warning. A write never moves from NBDSSL to NBD.
- The attachment lock is released after the device was verified and before it is opened for writing. The open
  checks again, on the open file, the device numbers, the disk ID and the size, but not the SCSI address. A device
  that vanished in that moment and whose name and numbers were taken over by another disk with the same disk ID and
  size would be opened; two disks share an ID only when their UUIDs are duplicates, which `disk.AllowDupUUID` allows.
  Checking the SCSI address on the open file as well would close this.
- A journal record with the disk mode `independent_persistent` lets recovery detach a disk in that mode at the
  recorded controller, unit and backing path. Before writes, recovery detached only `independent_nonpersistent` disks,
  which are never a worker's own. A forged record could therefore make recovery detach one of the worker's own
  independent disks; it never deletes a file. The journal directory is created with mode 0700 and is refused unless
  it belongs to the process's user, so only that user, normally root, can write a record.
- The free-space check (D-A004) takes both values from vCenter. A dishonest vCenter can make it pass or refuse, but
  it controls the attachment anyway; the check protects against a full datastore, not against vCenter.
- Reads restart the reply timeout for every piece of a reply in the same way as writes do for ERROR frames (older
  than LibreVDDK, outside this review). The deadline proposed in finding 2 would bound both.
- After a HotAdd write the device is closed, udev probes what was written, and a disk that something on the worker
  then claims (holders, including LVM, multipath, MD and bcache, or a mount) stays attached for review. The LVM check
  is always enforced for writes; MD assembly is not prevented, as SECURITY.md says.
- After a crash during a HotAdd write, recovery detaches the disk and warns that its content is undefined until the
  copy is run again. It cannot tell how much was written.

## Checked and found in order

- Switches: read once by `InitEx`, from its file and from the environment as the process had it when the library
  was loaded; a value other than a switch value leaves writes off from that source and is logged; `Init` logs which
  switches are on; `Exit` clears them.
- Scope, before any transport is tried: no snapshot named; the VM `poweredOff` (a suspended VM is refused); no raw
  device mapping, encrypted or independent disk; a flat, sparse or SE sparse backing; the path names the disk's
  current top file. The HotAdd attachment repeats the rules that apply to it. A write open of a file ESXi holds
  locked is refused by the host (NET-A001).
- NBDSSL and NBD: the write ticket is asked with `NfcRandomAccessOpenDisk` for the VM and the disk's device key; the
  session uses the same pinned TLS dial as reads, and a ticket without a thumbprint is refused before it is sent; the
  retry after a refused cipher offer takes a new ticket and never goes to clear text; one session, no read-ahead, one
  request in flight, reads included.
- Write requests: a whole number of sectors, at most 4 MiB, in pieces of at most 64 KiB; the range is checked
  against the disk's size before anything is sent, and the sector count is bounded by the size, so no byte count
  overflows. Replies: the direction, offset and length must match the request. ERROR frames: the payload is bounded
  by the session's payload limit, the host's text by 64 KiB, and the text is made printable and cut to 512 bytes
  before it is logged; a request is completed exactly once, also when the session fails while ERROR frames arrive.
  The end-of-session frame is recognised only in a session that opened a disk for writing.
- Content ID: 128 bits from the system's random source, set before the first write of an open. The first failed
  write or flush is what Flush and Close return; a write after the host ended the session fails without being sent.
- HotAdd identity (§57): a disk is written only when the worker gives attached disks disk IDs, never when it is
  identified by its position; the expected ID comes from the disk's UUID; a device with that ID already on the worker
  refuses the attachment, and two new matches are ambiguous; after udev has settled the device must be at the chosen
  target and bus, with the expected size, unclaimed and unmounted; the open file must be a block device with the
  listed numbers, the expected ID and size; writes need 512-byte logical blocks; the kernel read-only flag is cleared
  only after these checks.
- HotAdd writes: `pwrite` within the device's size, in whole blocks, never a short write reported as success;
  `fdatasync` for Flush and before Close; the attachment is `independent_persistent`, so a snapshot of the worker
  never captures the writes; detach checks the device key, the backing path and the journaled mode, and never carries
  a file operation; a journaled mode other than the two attachment modes is refused.
- Recovery: records of a live owner are left alone, and `openvddk-admin journal` says when that owner is stuck in
  uninterruptible I/O; a device still claimed on the worker is left for review; a recovered writable attachment is
  named in a warning. The `/proc` parsing behind the stuck-owner line is bounded.
- C interface: Write and WriteAsync check the handle, the I/O mode of the handle's earlier writes, read-only, the
  buffer and the range, in the reference's order; WriteAsync completes on the calling thread, so the library never
  uses the buffer after the call returns, and the callback (which may be NULL) runs with no lock held; Flush of a
  read-only handle does nothing; WriteMetadata, Shrink and Defragment stay unsupported.
- Concurrency: Close takes the disk's lock exclusively and waits for asynchronous reads; writes and reads hold it
  shared; over NBDSSL and NBD the write path's own lock keeps one request in flight. The race detector found nothing.
- Credentials: the new log lines carry paths, switch names and values, sizes and the host's printable text; no
  ticket, session ID or password.
- Fuzzing, after the unit tests and the race detector, both clean: `FuzzParseErrorReply` 49 million inputs and
  `FuzzParseWriteReply` 51 million in 15 minutes each, `FuzzEngineWrite` 28 million in 15 minutes, and, for the
  parts they share with the write path, `FuzzEngine` 12 million and `FuzzJournalRecord` 3.3 million in 10 minutes
  each; no failure.

## Not covered

- ESXi's behaviour for the cases of finding 1 (a multi-writer disk shared with a running VM) and for a VM powered on
  during a write: not tested in the lab.
- The minimal vCenter privileges for writes.
- Writes on vSphere other than 8.0.3, and on datastores other than VMFS.

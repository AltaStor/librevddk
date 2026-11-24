# D-A003 — Writes over HotAdd to a disk of a powered-off VM

- Date: 2026-11-05, revised 2026-11-12
- Contract: §37, §38, §57, §117, §126
- Status: accepted (2026-11-24, contract 0.44; amends D-0014 for disks
  opened for writing). Writes over HotAdd under the scope and the switch of
  D-A001. The reference observations HW1 to HW5 were recorded on 2026-11-05
  (HOTADD-A001); HW6 to HW9 on 2026-11-06 (HOTADD-A003, HOTADD-A004,
  HOTADD-A006, HOTADD-A007). Points 3, 4, 8 and 9 differ from the
  reference on purpose; the notes under them say why.
- D-A004 adds a free-space check before a write open of a disk that can
  grow.
- Reference for writes: VMware VDDK 9.1, as for D-A001.

## Context

D-0027 made the library read-only by design. Every consumer served so far
reads: a migration or a backup reads the source and writes the
destination with its own code. A restore into vSphere, or a migration
into vSphere, needs the opposite direction: writing a disk image into a
VM's disk. VMware's library offers that, and nbdkit's VDDK plugin exposes
it as a writable export.

§37 allows write support only with a new decision that defines snapshot
requirements, transport restrictions, alignment, ordering, durability,
flush behaviour, error handling and partial failure handling. This record
is that decision for a deliberately narrow first step. HotAdd is chosen
because:
- its data path is local block I/O on the proxy (`pwrite`,
  `fdatasync`), so it needs no new knowledge of a protocol;
- durability has a clear meaning on a block device;
- the attach, detach, journal and recovery machinery already exists and
  is qualified (§126).

Writes over NBDSSL and NBD are D-A002. The scope and the switch common to
every transport are D-A001.

The read-only design is also a safety property. Several layers make sure
that a bug cannot change a source disk:
- the non-persistent attach;
- `O_RDONLY` and `BLKROSET` on the device;
- the read-only NFC open flags.

This decision keeps all of them for every read-only open and adds writing
as a separate, opt-in path.

The current code holds two assumptions that writing breaks:
- **Telling attachments apart.** Proxy identification decides which disks
  are HotAdd attachments, and not the proxy's own disks, by their mode
  `independent_nonpersistent`.
- **Detach guard.** The detach refuses to remove a disk in any other mode.

A writable attachment cannot use that mode, so both rules have to rest on
the journal instead (point 5).

## Decision

1. **Scope.** The rules of D-A001 point 1, over HotAdd through vCenter
   (not a direct ESXi connection, D-0052). The HotAdd transaction checks
   them again before anything is attached, so a refusal names the rule for
   writes, not the one for reads.

   *Reference (HOTADD-A001).* VMware's library writes into a parent of a
   snapshot chain when asked, over HotAdd and NBDSSL, with or without the
   snapshot named. The snapshot's content changes and the VM's current
   state no longer opens (16008). A running VM is protected only by the
   host's file locks. It also writes over NBDSSL and NBD. Refusing these
   cases is a deliberate difference.

2. **Opt-in.** `OpenVDDK.hotadd.write = "true"`, or
   `OPENVIX_HOTADD_WRITE=1`, from the places of D-A001 point 2, enables
   writes over HotAdd. It is off by default, and independent of
   `OpenVDDK.nbd.write` (D-A002). The reason: consumers such as nbdkit
   open disks writable unless told otherwise. An upgrade of the library
   must not turn such opens into writable attachments of production
   disks.

3. **Open.**
   - **Transport list.** A write open over HotAdd moves on to the next
     listed transport whose switch is on only when HotAdd cannot run on this
     machine (not a VMware VM, a direct ESXi connection, no usable proxy)
     or when its datastore may not hold the write (D-A004). The move is
     logged at warning level; any other HotAdd failure ends the open
     (§126.9 is amended for writable opens). The reference falls back to
     NBDSSL silently (HOTADD-A001, hw3e); the warning, and never moving on
     after any other failure, are deliberate differences.
   - **Power state.** It is checked when the source is resolved, as now.
     The race with a power-on is closed by ESXi's file locking:
     - a VM that runs holds its disk, so the attach fails;
     - while the disk is attached to the proxy, the VM cannot be powered
       on.

     Both directions are to be confirmed on VMFS, NFS v3 and NFS 4.1
     (HW1).
   - **Relocation.** Consumers should call `PrepareForAccess` around the
     write, as for reads. The guide says so; Open does not require it.

4. **Attach mode.** A writable attachment uses
   `independent_persistent`:
   - `independent_nonpersistent` discards every write at detach;
   - plain `persistent` is captured by snapshots of the proxy, so a
     snapshot of the proxy taken while the disk is attached would send
     the writes into a delta file in the proxy's directory.

   There is no redo log, so §126.2 does not apply. Nothing is deleted
   after the detach, and no `scsiB:U.redo` key is expected.

   *Reference (HOTADD-A001, HW1).* VMware's library attaches the disk in
   plain `persistent` mode. The library keeps `independent_persistent`
   on purpose, for the reason above, and KNOWN-DIFFERENCES will say so.
   Point 5 is needed either way: a `persistent` attachment looks like one
   of the worker's own disks.

5. **Our attachments are known by the journal.**
   - **Journal record.** It carries `disk_mode`, journaled for every
     attachment, and `writable` (bool). Records without these fields,
     written by older versions, are read-only non-persistent attachments.
   - **Proxy identification** (§126.4) still skips
     `independent_nonpersistent` disks. It also skips every disk whose
     controller, unit and backing path match a journaled record of this
     vCenter and proxy in a state that may have attached, so a writable
     attachment left behind by a crash is never taken for one of the
     proxy's own disks. The controller and unit are journaled before the
     attach, where the device key is known only after it.
   - **Detach guard.** It requires the journaled backing path and the
     journaled mode. A writable record allows only
     `independent_persistent`, a read-only record only
     `independent_nonpersistent`.

6. **Device identity and host safety** (§57).
   - **Disk ID required.** A writable attachment is accepted only when
     identified by its disk ID (WWID `naa.<backing UUID>`), never by
     position alone. When the proxy has no disk ID settings and they
     cannot be set (`OpenVDDK.hotadd.configureProxy` is false, or vCenter
     refuses the change), HotAdd is unavailable for the write before the
     attach: the open moves on to the next listed transport whose switch is
     on, as point 3 allows for a proxy that cannot be used, and otherwise
     fails.
   - **LVM check always enforced.** The check of §126.8 is enforced for
     writable opens; `OpenVDDK.hotadd.hostSafety = warn` does not relax
     it.
   - **Device open.** The device is opened `O_RDWR | O_CLOEXEC`, with
     `O_DIRECT` under `VIXDISKLIB_FLAG_OPEN_UNBUFFERED`. It is re-verified
     on the open descriptor as now, and the kernel read-only flag is
     cleared explicitly (`BLKROSET` 0).
   - **Sector size.** A device whose logical block size is not 512 bytes
     is refused for writing. This step has no read-modify-write.

7. **Write path.**
   - **Requests.** `Write` checks the handle, the buffer and the sector
     range in the same order as `Read`. It then writes the consumer's
     memory to the device with a `pwrite` loop. The loop retries on
     `EINTR` and continues after a short write; a write that makes no
     progress is an error.
   - **Unaligned buffers.** Under `O_DIRECT` an unaligned buffer is
     copied into an aligned bounce buffer first. Offsets and lengths are
     whole 512-byte sectors and the device's logical block is 512 bytes,
     so only the buffer's address can be unaligned.
   - **Concurrency and ordering.** Writes on one handle may run
     concurrently, as reads do. Writes to ranges that do not overlap do
     not affect each other. For overlapping writes that are in flight at
     the same time, the order is not defined, as for `pwrite`. A `Write`
     that has returned is seen by every later `Read` on the handle.
   - **Device gate.** Writes count with the device gate (D-0024) like
     reads, so a reconfigure of the proxy waits for them as well.
   - **WriteAsync.** It completes on the caller's thread and calls its
     callback before it returns, as `ReadAsync` over HotAdd does
     (HOTADD-0004); the reference does the same for writes (HOTADD-A001,
     HW4). The rule against
     mixed synchronous and asynchronous modes stays (D-0023).
   - **Read-ahead.** It is off for writable handles: every read goes to
     the device.

8. **Durability** (§38).
   - **Write completion.** A completed `Write` is *acknowledged*: the
     guest kernel accepted the data (buffered) or the device completed it
     (`O_DIRECT`). It is not durable yet.
   - **Flush.** `VixDiskLib_Flush` calls `fdatasync` on the device. This
     makes the guest kernel send a cache flush to the virtual disk; a
     successful return means *flushed*.
   - **Close.** It flushes the same way before the descriptor is closed.
     When that flush fails, Close still closes, releases and detaches the
     device: a disk left attached helps nobody and blocks the VM. Close
     then returns an error and logs that the written data may not be on
     the datastore.
   - **Read-only handles.** On a read-only handle `Flush` keeps its
     current answer.
   - *Reference (HOTADD-A001, HW5).* `Flush` is one `fsync` on the
     device, which matches. `Close` without `Flush` issues no sync at
     all, so the sync at Close is stricter than the reference, on
     purpose.
   - *Reference, removed device.* When the guest device was removed
     under the writer, the reference's `Close` returned 0 and left the
     disk attached to the worker; only a later `VixDiskLib_Cleanup`
     detached it. The library detaches by the journaled vSphere device
     key, whatever the guest device's state, and `Close` reports a detach
     that failed.

9. **Errors.** Write errors map like read errors (D-0028): the device's
   `EIO` and a device that has gone away, each to the code observed for
   the reference (HW5). `ENOSPC` maps to `VIX_E_DISK_FULL` (8).

   *Reference (HOTADD-A001, HW5).*
   - **Buffered writes:** an `EIO` surfaces at `Flush`, which returns 7;
     every `Write` before it returned 0.
   - **Unbuffered writes:** the failing `Write` returns 7.
   - **Removed device:** the kernel answers a write with `ENOSPC`, and the
     reference returns 8, "disk full", buffered and unbuffered.

   The library returns the same codes. Its log line says that the
   device is gone, not that the disk is full.
   - **Full datastore.** On a thin target disk ESXi may not return
     `ENOSPC` at all. It can stop the proxy VM with an "out of space"
     question instead, and then the whole worker freezes. This is
     qualified as HW6 before acceptance, and the guide warns about thin
     targets on a nearly full datastore.

10. **Partial failure and recovery.**
    - **Crash mid-write.** A crash during writing leaves the target disk
      with some writes applied, which is the state of an interrupted
      copy.
    - **Recovery of a writable record** (§126.7):
      - detaches by a plain device remove;
      - never deletes or renames any datastore file;
      - logs a warning that names the VM, the disk and that its content
        is undefined until the copy is run again.

      A device still claimed in the guest (holders, mounts) is left for
      manual review, as now.
    - **Fault injection.** The kill points of §126.10 apply unchanged
      (HOTADD-A006).
      The residue check also requires that no `independent_persistent`
      attachment named in a journal record is left on the proxy.

11. **What the proxy sees in written data.** When a writable descriptor
    is closed, udev rescans the device. The partition tables, LVM
    physical volumes, MD superblocks and file systems just written become
    visible to the proxy:
    - **LVM.** Its check (point 6) keeps it away.
    - **MD and multipath.** Auto-assembly is to be checked (HW7).
    - **Before release.** Close checks the device for holders before it
      releases it, and a claimed device is not released silently.

12. **Logs.**
    - Open logs, at warning level, that the disk is open for writing,
      with the VM, the disk and the proxy.
    - The statistics at Close count writes and bytes written separately
      from reads, and name the flush result.

## Reference observations before acceptance

These are rows HW1 to HW9 of docs/HOTADD-QUALIFICATION.md, with VMware
VDDK 9.1 first and the candidate after it. All nine were run (HOTADD-A001
to HOTADD-A007).
They cover the reference's visible behaviour only (calls to vCenter, the
attach spec, its log, return codes, system calls on the guest device), as
for reads (docs/CLEAN-ROOM.md).

| Row | What is observed |
|---|---|
| HW1 | HotAdd write to a powered-off VM's disk: the attach spec (disk mode, file operations), extraConfig, the detach. Power-on of the target refused while attached, and attach refused while it runs, on VMFS, NFS v3 and NFS 4.1 |
| HW2 | Write open of a running VM, with a snapshot named, and of a file lower in the chain: codes and calls |
| HW3 | Write open with `nbdssl` and `nbd` in the list and HotAdd unusable: does the reference fall back, and with which codes |
| HW4 | `WriteAsync` over HotAdd: thread of the callback, return value |
| HW5 | `Flush` and `Close` after writes: the system calls on the device (`fdatasync`, `fsync`, `BLKFLSBUF`), codes for EIO and a removed device |
| HW6 | Thin target disk on a datastore that fills up: the proxy's state, the codes |
| HW7 | Images with LVM, MD and multipath signatures written and closed: what the proxy activates |
| HW8 | A/B: the same image written by both libraries to identical targets, then hashed through a read-only open and through the VM booted |
| HW9 | Kill at every fault point during a write, recovery, residue check (§126.10) |

The candidate also needs:
- unit tests for the journal fields and their absence;
- unit tests for the identification and detach guards with writable
  records;
- unit tests for the switch, the refusals of point 1 and the Close flush
  failure.

## Contract text

Applied in contract 0.44: §126.1, §126.4, §126.5 and §126.7 to §126.10
carry the changes below, and §126.13 the new subsection, with D-A004's
check. The point listed below for §126.9, no read-ahead, went into §126.5,
where the read-ahead is described. The text as decided:

- **§126.1 and §126.5:** a disk opened for writing is attached in mode
  `independent_persistent` instead of `independent_nonpersistent`; the
  detach guard requires the source path in the journaled mode,
  `independent_nonpersistent` for a read-only record and
  `independent_persistent` for a writable one.
- **§126.9:** a handle opened for writing has no read-ahead.
- **A new subsection of §126, "Writes (D-A003)":**

> - **Switch and scope:** `OpenVDDK.hotadd.write` or
>   `OPENVIX_HOTADD_WRITE` (§37, D-A001); the scope of §37, over HotAdd
>   through vCenter.
> - **Open:** a write open moves on to another listed transport only when
>   HotAdd cannot run on this machine or its datastore may not hold the
>   write (D-A004), and says so. The disk is attached
>   `independent_persistent`; no redo log exists and §126.2 does not
>   apply.
> - **Journal:** records carry `writable` and `disk_mode`; records without
>   them are read-only non-persistent attachments. Proxy identification
>   skips every disk named by a journaled record of this vCenter and proxy,
>   in addition to `independent_nonpersistent` disks.
> - **Device:** a writable attachment is accepted only by disk ID, never
>   by position alone. The LVM check of §126.8 is always enforced. The
>   device is opened `O_RDWR`, with `O_DIRECT` under
>   `VIXDISKLIB_FLAG_OPEN_UNBUFFERED`, and the kernel read-only flag is
>   cleared. A device whose logical block is not 512 bytes is refused.
> - **Write:** a `pwrite` loop from the consumer's memory, through a bounce
>   buffer when an `O_DIRECT` buffer is unaligned. Writes count with the
>   device gate (D-0024). WriteAsync completes on the caller's thread, as
>   ReadAsync over HotAdd. The handle has no read-ahead.
> - **Durability:** Flush calls `fdatasync` on the device; Close does the
>   same before closing it. A failed flush at Close still releases and
>   detaches the device, and Close returns an error. Close detaches by the
>   journaled vSphere device key whatever the state of the guest device,
>   and a detach that fails is reported.
> - **Errors:** `EIO` is answered with 7 (at Write for unbuffered writes,
>   at Flush for buffered ones), `ENOSPC`, which the kernel also returns
>   for a removed device, with 8, as the reference answers them.
> - **Recovery:** a writable record is detached by a plain device remove;
>   no datastore file is ever deleted for it. The log says that the disk's
>   content is undefined until the copy is run again.

## Consequences

- **Ledger.** `Write`, `WriteAsync` and `Flush` gain HotAdd in the
  compatibility ledger (D-A001).
- **Public wording.** KNOWN-DIFFERENCES says that the disk is attached
  `independent_persistent` where the reference attaches it `persistent`
  (contract 0.44).
- **Not planned in this step:**
  - writes to running VMs or at snapshots;
  - read-modify-write for 4Kn devices;
  - read-ahead on writable handles.

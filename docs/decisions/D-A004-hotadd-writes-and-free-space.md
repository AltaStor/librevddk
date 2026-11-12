# D-A004 — HotAdd writes and a datastore that fills

- Date: 2026-11-06
- Contract: §37, §38, §126.13
- Status: proposed. Amends D-A003 for write opens over HotAdd of disks
  that can grow. It does not change NBDSSL
  and NBD (D-A002), where a full datastore is reported as an error. FS1 and
  FS3 below are done; FS2 (the privileges) is open, because creating a
  test role in vCenter was not possible from the test session.
- Evidence: HOTADD-A003 (HW6), NET-A002.

## Context

A thin disk, and a delta of any kind, grows on its datastore as it is
written. HOTADD-A003 filled the datastore of a thin target during a write
over HotAdd, with VMware's library and with OpenVDDK:
- **The worker stops.** ESXi stopped the worker VM, not the write call,
  and posted an out-of-space question on it. Neither library got an error,
  and neither can report one.
- **The answers.** Cancel powers the worker off. Retry without free space
  leaves the writes queued with no end.
- **Everything else on the worker stops too.** Other copies running on the
  same worker stop with it.
- **A stuck writer.** A writer stuck in `O_DIRECT` I/O cannot be killed. Its
  journal record stays held by a live process, so Cleanup leaves it alone,
  and only a detach through vCenter, or a power-off, frees it.

Over NBDSSL and NBD the same condition is an error code: 8, the host ends
the session, and the worker keeps running (NET-A002).

VMware's library does nothing about it before a write. No library can make
a full datastore an error over HotAdd. A library can only refuse to start a
write that the datastore cannot hold, and say so.

## Decision

1. **When the check runs.** At a write open over HotAdd, after the scope
   checks of D-A001 point 1 and before anything is attached, when the
   disk's current top file can grow:
   - a thin flat disk;
   - a sparse or SE sparse file, which is every delta.

   A thick flat base disk never grows and is not checked. A delta is known
   by its chain as well as its backing: vCenter reports a VMFS 6 delta with
   the backing type of a flat disk (HOTADD-A008).

2. **What is compared.**
   - **Room needed:** the disk's capacity minus the space its top file
     already uses on the datastore, as vCenter reports it in the VM's file
     layout after a `RefreshStorageInfo` of the VM; without the refresh the
     value is stale (HOTADD-A005). This is the most a flat file can still
     grow by a full copy. For a delta one eighth is added for its own
     metadata, which took about one ninth of the data written in
     HOTADD-A005.
   - **Free space:** the datastore's free space, read after asking vCenter
     to refresh its storage information. vCenter caches the value, and a
     stale one was seen in the lab in both directions (HOTADD-A003).
     When the refresh is refused, the cached value is used and the log says
     so.

3. **What happens when the room needed is larger than the free space.** It
   depends on `OpenVDDK.hotadd.write.spaceCheck`:
   - **`refuse`, the default.** HotAdd is not used for this open:
     - when the program's transport list names NBDSSL or NBD after HotAdd
       and `OpenVDDK.nbd.write` is on (D-A002), the open moves on to the
       next of them, and the move is logged at warning level;
     - otherwise the open fails with 8 (`VIX_E_DISK_FULL`).
   - **`warn`.** The open goes on over HotAdd. The log says, at warning
     level, that the datastore may fill and that the worker VM would then
     stop.
   - **`off`.** No check and no log.

   The message names:
   - the datastore, its free space, and the room the disk may need;
   - the key, and that NBDSSL reports a full datastore as an error.

4. **Values that cannot be read.** When the file layout or the free space
   cannot be read at all, the outcome is that of a failed check under
   `refuse`, and is logged under `warn`. The message names the missing
   value and the vCenter privilege that reads it.

5. **What the check does not cover.** Other writers on the same datastore
   can use the space between the check and the end of the copy: other VMs,
   snapshots, other copies. The check refuses only what cannot fit at the
   start. The guides say this, and say what to do when a worker stops on the
   question:
   - answer Cancel, which powers the worker off;
   - then run Cleanup on the worker after it starts again.

6. **A writer stuck in `O_DIRECT` I/O.** `openvddk-admin journal` reports an
   owner process that is in uninterruptible sleep, or that has a fatal
   signal pending, and says that its record is freed by detaching the disk
   through vCenter or by powering the worker off. Cleanup does not detach a
   record a live process holds (§126.7); that stays as it is.

7. **Reference.** VMware's library performs no check. A refusal with 8, or a
   move to NBDSSL, before anything is written is a deliberate difference,
   listed in KNOWN-DIFFERENCES.

## Checks before acceptance

| Row | What is checked |
|---|---|
| FS1 | The file-layout size of a thin flat disk and of an SE sparse delta, against the space the datastore browser reports, before and after writes. Done (HOTADD-A005): equal after `RefreshStorageInfo`, stale without it; a delta's metadata added about 11 % |
| FS2 | The storage refresh and the reads of the layout and the free space with the minimal role of the HotAdd guide; the privilege each needs. Open: a test role could not be created from the test session |
| FS3 | On a small datastore: a write open that cannot fit is refused (8) or moved to NBDSSL, and one that fits runs to completion with the datastore not full. Done (HOTADD-A008), also for deltas, `warn` and `off` |

## Consequences

- **Contract:** for the maintainer to apply, the subsection of D-A003 on
  HotAdd writes gains the check,
  the key and the admin report.
- **Configuration:** a new key, `OpenVDDK.hotadd.write.spaceCheck`
  (`refuse`, `warn` or `off`; `refuse` by default).
- **Documentation:** the HotAdd guide and KNOWN-DIFFERENCES describe the
  stop of the worker on a full datastore, the check, and the recovery.
- **Not planned:** reserving space on the datastore, and watching its free
  space during a write.

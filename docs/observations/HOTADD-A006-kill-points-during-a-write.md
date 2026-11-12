# HOTADD-A006 — A kill at every fault point of a HotAdd write, and recovery (HW9)

- **Why:** HW9 of the HotAdd write qualification asks for a kill at every
  fault point during a write, then recovery and a check for residue.
  HOTADD-A002 covered the state `ACTIVE`; this record covers every point.
- **Library:** OpenVDDK built in the EL8 build container from a
    development version of this change (SHA-256 `a09a6ce0…`, as HOTADD-A004).
- **Environment:** vCenter 8.0.3 build 24022515, ESXi 8.0.3 build 24022510;
  the target VM (powered off, its current disk) and the worker
  the worker VM on the same host.
- **Method:** for each point of §126.10:
  - `vixwrite` opened the disk for writing over HotAdd with
    `OPENVIX_FAULT_KILL_AT` set to the point, wrote 1 MiB in 64 KiB calls,
    called `Flush` and `Close`; the library killed the process with SIGKILL
    right after the point;
  - the journal's state was read;
  - `VixDiskLib_Cleanup` ran through OpenVDDK;
  - the journal and the guest's disk inventory (`lsblk`) were compared with
    a baseline taken before the series.

  After the series the worker's disks and configuration keys were read from
  vCenter, and the region read back through VMware's library.
- **Date:** 2026-11-06. **Evidence:** evidence kept privately (the series
  output, every run's log).

## Results

| Point | Killed | Writes before the kill | Journal state after the kill | Cleanup | Journal and inventory after |
|---|---|---|---|---|---|
| ATTACH_SUBMITTED | yes | none | ATTACH_SUBMITTED | 1 cleaned, 0 remaining | empty, as the baseline |
| ATTACH_RETURNED | yes | none | ATTACH_SUBMITTED | 1, 0 | empty, as the baseline |
| ATTACH_CONFIRMED | yes | none | ATTACH_CONFIRMED | 1, 0 | empty, as the baseline |
| GUEST_DEVICE_IDENTIFIED | yes | none | GUEST_DEVICE_IDENTIFIED | 1, 0 | empty, as the baseline |
| GUEST_DEVICE_VERIFIED | yes | none | GUEST_DEVICE_VERIFIED | 1, 0 | empty, as the baseline |
| ACTIVE | yes | none | ACTIVE | 1, 0 | empty, as the baseline |
| CLOSE_STARTED | yes | all, 0 | CLOSE_STARTED | 1, 0 | empty, as the baseline |
| DETACH_INTENT | yes | all, 0 | DETACH_INTENT | 1, 0 | empty, as the baseline |
| DETACH_SUBMITTED | yes | all, 0 | DETACH_SUBMITTED | 1, 0 | empty, as the baseline |
| DETACH_RETURNED | yes | all, 0 | DETACH_SUBMITTED | 1, 0 | empty, as the baseline |
| DETACH_CONFIRMED | yes | all, 0 | DETACH_CONFIRMED | 1, 0 | empty, as the baseline |

- **The two "returned" points** found the journal one state behind, as
  designed: the reconfigure had taken effect, but its result was not yet
  journaled. Recovery found the actual state in vCenter.
- **The warning.** Every Cleanup logged that the disk was open for writing
  when its process ended and that its content is undefined until the copy
  is run again.
- **Residue.** After the series the worker had only its own disk, no redo
  log key named a file, the journal held no record, and the target VM needed
  no consolidation.
- **Data.** The 1 MiB written by the runs killed during `Close` (the same
  pattern each time) read back exactly through VMware's library.

The points `CHILD_REMOVE_INTENT` and `CHILD_REMOVED` belong to the redo log
of a read-only attachment; a writable attachment has none and never reaches
them.

## What this means

Recovery of a HotAdd write is complete at every fault point: Cleanup
detaches the disk, forgets the record and warns, and leaves nothing on the
worker.

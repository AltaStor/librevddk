# HOTADD-A005 — vCenter's file sizes of a thin disk and a delta (D-A004, FS1)

- **Why:** D-A004 (proposed) compares the room a thin disk or a delta can
  still take with its datastore's free space. FS1 asks whether vCenter's
  file layout gives the space a disk file uses, and when.
- **Environment:** vCenter 8.0.3 build 24022515, ESXi 8.0.3 build 24022510;
  the target VM powered off. Its 10 GiB base disk became thin when the VM was
  moved to another datastore (HOTADD-A003).
- **Method:**
  - the VM's `layoutEx` read without and with `RefreshStorageInfo`;
  - the datastore browser's size of the same file beside it;
  - between the reads, writes by OpenVDDK over NBDSSL into areas not yet
    allocated.
- **Date:** 2026-11-06. **Evidence:** evidence kept privately (the tool
  output).

## A thin flat disk

| Moment | `layoutEx` extent size | Datastore browser |
|---|---|---|
| before | 2035286016 | — |
| after 100 MiB written, no refresh | 2035286016 | — |
| 30 s later, no refresh | 2035286016 | — |
| after `RefreshStorageInfo` | 2144337920 | 2144337920 |

The extent grew by 104 MiB for 100 MiB of data. `uniqueSize` was always
equal to `size`.

## An SE sparse delta

A snapshot without memory gave the disk an SE sparse delta.

| Moment | Delta extent size | Datastore browser |
|---|---|---|
| after the snapshot, refreshed | 1048576 | 1048576 |
| after 64 MiB written, no refresh | 1048576 | — |
| after `RefreshStorageInfo` | 75497472 | 75497472 |

The delta grew by 71 MiB for 64 MiB of data: its own metadata took about
7 MiB, 11 % of what was written.

## What this means for D-A004

- **A refresh is needed.** The file layout gives the space a file uses only
  after `RefreshStorageInfo`; without it, vCenter kept the old value for at
  least 30 s after a write. Then it matches the datastore browser exactly.
- **Delta metadata.** A delta grows by more than the data written into it.
  The room a delta may still need is the capacity not yet used plus its
  metadata, about one ninth more in this test. D-A004 point 2 does not count
  that metadata.
- **Not covered here:** VMFS thin-provisioning granularity on other block
  sizes, vSAN and NFS datastores, and the privilege `RefreshStorageInfo`
  needs (FS2).

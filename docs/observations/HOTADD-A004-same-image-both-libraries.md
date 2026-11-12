# HOTADD-A004 — The same image written by both libraries (HW8), and the change tracking of written disks

- **Why:** HW8 of the HotAdd write qualification asks for a whole image
  written by VMware's library and by OpenVDDK and compared. NBDSSL was
  added, for D-A002.
- **Libraries:**
  - VMware VDDK 9.1 (`c955e08f…`), the reference for writes (D-A001);
  - OpenVDDK built in the EL8 build container from a
    development version of this change (SHA-256 `a09a6ce0…`), its first live run.
- **Environment:** as NET-A002 and HOTADD-A003: vCenter 8.0.3 build
  24022515, ESXi 8.0.3 build 24022510; the target VM (powered off)
  and the worker VM on the same host. the target VM tracks changed
  blocks (its new disks get a change tracking file).
- **Method:** four new thin disks of 64 MiB on one VMFS datastore, attached
  to the target VM:

  | Disk | Written by | Transport |
  |---|---|---|
  | A | VMware's library | HotAdd |
  | B | OpenVDDK | HotAdd |
  | C | VMware's library | NBDSSL |
  | D | OpenVDDK | NBDSSL |

  - **The image:** `vixwrite`'s pattern over the whole disk in 1 MiB
    `Write` calls, every fourth 1 MiB chunk all zeros, then `Flush` and
    `Close`.
  - **Compared:** the read-back through VMware's library, the space each
    disk uses (vCenter's file layout after a refresh), and the descriptor
    before and after.
- **Date:** 2026-11-06. **Evidence:** evidence kept privately (the program's
  output, the descriptors, the file lists).

## Results

| | A | B | C | D |
|---|---|---|---|---|
| Read back (131072 sectors) | 0 differ | 0 differ | 0 differ | 0 differ |
| Space used | 64 MiB | 64 MiB | 64 MiB | 64 MiB |
| New `CID` and `longContentID` | yes | yes | yes | yes |
| Descriptor `version` | 3 → 1 | 3 → 1 | 3 | 3 |
| Change tracking file | removed | removed | kept | kept |

- **Content and space.** Both libraries wrote the same bytes and allocated
  the same space. Neither left the zero chunks unallocated.
- **Change tracking.** After a write over HotAdd the change tracking entry
  was gone from the descriptor, the file was deleted, and the descriptor
  version dropped from 3 to 1. This happened with both libraries:
  - VMware's library attaches the disk `persistent`;
  - OpenVDDK attaches it `independent_persistent`.

  Over NBDSSL the tracking file stayed. The change tracking of a disk
  written over HotAdd is lost, and the next backup that relies on it has
  to read the whole disk.

## VMware's library without its local state directory

In the first run of disk A, VMware's library could not create its state
directory under `/tmp/vmware-root`: a directory with a stale `unmount.dat`
from an earlier NBDSSL session was in the way. ConnectEx reported that the
advanced transports were not available and returned 0.
- **Disk A,** asked for `hotadd` alone, was then opened over NBD:
  GetTransportMode answered `nbd`, the clear-text transport.
- **Disk C,** asked for `nbdssl`, also answered `nbd`.

`VixDiskLib_Cleanup` removed the stale state. The run of disk A was then
repeated, over HotAdd; the results above are from the repeated run.

## What this means

- **HW8.** Over HotAdd and over NBDSSL the two libraries leave the same
  content, the same allocation and the same kind of descriptor change.
- **Change tracking.** A write over HotAdd ends the target disk's change
  tracking, with either library; NBDSSL and NBD keep it. The guides say so.
- **Fallback.** VMware's library moved a write asked for HotAdd alone to
  clear-text NBD without an error. OpenVDDK never moves a write to NBD
  (D-A002).

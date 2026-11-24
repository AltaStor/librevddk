# NBD-A003 — AltaStor Backup & Replication backing up and restoring over NBDSSL and NBD

- **Why:** the first runs of a backup application with LibreVDDK: full and
  incremental backups of a disk, and a restore that writes a disk (D-A002).
- **Consumer:** `abr-vmdk-tool`, the disk tool of AltaStor Backup &
  Replication v26.11, built from its source before the release, unmodified.
  It hands `InitEx` a configuration file of its own that holds
  `OpenVDDK.nbd.write = "true"` and `OpenVDDK.hotadd.write = "true"`.
  For incremental backups it asks vCenter for the changed areas itself
  (`QueryChangedDiskAreas`) and reads them through the library.
- **Library:** two builds made on the development host (not the EL8 build
  container), both with writes: LibreVDDK 26.11.25.
  VMware's library was not run in this series.
- **Environment:**
  - vCenter 8.0.3 build 24022515, ESXi 8.0.3 build 24022510, VMFS;
  - the source: the 10 GiB disk of a VM, read with no snapshot named;
  - the target: the 10 GiB flat disk of the powered-off VM made for write
    tests (§120);
  - the program on a Debian 13 machine outside vSphere, signed in to
    vCenter.
- **Date:** 2026-11-25. **Evidence:** evidence kept privately (the
  program's output, the library's log of every run, the image hashes).

## Results

Every case ran with both builds, and every run ended with status "ok".

| Case | Transport list | What | Result |
|---|---|---|---|
| R1 | `nbdssl` | a full backup of the source | opened over NBDSSL (TLS 1.3, NFC version 11) |
| R2 | `nbd` | a full backup of the source | opened over NBD |
| R3 | none: the library's own `hotadd:nbdssl:nbd` | a full backup of the source | HotAdd skipped with "HotAdd works only from a VMware virtual machine; this machine is not one. Trying the next listed transport: nbdssl."; opened over NBDSSL |
| R4 | `nbdssl` | an incremental backup on top of R1, the changed areas of change ID `*` | opened over NBDSSL; the areas vCenter returned were read |
| W1 | `nbdssl` | a full backup of the target restored onto its current disk | 2,021,838,848 bytes in 2169 `Write` calls; `Close` 0 |
| W2 | `nbd` | the same restore over NBD | the same bytes and calls; `Close` 0 |
| L1 | a local VMDK file | a backup | `ConnectEx` fails with 6 (VIX_E_NOT_SUPPORTED): local file access is not supported (D-0039) |

The tool restored every backup to a local file, and the files were hashed:

- R1, R2, R3 and the chain of R1 and R4 give one image, the same with both
  builds.
- After W1 and after W2, with either build, a backup of the target over
  NBDSSL gives the same image as the backup that was restored.

## Not covered

- HotAdd, for reads and writes: the program ran outside vSphere.
- A comparison with VMware's library on the same runs.
- A restore into a delta of a VM with snapshots, and a restore that fails
  on the way (a full datastore, a cut connection).

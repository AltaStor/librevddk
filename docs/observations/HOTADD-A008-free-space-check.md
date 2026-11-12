# HOTADD-A008 — The free-space check before a HotAdd write (D-A004, FS3)

- **Why:** FS3 of D-A004: the check refuses a write open that cannot fit, or
  moves it to NBDSSL, and lets one that fits run.
- **Library:** OpenVDDK built on the development host from the D-A004 changes,
  before they were committed; a second build after the fix described under
  "Deltas".
- **Environment:**
  - vCenter 8.0.3 build 24022515, ESXi 8.0.3 build 24022510;
  - the target VM (powered off) with two new thin disks, 1 GiB and
    128 MiB, alone on a VMFS datastore of 1.75 GiB with 347 MiB free;
  - the worker VM on the same host.
- **Configuration:** `OpenVDDK.hotadd.write = true`. Where named, also
  `OpenVDDK.nbd.write = true` and `OpenVDDK.hotadd.write.spaceCheck`.
- **Date:** 2026-11-06. **Evidence:** evidence kept privately (the program's
  output and the library's log).

## Flat thin disks

| Case | Disk | Transport list, setting | Result |
|---|---|---|---|
| a | 1 GiB, empty | `hotadd` | Open 8: "may grow by up to 1024.0 MiB on datastore [small datastore], which has 347.0 MiB free", with the reason and the key; nothing attached |
| b | 1 GiB | `hotadd:nbdssl`, both write keys | the same refusal for HotAdd, then "HotAdd cannot write this disk here; trying the next listed transport"; written over NBDSSL, 0 for every call |
| c | 128 MiB, empty | `hotadd` | "may grow by up to 128.0 MiB … 347.0 MiB free"; the whole disk written over HotAdd, 0 for every call; read back exactly; 208 MiB left free |
| d | 1 GiB, 4 MiB used | `hotadd`, `warn` | the warning ("may grow by up to 1020.0 MiB … 215.0 MiB free … Going on over HotAdd"), then 4 MiB written over HotAdd |
| e | 1 GiB | `hotadd`, `off` | no check and no line about it; 4 MiB written over HotAdd |

After the cases the journal was empty and the worker had only its own disk.

## Deltas

After a snapshot of the target VM, both disks had a delta on the same
datastore.
- **The first build** measured the 128 MiB disk's delta as needing 127 MiB,
  without the one eighth that D-A004 adds for a delta's metadata.
- **The cause:** vCenter reports the VMFS 6 delta with the backing type of a
  flat disk, so the library took it for a flat file. A disk whose chain has
  a parent is now treated as a delta whatever its backing type.

The repeated run with the fixed build:

| Case | Disk | Result |
|---|---|---|
| g | the 128 MiB disk's delta | "may grow by up to 142.9 MiB on datastore [small datastore], which has 206.0 MiB free"; 4 MiB written over HotAdd |
| h | the 1 GiB disk's delta | Open 8: "may grow by up to 1150.9 MiB … 189.0 MiB free" |

The snapshot was then removed and vCenter reported no consolidation
needed.

## What this means

- The check refuses what cannot fit before anything is attached. It moves
  such an open on to NBDSSL when the program lists it and its write key is
  on, and lets what fits run.
- `warn` and `off` behave as D-A004 decides.
- A delta is known by its chain, not by its backing type.

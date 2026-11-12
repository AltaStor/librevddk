# HOTADD-A001 — VMware's library writing a VM's disk (D-A001, D-A003, rows HW1 to HW5)

- **Why:** the write decisions (D-A001 for every transport, D-A003 for
  HotAdd) list the reference's behaviour to observe before they are
  accepted (HW1 to HW5). This record keeps what VMware's library does when
  a program opens a disk for writing: over HotAdd, NBDSSL and NBD.
- **Reference:** VMware VDDK **9.1** (`libvixDiskLib.so.9.1.0.0`, SHA-256
  `c955e08f…`). The maintainer made 9.1 the reference for every write
  observation (D-A001); VDDK 8.0.3 stays the reference for reads
  (D-0046).
- **Environment:**
  - vCenter 8.0.3 build 24022515; one ESXi 8.0.3 host, build 24022510;
    one VMFS datastore holding the worker and the target.
  - The worker VM: Debian 13, kernel 6.12.74, 2 vCPU, 1 GiB, no
    LVM, no disk ID settings before the first attach.
  - The target VM: a full clone of a Debian test VM (Debian, one 10 GiB
    thick flat disk), made for these tests and rebuilt from the same source
    twice after HW2 and HW3 broke its disk chain (below). Worker and target
    are clones of one VM, so the attached disk carries the worker's own
    ext4 and swap UUIDs.
- **Consumer:** `vixwrite`, a lab program written for this record:
  - it connects with `ConnectEx` (read-only FALSE unless stated) and opens
    the disk without `VIXDISKLIB_FLAG_OPEN_READ_ONLY`;
  - it writes 64 KiB requests of a pattern derived from the sector number
    and a seed, with `Write` or `WriteAsync` (one in flight, then `Wait`);
  - optionally it calls `Flush` and waits before writing;
  - it verifies the pattern through a separate read-only open.

  `LD_LIBRARY_PATH` must name VDDK's `lib64`: without it the transport
  plugin's own libraries are not found, advanced transports are off and
  Open fails with 16053.
- **Method:**
  - the worker VM's disks, disk modes and disk-related extraConfig polled
    from vCenter every 0.5 s during each run;
  - strace of the consumer on the worker (opens, ioctls, writes, syncs,
    closes);
  - the guest's block devices listed while the disk was attached.
- **Date:** 2026-11-05. **Evidence:** evidence kept privately:
  - the consumer's source and its output for every case;
  - the vCenter polls;
  - the strace files.

## HW1: a write over HotAdd to a powered-off VM, no snapshot

The program wrote 4 MiB at 1 MiB into the target's disk (its base file,
the current top), with `Flush` before `Close`.

| Step | Observed |
|---|---|
| Before the attach | `disk.EnableUUID` and `disk.AllowDupUUID` set to TRUE on the worker, as for reads (HOTADD-0008) |
| Attach | the target's disk file on the worker at scsi0:1 in mode **`persistent`** (not independent); `scsi0:1.ctkEnabled` TRUE, carried from the target; `scsi0:1.redo` empty |
| Guest device | `sdb`, WWID `naa.6000c29…` from the disk's UUID, logical block 512, kernel read-only flag off; its partitions show the worker's own ext4 and swap UUIDs; the swap on it was not activated |
| Device open | first `O_RDONLY` (size and SCSI address queries), then `O_RDWR`, without `O_DIRECT` |
| Writes | one `pwritev` of 64 KiB per `Write`, on the calling thread; no asynchronous I/O |
| `Flush` | one `fsync` on the device |
| `Close` | `close` of the device, then the detach; the worker held only its own disk afterwards |
| Codes | 0 for ConnectEx, Open, every Write, Flush, Close and Disconnect |
| Power-on of the target while attached | refused by vCenter: the disk's flat file is locked (hw1p); the write went on and finished with 0 |

The two regions written in HW1 and HW4 read back exactly through a
read-only NBDSSL open (0 differing sectors). The same check against the
wrong seed failed for every sector.

## HW4: `WriteAsync` over HotAdd

32 `WriteAsync` calls each returned `VIX_ASYNC`. All 32 callbacks ran on
the calling thread, before the call returned, with 0. The calling thread
made every `pwritev`. `WriteAsync` over HotAdd is synchronous, as
`ReadAsync` is (HOTADD-0004).

## HW5: `Flush` and `Close`

- With `Flush`, the device got one `fsync` before `close` (HW1).
- Without `Flush`, the device got **no** `fsync`, `fdatasync` or
  `sync_file_range` before `close` (HW4 run). The data was on the disk
  afterwards: the kernel writes a block device's dirty pages back on its
  last close.

Faults were injected on the worker after Open had returned, during a
hold before the writes:
- **offline:** the SCSI device set `offline` for 20 s, so its I/O fails
  with `EIO`, then back to `running` before `Close`;
- **removed:** the SCSI device deleted from the guest (`delete` in
  sysfs).

| Case | Open flags | Fault | `Write` | `Flush` | `Close` | Afterwards |
|---|---|---|---|---|---|---|
| hw5a | buffered: `O_RDWR` | offline | 0 for all 16: the data stays in the page cache | **7**: `fsync` failed with `EIO` | 0 | detached |
| hw5b | unbuffered: `O_RDWR\|O_DIRECT` | offline | **7** at the first: `pwritev` failed with `EIO` | 0 | 0 | detached |
| hw5c2 | buffered | removed | **8**, "disk full": `pwritev` on the deleted device failed with `ENOSPC` | 0 | **0, but the disk stayed attached to the worker** | `Cleanup`: cleaned 1, remaining 0; detached |
| hw5d | unbuffered | removed | **8**, as hw5c2 | 0 | **0, the disk stayed attached** | `Cleanup`: cleaned 1, remaining 0; detached |

- **Close after a removed device.** It tried to open the device again,
  failed, and returned 0 without detaching. What it had attached is
  recorded under `/tmp/vmware-root/<BIOS UUID>-<vm>/hotadd/` (`unmount.dat`
  and a stub descriptor), and `Cleanup` used that to detach. In an earlier
  run (hw5c) that directory had been removed by hand before `Cleanup` ran.
  `Cleanup` then reported 0 cleaned, 0 remaining and the disk stayed
  attached until it was removed from vCenter. Without its local state the
  reference cannot recover its own attachment.
- **A device that cannot be opened during Open.** In a first attempt at
  hw5a the device went offline while Open was still verifying it. The
  reference got `ENXIO`, tried again about every 10 s and opened the
  device once it was back; Open returned 0, 20 s later.

## HW2: write opens that D-A001 refuses

| Case | Target state | Snapshot named | File | Transports | Result |
|---|---|---|---|---|---|
| hw2a | powered off, one snapshot | yes | the snapshot point (base) | hotadd | Open 0; **the base attached `persistent` and written** |
| hw2b | powered off, one snapshot | no | the base, below the current delta | hotadd | Open 0; **the base attached `persistent` and written** |
| hw2d | running, no snapshot | no | the base (current top) | hotadd | Open 13: the attach failed, the file is in use |
| hw2d2 | running, no snapshot | no | the base (current top) | nbdssl | Open 1: the host refused the open, file locked |
| hw2e | running, one snapshot | yes | the snapshot point (base) | hotadd | Open 13: vCenter refused the reconfigure of the worker with `FileLocked` |

After hw2b the snapshot point held the written data. The VM's current
state no longer opened: 16008, the parent's content ID no longer matches
the one recorded in the child. Removing the snapshot reported success but
left the VM needing consolidation, with its disk at the delta and a
capacity of 0. vCenter refused to destroy it ("invalid state"); it was
unregistered and its directory deleted. A write open of the current delta
on that broken chain (hw2c) failed with 13.

A running VM is protected by the host's file locks, over HotAdd and over
NBDSSL alike. A powered-off VM with snapshots is not: the library writes
into a parent of the chain when asked to, and the chain is broken.

## HW3: transports

| Case | Where the program runs | Transports | File | Result |
|---|---|---|---|---|
| hw3a | the worker | hotadd:nbdssl:nbd | the current delta (powered off, one snapshot) | hotadd; the delta attached `persistent` and written |
| hw3b | the worker | nbdssl | the current delta | nbdssl; written |
| hw3c | the worker | nbd | the current delta | nbd; written |
| hw3d | the worker | nbdssl | the base below the delta | nbdssl; **written; the chain broken as in hw2b** |
| hw3e | a Linux machine that is not a vSphere VM | hotadd:nbdssl:nbd | the base (no snapshot) | **nbdssl, without a message to the program**; written |
| hw3f | the same machine | hotadd | the base | ConnectEx 3, as for reads |

- **Writes are not limited to HotAdd.** The reference writes over NBDSSL
  and NBD as well.
- **No warning on fallback.** A write open that cannot use HotAdd falls
  back to the next listed transport without warning the program.
- **Deltas can be written.** HotAdd can write the current delta of a VM
  with snapshots (hw3a); the refusal in hw2c came from the broken chain.

## A side observation: leftovers in `/tmp/vmware-root`

A failed NBDSSL open (hw2d2) left
`/tmp/vmware-root/<BIOS UUID>-<vm>/nbdssl/unmount.dat` on the worker.

- **On the next run.** The next ConnectEx with a snapshot for that VM
  could not create the directory. Advanced transports were then off for
  it, and Open, asked for `hotadd` alone, fell back to clear-text NBD and
  failed on the lock with 1.
- **After removal.** With the directory removed, the same case reached
  HotAdd (hw2e above).

## What this means for the write decisions

| Point | Reference | Consequence |
|---|---|---|
| D-A001 1, scope | writes powered-off VMs' base disks and deltas; writes into a parent of a snapshot chain and breaks it (hw2a, hw2b, hw3d); refuses running VMs through the host's locks | Refusing snapshots and lower chain files is a deliberate difference that prevents chain corruption. Allowing the current delta (hw3a) matches the reference |
| D-A003 3, no fallback | falls back to NBDSSL silently (hw3e) | Deliberate difference: a write open moves on only from a HotAdd that cannot run on this machine, says so, and never moves from NBDSSL to NBD (D-A002 point 3) |
| D-A003 4, attach mode | `persistent` | D-A003 keeps `independent_persistent` as a deliberate difference (a snapshot of the worker would capture plain `persistent` writes). To be recorded in KNOWN-DIFFERENCES |
| D-A003 5, proxy identification | — | A writable attachment in plain `persistent` mode looks like one of the worker's own disks; the journal must tell them apart either way |
| D-A003 6, device | `O_RDWR`, no `O_DIRECT` without the unbuffered flag; 512-byte logical block | Matches |
| D-A003 7, WriteAsync | synchronous on the calling thread | Matches |
| D-A003 8, durability | `Flush` is one `fsync`; `Close` does not flush; after a removed device `Close` returns 0 without detaching | Flush matches. Syncing at Close is stricter than the reference. Detaching by the journaled device key, and reporting a failed detach, are deliberate differences |
| D-A003 9, errors | 7 for `EIO` (at `Write` unbuffered, at `Flush` buffered); 8 for a removed device (`ENOSPC` from the kernel) | The same codes; the log names a removed device as such |

## Still to observe

- **HW1 on NFS:** the lock checks on NFS v3 and NFS 4.1 datastores (this lab has VMFS only).
- **HW6 to HW9:** a datastore that fills, what the worker activates in
  written data, A/B data runs and crash recovery.

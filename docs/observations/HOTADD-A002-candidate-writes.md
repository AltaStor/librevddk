# HOTADD-A002 — OpenVDDK writing a VM's disk over HotAdd (D-A003)

- **Why:** the first live runs of the write path of D-A003. They compare
  OpenVDDK with what HOTADD-A001 recorded for VMware's library:
  - the attachment, the device calls and the data written;
  - the refusals;
  - a write to the current delta of a VM with a snapshot;
  - recovery after the writer is killed.
- **Library:** OpenVDDK built from a development version of this change on
  the development host (not the EL8 build container), linked against
  glibc 2.41 on the worker. The reference for the read-back checks
  is VMware VDDK 9.1 (`c955e08f…`), the reference for writes (D-A001).
- **Environment:** as HOTADD-A001:
  - vCenter 8.0.3 build 24022515, ESXi 8.0.3 build 24022510;
  - one VMFS datastore;
  - the worker VM (Debian 13, no LVM);
  - the target VM (a clone of a Debian test VM, 10 GiB thick flat disk,
    powered off unless stated).
- **Consumer:** `vixwrite` from HOTADD-A001, built against OpenVDDK's
  headers. The configuration file passed to `InitEx` holds
  `OpenVDDK.hotadd.write = "true"` unless stated.
- **Method:**
  - the worker VM's disks and disk modes polled from vCenter every 0.5 s;
  - strace of the consumer on the worker;
  - the HotAdd journal directory listed after each run.
- **Date:** 2026-11-05. **Evidence:** evidence kept privately: the consumer's
  output for every case, the vCenter polls and the strace files.

## Writes

| Case | What | Result |
|---|---|---|
| ovx1 | 4 MiB at 1 MiB, `Write` in 64 KiB requests, `Flush`, `Close` | 0 for every call; attached at scsi0:1 as **`independent_persistent`**, verified by disk ID, `scsi0:1.redo` empty; detached; journal empty |
| ovx2 | the ovx1 region read back | 0 of 8192 sectors differ: through VMware's library (read-only, NBDSSL) and through OpenVDDK (read-only, HotAdd) |
| ovx3 | 2 MiB with `WriteAsync`, one in flight, then `Wait` | 32 callbacks with 0, all on the calling thread, before each call returned; the region reads back exactly |
| ovx8 | 64 KiB to the current delta of the target VM after a snapshot | written; VMware's library then reads the data through the VM's current state, and the snapshot point does not hold it: the chain is intact |

Device calls of ovx1, from strace:
- `/dev/sdb` opened `O_RDWR|O_CLOEXEC`;
- `BLKGETSIZE64` matching the capacity, then `BLKROSET` 0;
- 64 `pwrite64` of 64 KiB;
- one `fdatasync` at `Flush` and one at `Close`, then `close`.

No other process opened the device. The worker was identified by position,
because its own disk had no ID before its next power cycle. The attached
disk was identified by its disk ID, as D-A003 requires.

## Refusals

| Case | What | Result |
|---|---|---|
| ovx4 | no configuration file | Open 6; the reason names `OpenVDDK.hotadd.write` |
| ovx5 | transports `nbdssl:nbd` | Open 6; the reason says writes need HotAdd |
| ovx6 | snapshot named, the snapshot point's file | Open 13 ("a disk is written only without a snapshot named"); nothing attached |
| ovx7 | no snapshot, the base file below the current delta | Open 13 ("not the current top file of its disk"); nothing attached |
| ovx9 | target powered on | Open 13 ("written only while its virtual machine is powered off"); nothing attached |

In the first run of ovx9 the reason was the one for reads ("a snapshot is
required"). The write checks now run first; the second run gave the reason
above.

VMware's library wrote in the cases of ovx5, ovx6 and ovx7 (HOTADD-A001), and
in the cases of ovx6 and ovx7 that broke the chain.

## Recovery after the writer was killed (ovx10)

The consumer opened the disk for writing and was killed with SIGKILL while it
held it, 1 s after Open returned:
- the journal record stayed in state `ACTIVE`, with `writable` true and
  `disk_mode` `independent_persistent`;
- `sdb` stayed in the guest and the disk on the worker.

`VixDiskLib_Cleanup` through OpenVDDK then:
- cleaned 1 and left 0 remaining;
- logged the recovered transaction and the warning that the disk's content
  is undefined until the copy is run again;
- left the journal empty, and the worker with only its own disk.

In a first attempt the SIGKILL went to the strace process that wrapped the
consumer, and the consumer itself kept running. Its record was still locked
by a live process, so Cleanup left it alone (0 cleaned, 0 remaining). That is
the intended behaviour; the attempt was repeated with the consumer killed.

## Still open

- **Rows not yet run:** HW6 (a datastore that fills), HW7 (written images
  that the worker's LVM, MD or multipath could claim) and the other kill
  points of HW9.
- **HW8:** the same image written by both libraries and compared whole.
  Here only regions were compared.
- **The build container:** these runs used a library built on the
  development host. A build from the EL8 container is to be tested the same
  way before a release.

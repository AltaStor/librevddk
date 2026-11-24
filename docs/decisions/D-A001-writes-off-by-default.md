# D-A001 — Writes: off by default, switched on outside the program, and the scope of a write

- Date: 2026-11-12
- Contract: §15, §37, §38, §49
- Status: accepted (2026-11-24, contract 0.44). Amends D-0027 for one case:
  writing the current disk of a powered-off VM, once a switch outside the
  program turns writes on. The transports are D-A002 (NBDSSL and NBD) and
  D-A003 (HotAdd). Also amends D-0007 for the switches' environment
  variables, read in production (point 2), and D-0046 for the reference of
  writes.
- Reference for writes: VMware VDDK 9.1. Every write observation is made
  with it; VDDK 8.0.3 stays the reference for reads (D-0046). The package
  and library hashes are in CONTRACT.md §49.

## Context

D-0027 made the library read-only by design. Every consumer served so far
reads: a migration or a backup reads the source and writes the destination
with its own code. A restore into vSphere, or a migration into vSphere,
needs the opposite direction: writing a disk image into a VM's disk.
VMware's library offers that, and nbdkit's VDDK plugin exposes it as a
writable export.

Two kinds of consumer pull in opposite directions:
- **Backup products** expect a drop-in replacement: writes must work
  without a change to the program's code.
- **Migration tools** are most of today's users. A cold-migration source is
  exactly a powered-off VM with no snapshot, the case this decision makes
  writable. Programs such as nbdkit open disks writable unless told
  otherwise, so an upgrade of the library must not turn their opens into
  writes of production disks.

The switch therefore lives outside the program: a service definition or
the program's own configuration file turns writes on, and a migration host
where nobody does stays read-only.

VMware's library also writes where this decision refuses (HOTADD-A001):
- into a parent of a snapshot chain, over HotAdd and NBDSSL, with or without
  the snapshot named; the snapshot's content changes and the VM's current
  state no longer opens (16008);
- a running VM's disk, protected only by the host's file locks.

## Decision

1. **Scope.** A disk is opened for writing only when all of these hold:
   - the VM is powered off;
   - no snapshot is named (`ConnectEx` with `snapshotRef` NULL);
   - the path is the disk's current top file in the VM's current
     configuration (D-0031): a delta file when the VM has snapshots, the
     base disk otherwise;
   - the disk is a dependent flat, sparse or SE sparse disk. Independent
     disks, raw device mappings of both modes and encrypted disks stay
     refused.

   The checks run in Open, after the disk is resolved and before any
   transport is tried, so a refusal is the same over every transport:
   `VIX_E_FILE_ACCESS_ERROR` (13). Everything else stays read-only.

   *Reference (HOTADD-A001).* Refusing snapshots and files lower in the
   chain is a deliberate difference that prevents chain corruption.
   Writing the current delta (hw3a) matches the reference.

2. **Off by default; switched on outside the program, per transport
   family.** Each family has its own switch: `OpenVDDK.nbd.write` for
   NBDSSL and NBD (D-A002), `OpenVDDK.hotadd.write` for HotAdd (D-A003).
   A configuration written for HotAdd writes does not start sending disk
   data over the network. Writes over a family are off until one of these
   turns them on:
   - `OPENVIX_NBD_WRITE=1` or `OPENVIX_HOTADD_WRITE=1` in the
     environment, easy to set in a service definition.
   - its key set to `"true"` in the configuration file the program passes
     to `VixDiskLib_InitEx`, if it passes one.

   Any one of them is enough. `1`, `true`, `yes` and `on` turn a switch
   on; `0`, `false`, `no` and `off` leave it off; any other value leaves
   writes off from that source and is reported at warning level.
   - **Start-up log.** `Init` and `InitEx` log, for each family that is
     on, the switches that turned it on.
   - **Read-only stays read-only.** An open with
     `VIXDISKLIB_FLAG_OPEN_READ_ONLY` is read-only in every case, and reads
     need none of the switches.
   - **Writes off.** An open without the read-only flag answers as before
     this decision: `VIX_E_NOT_SUPPORTED` (6) on a read-write connection,
     whose reason names the switches of the listed transports, and
     `VIX_E_INVALID_ARG` (3) on a read-only connection.
   - **Names.** The variables are named after the keys, with the
     project's `OPENVIX_` prefix (D-0007, D-0051). D-0007 makes the
     project's variables developer and test overrides; this decision
     amends it for these two variables, which are read in production
     (CONTRACT.md §15).

3. **Transports.** A write open tries the listed transports whose switch is
   on, in the program's order, and passes the others over. D-A002 adds
   NBDSSL and NBD, D-A003 HotAdd. A write never moves from NBDSSL to
   clear-text NBD; the only move a write open makes is from a HotAdd that
   cannot run on this machine (D-A003). When none of them is listed, Open
   answers 6.

4. **Functions.** `Write`, `WriteAsync` and `Flush` act on a handle open for
   writing. `WriteMetadata`, `Shrink` and `Defragment` stay unsupported:
   a read-only handle answers 11 as before (A15), a handle open for writing
   answers 6. The functions that create or change disks without an open
   handle stay as D-0027 left them.

5. **The reference package.** The VDDK 9.1 package
   (`VMware-vix-disklib-9.1.0.0.25379531.x86_64.tar.gz`) was downloaded
   from VMware's public download page without an account, before August
   2026. Its `doc/vmware-general-terms.pdf`, the VMware General Terms of
   22 November 2023, is the same file as the 8.0.3 package's (SHA-256
   `7ca6efae3eab3336e76dada258fafe808e0601d4ae7e5044ef1fff14be8d6758`),
   so items 4 and 5 of D-0053 apply to it unchanged. It was used the way
   the reference for reads is (CLEAN-ROOM.md): through its exported
   functions, from a test program, with the observations recorded.

## Contract text

Applied in contract 0.44: §37 keeps its list of required definitions and
ends with the text below, with the switches and the codes of point 2
spelled out; §15 names the variables and §49 the reference for writes.

> The library is read-only by design (D-0027), with one exception
> (D-A001): the current disk of a powered-off VM opened without a snapshot
> may be written once a switch outside the program turns writes on for the
> transport family (`OpenVDDK.nbd.write` or `OpenVDDK.hotadd.write` in the
> configuration file, or `OPENVIX_NBD_WRITE` or `OPENVIX_HOTADD_WRITE`).
> Outside this exception the write functions answer as the reference
> answers on a read-only handle.
> Any further write support needs a new decision and a contract revision.
>
> For the exception, the definitions this section requires are:
>
> - **snapshot requirements:** no snapshot is named; the VM is powered off;
>   the path is the disk's current top file;
> - **transport restrictions:** the transports of D-A002 and D-A003;
>   dependent flat, sparse and SE sparse disks only; never a move from
>   NBDSSL to NBD;
> - **alignment, ordering, durability and flush, error handling, partial
>   failure handling:** as each transport's decision states them.

## Consequences

- **D-0027.** `Write`, `WriteAsync` and `Flush` change in the compatibility
  ledger from UNSUPPORTED-BY-DESIGN to PARTIAL, with the conditions of
  point 1. `WriteMetadata`, `Shrink` and `Defragment` stay unsupported.
- **Public wording.** README, KNOWN-DIFFERENCES and the guides change "read
  only by design" to: reads; writes to powered-off VMs' current disks, off
  unless switched on outside the program. Done with contract 0.44, in the
  kit's README and KNOWN-DIFFERENCES, the developer guide, SECURITY.md,
  ARCHITECTURE.md, CONTRIBUTING.md and the compatibility matrix.
- **Security review.** A review of the write path comes before the first
  release with it; SECURITY.md says it has not been done yet.
- **Not planned:** writes to running VMs or at snapshots.

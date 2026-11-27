# Security

CONTRACT.md §21, §54–§58.

## Current posture (LibreVDDK 26.11.27)

The library connects to vCenter, or directly to an ESXi host; for HotAdd,
through vCenter only, it attaches disks to the proxy and reads block
devices; for NBDSSL and NBD it reads from the ESXi host's NFC service. When
writes are switched on (CONTRACT.md §37), it also writes the current disk
of a powered-off VM over those transports. The last review is
docs/SECURITY-REVIEW-0.7.4.md, of the changes since
docs/SECURITY-REVIEW-0.7.0.md; the write path (D-A001 to D-A004) has not
been reviewed yet, and is reviewed before the first release with it. The
following is enforced:

- **TLS:** the server's thumbprint (vCenter's, or the ESXi host's on a
  direct connection) is pinned (SHA-1 or SHA-256) whenever it is given,
  otherwise the system's roots and the host name are checked. The NFC
  sessions to ESXi are pinned to the thumbprint in the ticket; a ticket
  without one is refused before it is sent. On a CPU without AES
  instructions the TLS 1.2 offer is AES-128-GCM alone; when the host
  refuses it, the library connects again with the full list, never in
  clear text. Nothing turns verification off. Connect, handshake and every
  NFC step time out; the wait for a reply follows
  `vixDiskLib.nfc.ReadTimeoutMs` (default 60 s).
- **No silent downgrade:** only transports the consumer listed are used.
  The list is read as VMware's library reads it: `hotadd`, `nbdssl` and
  `nbd`, written exactly so, count; any other entry is skipped and named
  in an informational log line, so `NBDSSL:nbd` means clear-text NBD
  (SECURITY-REVIEW-0.7.4, finding 2). NBDSSL is replaced by clear-text NBD
  only when the consumer listed `nbd` after `nbdssl` and the host refused
  TLS for the data stream at the TLS level (an alert, or a reply that is
  not TLS); a reset, an end of stream or a timeout never counts (D-0039).
  The warning that says so is held back when VMware's
  `vixDiskLib.transport.LogLevel` is 0 and no trace file is set (finding
  3).
- **Allocation queries:** over NBDSSL the disk's own encrypted session
  answers QueryAllocatedBlocks. For a HotAdd disk the library asks the
  host over an NFC session whose frames are not encrypted, as VMware's
  library does: the ticket is presented inside TLS, but the disk's path
  and its allocation map are not protected on the network (finding 4).
- **Credentials:** user-ID credentials only. Passwords are never logged.
  The password is kept in the connection until Disconnect (Close may have
  to sign in again) and then wiped; each sign-in makes a string copy Go
  cannot wipe. ConnectEx signs in only when HotAdd is the only transport
  the connection could use (to check the worker's BIOS UUID); otherwise the
  first Open does. On a direct ESXi connection, PrepareForAccess and
  EndAccess sign in and fail with 20 without changing anything.
- **Source disks:** read-only. An open with
  `VIXDISKLIB_FLAG_OPEN_READ_ONLY` is read-only in every case. Its
  attachment is `independent_nonpersistent`, so even a stray proxy write
  lands in a throw-away redo log; its device is opened read-only and set
  read-only (`BLKROSET`); its NFC open uses the read-only flags.
- **Writes are off by default.** An open without the read-only flag fails
  unless a switch outside the program turns writes on for the transport
  family: `OpenVDDK.hotadd.write` or `OpenVDDK.nbd.write` in the
  configuration file passed to `InitEx`, or `OPENVIX_HOTADD_WRITE=1` or
  `OPENVIX_NBD_WRITE=1` in the environment (D-A001). A program that opens
  disks writable by default (nbdkit without `-r`) therefore stays
  read-only after a library upgrade. `Init` logs which switches are on.
- **What may be written.** Only the current top file of a powered-off VM's
  dependent disk, with no snapshot named; never a snapshot point, a file
  lower in the chain, a running VM, an independent disk, a raw device
  mapping or an encrypted disk. Open checks this before any transport is
  tried. VMware's library writes into a parent of a snapshot chain when
  asked and breaks the chain (HOTADD-A001); LibreVDDK refuses.
- **Writes over HotAdd** (D-A003): the disk is attached
  `independent_persistent`, so no snapshot of the worker captures the
  writes; the attachment is known by its journal record. It is accepted only
  by its disk ID, never by position; the LVM check below is always
  enforced, whatever `OpenVDDK.hotadd.hostSafety` says; the device is opened
  `O_RDWR` and its read-only flag cleared only then. Before a disk that can
  grow is attached, its datastore's free space is checked (D-A004): a
  datastore that fills during a HotAdd write stops the whole worker VM
  instead of failing a call.
- **Writes over NBDSSL and NBD** (D-A002): a write is never moved from
  NBDSSL to clear-text NBD. A write over NBD crosses the network
  unencrypted and can be read and changed on the way; each such open logs a
  warning. List `nbdssl` without `nbd` for writes that must stay
  encrypted.
- **Device identity:** see the HotAdd section below.
- **Journal:** the directory must be owned by the effective user, must not
  be group or other writable, and must not be a symlink. Records are written
  atomically. Redo log files named in a record are deleted only if they are
  still the worker's own redo log for the recorded location.
- **Guest device removal:** at Close the attached disk is removed from the
  worker's guest only after checking that the device under its name is
  still that disk (WWID, size, SCSI address, device number).
- **Network input:** NFC replies, descriptor values, allocation bitmaps,
  zero-skipped read pieces and VMDK descriptors are bounded and checked;
  read pieces must lie inside their request and arrive once; a
  zero-skipped piece is accepted only when the read asked for it, and its
  runs must lie inside it in ascending order; each request is completed
  exactly once, never while its data is still being copied into the
  caller's buffer. The host's error frames for a write are bounded in
  number per request and in text length, and the reply to a write must
  name the request's offset and length.
- **Several NFC sessions per disk:** with `OpenVDDK.nbd.sessions` above 1,
  each session has its own ticket and pin. Close ends the sessions one
  after the other, so with a host that stopped answering it can take one
  reply timeout per session (finding 1).
- **Log files:** the library's own log file is created in
  `openvddk-<user>` under `tmpDirectory` or the system's temporary
  directory, which must be a directory of this user closed to others; the
  file is opened without following a link, mode 0600.
- **Diagnostic bundle:** `openvddk-admin bundle` takes only the library's
  own log files (regular files with one name, not links, in a private
  directory of the file's owner).
- **Handles:** never dereferenced; forged or stale values are rejected
  (D-0006).
- **Memory ownership:** consumer-visible memory is C heap with explicit
  owners. `FreeConnectParams` never frees memory it did not allocate.
- **Configuration file:**
  - bounded to 1 MiB;
  - parsed without allocation proportional to hostile input;
  - `#` outside double quotes starts a comment;
  - malformed input produces warnings, never crashes (fuzzed).
- **Log redaction:** log text passes through `logx.Redact`, which strips
  password, cookie, ticket, token, session and key values before reaching
  consumer callbacks.
- **Symbol surface:** the dynamic symbol surface is exactly the 43 public
  functions; the Go runtime is not exported.
- **Hardening:** `BIND_NOW`, RELRO, a non-executable stack, no TEXTREL, no
  RPATH. These are checked by `make abi-gate`.

## Threat model

Covered by the review of 0.7.0 (docs/SECURITY-REVIEW-0.7.0.md) and the
review of the changes up to 0.7.4 (docs/SECURITY-REVIEW-0.7.4.md), with
the findings and what was done about each:

- malicious or compromised vCenter and ESXi; malformed NFC traffic;
- man in the middle, through pinned TLS and no silent downgrade;
- credential leakage (logs, errors, journal, diagnostic bundle);
- symlink, path and temporary-file attacks on journals and log files;
- malicious VMDK metadata; integer overflow and memory exhaustion;
- journal tampering; concurrent cleanup races;
- the HotAdd device-identity checks of §57.
- direct connections to an ESXi host (0.7.4), and the NFC read modes and
  settings added in 0.7.2 and 0.7.3 (zero-skipped reads, the shorter
  cipher offer, sessions per disk, timeouts, log levels).

Not reviewed yet: the write path (D-A001 to D-A004): the switches, the
write ticket and the NFC write messages, the writable HotAdd attachment and
its device, and what the worker may activate in data written to a disk.
What a host sends to a session open for writing is fuzzed: the ERROR frames,
the write and flush replies, and the session that receives them.

## HotAdd: guest content on the proxy (HOTADD-0001, CONTRACT.md §126.8)

When a source disk is attached to the proxy, the proxy kernel and udev read
its partition table and filesystem signatures. In the lab, the fixture's LVM
physical volume carried the same PV UUID as the proxy's own root PV, because
both came from one template. LVM logged a duplicate PVID. Nothing was
activated only because the proxy uses an LVM devices file.

Without a devices file or filter, LVM can prefer the attached copy or
activate its volume groups on the proxy. That threatens both the proxy and
the integrity of the read.

The library therefore:

- checks the proxy's LVM configuration (`lvmconfig`) at the first HotAdd
  Open of a process, and again whenever `lvmconfig` or a file in
  `/etc/lvm` or `/etc/lvm/devices` changes (D-0050). It refuses the Open
  unless LVM is restricted:
  - either `use_devicesfile=1` with an existing devices file,
  - or an operator `global_filter` that does not accept every device.

  `OpenVDDK.hotadd.hostSafety = "warn"` downgrades the refusal to a logged
  warning;
- refuses an attached disk that has holders (LVM, device-mapper or
  multipath) on the disk or its partitions, or that is mounted;
- never mounts or activates guest content (CONTRACT.md §58);
- after a crash, leaves a device that is claimed on the proxy for manual
  review instead of detaching it;
- for a disk opened for writing, always enforces the LVM check, and after
  Close lets udev see what was written before the disk is released: a disk
  the worker then claims stays attached, with its journal record, for
  review (D-A003).

The check covers LVM only. On a worker with `mdadm`'s udev rules, a disk
that carries a member of an MD array whose home host is the worker's own
host name is put into an inactive array the moment it is attached, for a
read and for a write alike. The open is then refused (the disk is claimed),
but the array stays on the worker (HOTADD-A007). Turn automatic assembly
off on such a worker (`AUTO -all` in `mdadm.conf`).

**Required proxy configuration.** Use the LVM devices file. It is the
default on EL9, where `lvmdevices` lists only the proxy's own physical
volumes. Alternatively, set a `global_filter` in `/etc/lvm/lvm.conf` that
accepts only the proxy's own disks. `disk.EnableUUID=TRUE` on the proxy
VM is no longer required: like VDDK, the library sets it (and
`disk.AllowDupUUID`) before its first attach, unless
`OpenVDDK.hotadd.configureProxy = "false"` (D-0016).

**Identity checks (§57, §126.4, §126.5).** The proxy is identified by its
SMBIOS UUID, corroborated by its own disks' WWIDs or, when its disks carry
no IDs, by its network adapters' hardware addresses and its own disks'
SCSI positions and sizes; a BIOS UUID shared by several matching VMs is
refused. Every attached device must show all of the following before it is
opened:

- the WWID derived from the source disk's backing UUID; or, when the
  proxy's settings could not be switched on, no WWID at all together with
  the expected SCSI bus (from the controller's PCI slot label), being new,
  and being the only such disk;
- the expected SCSI target and LUN 0;
- the source capacity.

These are checked again on the open descriptor. A disk opened for writing
must carry the WWID: identification by position is never enough to write
(§57, D-A003).

## Reporting

Report suspected vulnerabilities privately through GitHub's private
vulnerability reporting for the LibreVDDK repository,
github.com/AltaStor/LibreVDDK. Do not open public issues. Please include:

- the exact build (the first line of the library's log);
- what you observed, and how to reproduce it;
- the impact you expect.

You will get an acknowledgement, and a note when it is resolved. Please
allow time for a fixed build before discussing it elsewhere.

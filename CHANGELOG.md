# Changelog

LibreVDDK's changes since OpenVDDK 0.7.5, newest first.  
A LibreVDDK version is the date of its source in the form YY.MM.DD,  
kept in the [VERSION](VERSION) file.  
OpenVDDK's history up to 0.7.5 is in its release notes, the last being
[docs/RELEASE-NOTES-0.7.5.md](docs/RELEASE-NOTES-0.7.5.md).

## v26.11.27

The first LibreVDDK version, based on OpenVDDK 0.7.5 (the tag `v0.7.5` and
the two documentation commits after it). Its release carries the runtime
kit (also as `librevddk-runtime.tar.gz`), the developer kit, the SBOM and
`SHA256SUMS`; the kits can also be built from source (`make runtime`,
`make devkit`).

Anyone with OpenVDDK 0.7.5 can move to it with the same configuration file.
Writes stay off until switched on, and until then an open without
the read-only flag answers as in 0.7.5.  
The exported interface (ABI) and the headers are unchanged.
Scripts that look for the kit's file names, or for `OpenVDDK/` in
vCenter's session list, need the new names below.

### Writes

- **Off by default, switched on per transport family** (D-A001). Writes
  over NBDSSL and NBD are switched on by `OpenVDDK.nbd.write = "true"` in
  the file passed to `InitEx` or by `OPENVIX_NBD_WRITE=1` in the
  environment; writes over HotAdd by `OpenVDDK.hotadd.write` or
  `OPENVIX_HOTADD_WRITE=1`. Either place is enough, and `InitEx` logs
  which switches are on. An open with the read-only flag stays read-only,
  and reads need no switch, so a program that opens disks writable unless
  told otherwise (nbdkit without `-r`) does not start writing after an
  upgrade.
- **What may be written.** The current disk of a powered-off VM, opened
  over a read-write connection with no snapshot named: its delta when the
  VM has snapshots, its base disk otherwise. Flat, sparse and SEsparse
  disks only. Anything else fails `Open` with 13 before any transport is
  tried: a snapshot named, a file lower in the chain, a running VM, an
  independent disk, a raw device mapping, an encrypted disk.
- **`Write`, `WriteAsync` and `Flush` over NBDSSL and NBD** (D-A002). One
  NFC session per disk, no read-ahead and one request in flight; the disk
  gets a new content ID before the first write. Errors use VMware's codes:
  8 for a full datastore, 7 for another refusal, 14009 once the host has
  ended the session. A write never moves from NBDSSL to clear-text NBD,
  and the log warns at every write open over NBD.
- **Writes over HotAdd** (D-A003). The disk is attached
  `independent_persistent` and written only when identified by its disk
  ID, so the worker needs its disk ID settings. The LVM devices file or
  filter is always required for writes, whatever
  `OpenVDDK.hotadd.hostSafety` says. Reads of a disk being written skip
  the read-ahead. `WriteAsync` completes on the calling thread, as
  VMware's library does over HotAdd. When HotAdd cannot run on this
  machine, the open moves on to the next listed transport whose switch is
  on, and says so in the log.
- **Free-space check before a HotAdd write** (D-A004). When a datastore
  fills under HotAdd, ESXi stops the worker VM with a question instead of
  failing the write. Before a thin disk or a delta is attached for writing,
  the room it may still take is therefore compared with its datastore's
  free space: `OpenVDDK.hotadd.write.spaceCheck` is `refuse` (the
  default), `warn` or `off`. When the write may not fit, the open moves on
  to NBDSSL or NBD if listed with their switch on, and fails with 8
  otherwise.
- **Flush and Close.** Only a successful `Flush` says that the data
  written before it is stored. `Close` flushes a disk open for writing
  (fdatasync over HotAdd, the flush message over NBDSSL and NBD), where
  VMware's library does not; `Flush` and `Close` return the first failed
  write. Over HotAdd, a written disk that the worker claimed after the
  device was closed is left attached for review.
- **Reads of a disk open for writing.** A read whose session the host
  ended answers 1, as reads of read-only disks do since 0.7.5; a lost
  session still answers 14009. `ReadAsync` waits for the write in flight,
  as `Read` does.
- **Not supported:** `WriteMetadata`, `Shrink` and `Defragment` fail with 6
  on a disk open for writing (11 on a read-only one, as before).
- **Change tracking.** A write over HotAdd ends the target disk's changed
  block tracking, with VMware's library too; over NBDSSL and NBD it is
  kept.
- **Reference.** VMware VDDK 9.1 is the reference for writes; VDDK 8.0.3
  stays the reference for reads (contract §49).

The kit's README ("Writing") and KNOWN-DIFFERENCES.md describe writes in
full, including where VMware's library writes and LibreVDDK refuses.

### HotAdd journal and openvddk-admin

- The journal keeps each attachment's disk mode; a record from before is
  a read-only `independent_nonpersistent` attachment, as every record of
  0.7.5 is. Detach removes a disk only in the mode its record names, and
  attachments are told from the worker's own disks by that mode or by a
  journal record. Recovery warns when a writing process did not finish.
- `openvddk-admin journal` shows each record's disk mode and whether it
  was opened for writing, and says when a record's owner is stuck in
  uninterruptible I/O, which recovery leaves alone.
- The diagnostic bundle recognises a LibreVDDK library as well as an
  OpenVDDK one.

### Names and version

- The project is LibreVDDK, a VMware Dependency Delete Kit (VDDK), forked
  from OpenVDDK. OpenVDDK's copyright line stays, and Norvitex OÜ
  (AltaStor.com) holds the copyright of LibreVDDK's changes. NOTICE names
  LibreVDDK's copyright holder and keeps OpenVDDK's notice unchanged.
- A new source file carries the copyright line of its author, or by
  default `Copyright <year> the LibreVDDK contributors`;
  `tools/stamp-headers.sh --holder NAME` names the author (contract §88).
- The build version comes from the VERSION file only; the build stops on a
  missing value or one of another form than YY.MM.DD.
- The library names itself LibreVDDK in its first log line,
  `LibreVDDK <version> (<commit>), independent implementation; …`,
  and in the client identification sent to vCenter and ESXi,
  `LibreVDDK/<version> (VixDiskLib 8.0.3 compatible) <program>`.
- The kits are `librevddk-runtime-<version>-<commit>.tar.gz` (also built as
  `librevddk-runtime.tar.gz`) and `librevddk-devkit-<version>-<commit>.tar.gz`;
  their BUILD.txt and the SBOM name LibreVDDK.
- The runtime kit holds what a program needs to run with the library: the
  library and its links, `lib64`, `openvddk-admin`, the user guide and the
  licence files (D-A006). The headers and the `read-disk` example are now
  only in the developer kit, which also gains the `lib64` link, so its
  directory works as nbdkit's `libdir` too. The headers are unchanged.
  In the source tree, the runtime kit's texts are in `runtime/` (was
  `evalkit/`) and its script is `tools/runtime.sh`; `make evalkit` still
  works.
- The developer kit's pkg-config module is `librevddk`. The kit carries the
  same file as `openvddk.pc`, so `pkg-config openvddk` keeps working.
- Log lines no longer start with `OpenVDDK: VixDiskLib: ` (D-A005). A
  line of an API call starts with the call's name
  (`VixDiskLib_OpenEx: Open disk '…'`), any other line with its text
  (`Disk IO Count: 7`); the library's own log file and the trace file
  carry the same text after the time and the level. Log searches and
  scripts that look for the prefix need to look for the function name or
  the text instead.
- Kept, so that existing setups keep working: the `OpenVDDK.`
  configuration keys, the `OPENVIX_` environment variables, the
  `openvddk-admin` program and the Go module path.

### Security and testing

- The write path has not had a security review yet (SECURITY.md).
- New fuzz targets for what a host sends to a session open for writing:
  `FuzzParseErrorReply`, `FuzzParseWriteReply` and `FuzzEngineWrite`.
- Problem and security reports go to the issue tracker of
  github.com/AltaStor/LibreVDDK.

### Documentation

- The contract keeps OpenVDDK's document version 0.43 and adds a LibreVDDK
  document version, the date of the revision: 26.11.24 describes writes
  (§15, §37, §49, §126, the new §131), 26.11.25 the version and the names
  (§86, §127.1, §129), 26.11.25.1 the log lines without the prefix
  (§86), 26.11.25.2 the project's name in the title and the copyright
  lines (§88, §129) and 26.11.27 the runtime kit's contents (§127, §129).
- Decisions D-A001 to D-A004 and their observations record how writes were
  designed and what VMware's library was seen to do; D-A005 records the
  log line format and D-A006 the runtime kit without headers and example.
- The compatibility matrix, the NBD qualification catalogue and the kit's
  README list AltaStor Backup & Replication v26.11 as a tested application:
  full and incremental backups and restores over NBDSSL and NBD (NBD-A003).
- The README, the kit's README and KNOWN-DIFFERENCES.md, the developer
  guide, ADMIN.md, ARCHITECTURE.md, the compatibility matrix and the HotAdd
  and NBD qualification catalogues describe writes. COMPATIBILITY.md lists
  `Write`, `WriteAsync` and `Flush` as partial.
- The documents name the library LibreVDDK where they describe it as it is
  now, and keep OpenVDDK's name for its releases, its records and the names
  kept for compatibility. The README says that writes were tested on
  vSphere 8.0.3 only, and CLEAN-ROOM.md and PROVENANCE.md point to D-A001
  and D-A002 for how writes were learned.

### Unchanged

- The exported interface (ABI) and the headers, every existing default,
  and the read path: read requests put the same bytes on the wire as in
  0.7.5.

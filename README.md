# LibreVDDK

LibreVDDK is a free and open-source VMware Dependency Delete Kit (VDDK), not to
be confused with the VMware Virtual Disk Development Kit, which uses the same acronym.

LibreVDDK is a fork of OpenVDDK, which was originally written by Andrija Panić and reads virtual disks only.
LibreVDDK also writes disks and aims to cover more of the public API and behaviour of VMware's VDDK.
What it implements today is listed function by function in [COMPATIBILITY.md](COMPATIBILITY.md).

LibreVDDK is an independent implementation of VMware's VixDiskLib, the virtual disk library of
VMware's VDDK 8.0.3, for Linux x86-64.
It is a drop-in replacement: it builds as `libvixDiskLib.so.8.0.3` (with the `.so.8` and `.so` links)
and has the same C interface, so programs written for VixDiskLib, such as AltaStor Backup & Replication,
nbdkit's VDDK plugin and virt-v2v, load it in place of VMware's library without source changes.

LibreVDDK reads disks at a snapshot or, on a powered-off VM, as they are.
It also writes the current disk of a powered-off VM over HotAdd, NBDSSL and NBD, with no snapshot named.
Writes are off by default and are switched on explicitly (CONTRACT.md §37; "Writing" in
[evalkit/README.md](evalkit/README.md)); so far they have been tested on vSphere 8.0.3 only.

The library names itself LibreVDDK in its first log line and in the
client identification it sends to vCenter and ESXi, and the kits are
`librevddk-runtime-…` and `librevddk-devkit-…`, with the pkg-config
module `librevddk` (`openvddk` still works). Log lines start with the
function they belong to (`VixDiskLib_OpenEx: …`) or with their text.
Elsewhere OpenVDDK's names remain, so existing setups keep working: the `OpenVDDK.`
configuration keys, the `OPENVIX_` environment variables, the `openvddk-admin`
program and the Go module path.

It was written from public documentation, the facts any program needs to
link against the library, and observation of what VMware's library visibly
does when it runs. It contains no VMware code. How each behaviour was
learned is recorded in [docs/CLEAN-ROOM.md](docs/CLEAN-ROOM.md),
[PROVENANCE.md](PROVENANCE.md) and the observation records under
[docs/observations/](docs/observations/); for writes, in decisions D-A001 to
D-A004 under [docs/decisions/](docs/decisions/).

## Status

**LibreVDDK 26.11.26** is the first version of the project.
Its [CHANGELOG.md](CHANGELOG.md) lists LibreVDDK's changes since OpenVDDK 0.7.5.

**LibreVDDK 26.11.26** is based on **OpenVDDK 0.7.5** (released 2026-11-22; release notes:
[docs/RELEASE-NOTES-0.7.5.md](docs/RELEASE-NOTES-0.7.5.md)) with LibreVDDK's
writes over HotAdd, NBDSSL and NBD on top (D-A001 to D-A004).

LibreVDDK's build version is the date of its source in the form YY.MM.DD,
such as 26.11.26. The [VERSION](VERSION) file holds it, and the build
stamps it into the library and `openvddk-admin`: it is the `<version>` of
the library's first log line, of the client identification sent to vCenter
and ESXi, and of the kit archive names (CONTRACT.md §86).

Pull requests are welcome: please read CONTRIBUTING.md first, and for
anything larger than a small fix open an issue before you start.
Problem reports and questions are welcome as issues (see "Reporting
problems" below).

## What is supported

- vSphere 6.7, 7.0, 8.0 and 9.1 (tested on 6.7 U3, 7.0.3, 8.0.3 and 9.1.1;
  ESXi 7.0 GA verified over NBDSSL and NBD on a user's host), through
  vCenter and by signing in to an ESXi host directly, including a
  standalone host without vCenter
  ([docs/COMPATIBILITY-MATRIX.md](docs/COMPATIBILITY-MATRIX.md)).
  Writes were tested on vSphere 8.0.3 on VMFS only ("Writing" in the matrix).
- Transports HotAdd, NBDSSL and NBD, listed as `hotadd:nbdssl:nbd`
  (D-0039). HotAdd is used through vCenter.
- Linux x86-64 with glibc 2.28 or newer.
- The 43 public functions of VixDiskLib 8.0.3 with the reference library's
  SONAME and symbol versions; the status of each function is in
  [COMPATIBILITY.md](COMPATIBILITY.md). Behaviour that differs from VMware's
  library on purpose is listed in
  [evalkit/KNOWN-DIFFERENCES.md](evalkit/KNOWN-DIFFERENCES.md).

How to install and run the library is described in the user guide,
[evalkit/README.md](evalkit/README.md); the developer guide is
[devkit/GUIDE.md](devkit/GUIDE.md); the recovery and diagnostic program
`openvddk-admin` is described in [docs/ADMIN.md](docs/ADMIN.md). For large
disks over NBDSSL and NBD (several NFC sessions per disk, larger buffers, and
how many disks a host serves at once), see
[docs/TUNING.md](docs/TUNING.md).

## Build and test

All builds run in a pinned AlmaLinux 8 container under rootless Podman, on
Linux or in WSL2 (CONTRACT.md §82). In the source tree:

```sh
tools/container/run.sh make check          # the full gate: format, vet, unit and race tests,
                                           # library build, ABI gate, layout, conformance, ledger
tools/container/run.sh make repro          # two clean builds, byte-compared
tools/container/run.sh make env-report     # builder identity
```

The first run builds the container image, which needs network access.
The build itself runs without network access and takes the Go modules
pinned in `go.mod` and `go.sum` from the container's module cache (the
`openvddk-gomod` volume). `OPENVIX_NETWORK=1` gives the runner network
access to fill that cache once, for example:

```sh
OPENVIX_NETWORK=1 tools/container/run.sh env GOPROXY=https://proxy.golang.org go mod download
```

The library lands in `build/lib/`: `libvixDiskLib.so.8.0.3` plus the
`.so.8` and `.so` links. `make evalkit` and `make devkit` build the binary
kit and the developer kit from a committed tree. [TESTING.md](TESTING.md)
lists every target.

Differential runs against VMware's library need VMware's VDDK package,
which you obtain yourself; it is mounted read-only and is test input only:

```sh
OPENVIX_REF_DIR=/path/to/vmware-vix-disklib-distrib \
  tools/container/run.sh make differential
```

## Layout

| Path | Contents |
|---|---|
| `cabi/` | C ABI boundary: exported functions, callbacks, version script, layout pins; Go bridge (`package main`, c-shared) |
| `include/` | Independently written declarations: `openvix/vixdisklib_abi.h` and the source-compatibility header `vixDiskLib.h` (D-0038) |
| `internal/` | Go implementation: `vix` errors, `lifecycle`, `config`, `handles`, `core`, `transport`, `logx`, `buildinfo`; HotAdd in `hotadd` (transaction, journal, recovery), `vsphere` (session, inventory, proxy reconfigure, access control), `linuxdev` (SCSI discovery, verified device I/O, host safety), `vmdk` (descriptor parser); NBD and NBDSSL in `nfc`; `readahead` |
| `cmd/openvddk-admin/` | Recovery utility and diagnostic bundle |
| `abi/vddk-8.0.3/` | ABI manifest and ledger source, reference ELF facts, layout, recorded reference transcript, accepted differences |
| `tests/abi/` | Native C conformance harness, layout probe, fork test |
| `tests/consumer/` | `vixcopy` and other test consumers; `ab-swap.sh`, the library swap test against VMware's library with SHA-256 and timing (CONTRACT.md §124) |
| `tests/lab/` | Live HotAdd tooling: observation harness, crash series, proxy residue check |
| `tests/fixtures/` | Fixture definitions and golden hashes (the VM images are not distributed) |
| `tools/ovxtool/` | ELF facts, ABI gate, layout check, transcript diff, ledger, SBOM |
| `tools/container/` | Pinned build container and runner |
| `configs/` | Lab description schema (example only) |
| `evalkit/`, `devkit/` | User guide, known differences, developer guide and examples shipped in the kits |
| `docs/decisions/` | Decision records D-0001… |
| `docs/observations/` | Observation records: how VMware's library behaves, and how this library compares |

The governing specification is [CONTRACT.md](CONTRACT.md); the
architecture is in [ARCHITECTURE.md](ARCHITECTURE.md) and the security
posture in [SECURITY.md](SECURITY.md).

## Reporting problems

Open an issue in this repository for a reproducible problem, a compatibility report
(including "it simply worked with tool X") or a question about behaviour.

- Name the exact build: the first line of the library's log reads
  `LibreVDDK <version> (<commit>) ...`.
- Remove sensitive details before you post: passwords, tokens, keys,
  session tickets; host names, IP addresses and user names of your vCenter
  and ESXi hosts; VM names, datastore names and paths that identify
  people, organisations or projects; certificate thumbprints. Keep versions, transport,
  datastore type, error codes and the library's log lines around the
  problem.
- Do not post VMware's library files, disassembly of them or their log
  text. If you compare with VMware's library, describe what it did
  (succeeded or failed, error code) instead of pasting its output.

Suspected security problems do not belong in issues: report them privately
as [SECURITY.md](SECURITY.md) describes.

## Support

LibreVDDK is provided under the Apache License 2.0 on an "AS IS" basis,
without warranties or conditions of any kind, and without liability for
damages (sections 7 and 8 of the licence). There is no guaranteed response time.
Questions, problem reports and other enquiries go to the issue
tracker of github.com/AltaStor/LibreVDDK.

## Licence

Licensed under the Apache License, Version 2.0: see [LICENSE](LICENSE) and [NOTICE](NOTICE).
The third-party modules compiled into binary builds and their licences are listed in
[THIRD-PARTY-NOTICES](THIRD-PARTY-NOTICES).

Every source file carries an `SPDX-License-Identifier: Apache-2.0` line and the copyright line of its author.
OpenVDDK's files keep OpenVDDK's line, and the files LibreVDDK changed add Norvitex OÜ's line for the changes.
A new file names its author or, by default, the LibreVDDK contributors (CONTRIBUTING.md).

Copyright 2026 Andrija Panić and the OpenVDDK contributors.  
Copyright 2026 Norvitex OÜ (AltaStor.com), for LibreVDDK's changes.

OpenVDDK was originally written by Andrija Panić.

VMware, vSphere, vCenter, ESXi and VDDK are trademarks of Broadcom Inc. or its subsidiaries.

LibreVDDK and OpenVDDK are independent projects and are not affiliated with or endorsed
by Broadcom Inc. or its subsidiaries.

Contact: librevddk@altastor.com

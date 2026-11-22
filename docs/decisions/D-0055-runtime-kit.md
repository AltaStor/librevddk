# D-0055 — The runtime kit: new name, a fixed-name copy, and a lib64 link

- Date: 2026-11-22
- Contract: §127
- Status: accepted by the maintainer (name 2026-11-03, confirmed 2026-11-08; fixed-name copy 2026-11-08; the
  link in the 0.7.5 scope, 2026-11-22).

## Context

- The kit's archive was `openvddk-evalkit-<version>-<commit>.tar.gz`. Other projects install it by that name,
  pinned by SHA-256, and the name reads like a trial version.
- A user asked for a link that always gives the newest kit. A versioned file name cannot give one.
- nbdkit's VDDK plugin, and virt-v2v through it, looks for `lib64/libvixDiskLib.so.8` under its `libdir`. The kit
  has `lib/`, so users had to make a `lib64` link by hand before pointing the plugin at the kit, and kept
  hitting this although the README said so.

## Decision

1. The kit is the **runtime kit**: `openvddk-runtime-<version>-<commit>.tar.gz`, with the same top-level directory
   name. Its contents are unchanged apart from item 3. `make runtime` builds it; `make evalkit` stays as the old
   name. The developer kit keeps its name.
2. The build also writes the same bytes as `openvddk-runtime.tar.gz`. A release carries both files and lists both
   in its `SHA256SUMS`, so `…/releases/latest/download/openvddk-runtime.tar.gz` always gives the newest kit. A
   draft kit (terms marked DRAFT) gets no fixed-name copy.
3. The kit carries `lib64`, a link to `lib`, so its own directory works as `libdir`. The README keeps the
   one-line fix (`ln -s lib lib64`) for copies that lost the link.

## Consequences

- Released 0.7.4 files keep their names and checksums; pinned installers keep working.
- Installers that download by name need the new name from 0.7.5 on; the release notes say so plainly (same
  contents, new name), and downstream users who are known to us are told before the release.
- Old records keep their wording (`evalkit`); new text uses the new name. The source folder
  `evalkit/` and the script `tools/evalkit.sh` keep their names.

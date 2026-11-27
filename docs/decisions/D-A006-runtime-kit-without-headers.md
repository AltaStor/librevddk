# D-A006 — The runtime kit without headers and example

- Date: 2026-11-27
- Contract: §127.1, §127.2, §127.4, §129
- Status: accepted (2026-11-27, contract 26.11.27). Amends D-0055 point 1 (the runtime kit's contents) and
  its last consequence (the source folder's and the script's names), and D-0041 point 1 (where the developer
  kit takes `read-disk.c` from). The archive names, the fixed-name copy and the developer kit's gates stay as
  they were.

## Context

The runtime kit began as the evaluation kit (contract 0.16): the library, the project's header and one example,
so that an evaluator could build a small consumer and try the library. The developer kit (D-0041) later took
over that role with both headers, four examples, a pkg-config file and a guide, and D-0055 renamed the
evaluation kit to the runtime kit with its contents unchanged.

So the two kits differed only in a few text files, and the runtime kit still carried what only a developer
needs. The programs that use the runtime kit load the library and need neither headers nor examples: nbdkit's
VDDK plugin, virt-v2v, and backup and migration products. The developer kit, on the other hand, lacked the
`lib64` link that lets nbdkit use a kit's directory as `libdir`.

## Decision

1. The runtime kit holds what a program needs to run with the library: the library and its `.so.8` and `.so`
   links, `lib64`, `bin/openvddk-admin` and `ADMIN.md`, the README, `KNOWN-DIFFERENCES.md`, the licence files,
   the SBOM, `BUILD.txt` and `SHA256SUMS`. It no longer carries `include/` or `examples/`.
2. `openvddk-admin` stays in the runtime kit. It is an operations tool: the README's recovery after a crash
   and its steps for reporting a problem use it.
3. The runtime kit's gate no longer builds the example. It checks instead that the library's SONAME is
   `libvixDiskLib.so.8.0.3`, that the library exports every function the project's header declares, and that
   `lib/libvixDiskLib.so`, `lib/libvixDiskLib.so.8` and `lib64/libvixDiskLib.so.8` reach it.
4. The README says that the headers and examples are in the developer kit.
5. `read-disk.c` moves from `evalkit/examples/` to `devkit/examples/`, which already built it; the runtime
   kit's own example Makefile is removed.
6. The developer kit carries `lib64`, a link to `lib`, as the runtime kit does.
7. The source folder `evalkit/` is renamed `runtime/`, and the script `tools/evalkit.sh` is renamed
   `tools/runtime.sh`, so that the source names match the kit's name. `make evalkit` stays as the old name of
   `make runtime`.

## Consequences

- The developer kit holds everything the runtime kit holds, with the runtime kit's README as `OPERATIONS.md`.
- A program built against the runtime kit's header must take the headers from the developer kit; nothing
  else changes for it, and the headers themselves are unchanged.
- The archives keep their names; installers that unpack the runtime kit and use `lib/`, `lib64/` or
  `bin/` keep working.
- `runtime/` holds only texts of the runtime kit: its README and `KNOWN-DIFFERENCES.md`.
- Old records keep their wording and paths (`evalkit/README.md`, `make evalkit`); current documents name
  `runtime/`.

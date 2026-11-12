# NBD-A002 — OpenVDDK writing over a direct connection to an ESXi host (D-A002)

- **Why:** D-A002 writes over NBDSSL and NBD also on a direct connection to
  an ESXi host (D-0052). NBD-A001 ran through vCenter only.
- **Library:** OpenVDDK built in the EL8 build container from a
  development version of this change (SHA-256 `a09a6ce0…`), which also
  wrote over HotAdd (D-A003). The read-backs used VMware VDDK 9.1
  (`c955e08f…`).
- **Environment:** ESXi 8.0.3 build 24022510, managed by vCenter 8.0.3
  build 24022515. The program signed in to the host as root and named the
  target VM (powered off) by the host's own ID. The configuration file
  enabled both `OpenVDDK.hotadd.write` and `OpenVDDK.nbd.write`.
- **Date:** 2026-11-06. **Evidence:** evidence kept privately (the program's
  output and the library's log for every case).

## Results

| Case | Transport list | What | Result |
|---|---|---|---|
| esx1 | `nbdssl` | 8 MiB in 4 MiB `Write` calls, `Flush`, `Close` | 0 for every call; read back exactly |
| esx2 | `nbd` | 4 MiB, `Flush`, `Close` | 0 for every call; read back exactly |
| esx3 | `hotadd:nbdssl` | 4 MiB, `Flush`, `Close` | HotAdd was refused because the connection is to an ESXi host (D-0052); the open moved on to NBDSSL with the warning "HotAdd cannot write this disk here; trying the next listed transport". Then 0 for every call; read back exactly |
| esx4 | `hotadd` | an open for writing | Open 13: HotAdd is unavailable on this connection, and nothing writable follows it in the list |

The write ticket came from the host's NFC service, as VMware's library
asks for it there (NET-A003).

## Not covered

- A host with the free "vSphere 8 Hypervisor" licence, which refuses API
  writes (NBD-0031).
- ESXi 6.7 and 7.0 directly.

# NBD-0033 — The host ends an NFC session during the reads when its NFC memory is full

- **Why:** the same host refusal gave different codes: VMware's library 1,
  OpenVDDK 0.7.4 14009, and OpenVDDK's log only said "bad frame magic 0x4".
- **Source:** our NFC limits tests, phase 1 (2026-11-03): many readers of
  one disk at once with 2 MiB buffers, through vCenter 9.1.1, directly on
  ESXi 9.1.1 and through vCenter 7.0.3, default host settings.
- **Date:** 2026-11-03 (test), 2026-11-22 (change).

## What was seen

- With 2 MiB buffers (`vixDiskLib.nfcAio.Session.BufSizeIn64KB = 32`) the
  host served 16 NFC sessions at once and refused the 24th, with either
  library; the limit follows sessions × buffer size, which fits a host
  memory budget. Sessions already running were not affected.
- The refusal came during the reads, not at `Open`: the host sent the
  classic message 4 (SessionComplete) where a read reply was due, and
  ended the session.
- VMware's library: the read failed with error 1 (once 16000); its log says
  `NFC_SESSION_ERROR` and nothing about the cause.
- OpenVDDK 0.7.4: the read failed with 14009; its log said "unexpected
  reply from the host: bad frame magic 0x4".

## Change

From 0.7.5:

- A SessionComplete where an AIO reply was expected is
  `nfc.ErrSessionEnded` (still also `nfc.ErrProtocol`, and named in the
  error text since NBD-0032).
- A read or asynchronous read that fails this way returns 1, as the
  reference does (`nbdReadCode`). A lost connection (end of file, reset,
  no reply within the timeout) stays 14009, as before (NBD-0002, NBD-0008,
  NBD-0028): when the management agents restart, the host closes the
  connection without a SessionComplete, and both libraries answer 14009.
- The first such failure of a disk logs one line naming the likely cause:
  "The ESXi host ended the NFC session for this disk: it accepted no more
  transfers. This usually means the host's NFC memory is full (too many
  sessions at once, or 2 MiB buffers). Run fewer disks at once, use fewer
  OpenVDDK.nbd.sessions or the default buffers, or raise the host's NFC
  memory (VMware KB 389223). Transfers already running are not affected."
- `GetErrorText(1)` keeps the standard text.

Tests: `TestParseAIOHeaderSessionComplete`, `TestNBDReadCodeSessionEnded`.

## Not verified yet

A refused session against a real host with the 0.7.5 candidate (the 0.7.5
release checks repeat the 24-session case).

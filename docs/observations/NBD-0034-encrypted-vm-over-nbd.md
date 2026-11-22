# NBD-0034 — An encrypted VM asked over NBD: the reference encrypts the NFC stream and reports NBD

- **Why:** before 0.7.5, OpenVDDK could not read an encrypted VM over NBD,
  while VMware's library could.
- **Source:** our encrypted-VM comparison on vSphere 7.0.3 (ESXi running as
  a virtual machine): a clone of the 20 GiB test VM with the VM Encryption
  policy applied to its home and disk (the VM and its disk carry a key ID);
  VMware VDDK 8.0.3 against OpenVDDK 0.7.4, one snapshot, every transport
  list.
- **Date:** 2026-11-03 (test), 2026-11-22 (change).

## What was seen

| Transport list | VMware 8.0.3 | OpenVDDK 0.7.4 |
|---|---|---|
| `nbdssl` | reads; same SHA-256 as the unencrypted control | reads; same SHA-256 |
| `nbd` | reads; same SHA-256; reports `nbd`; its log shows it opening `vpxa-nfcssl://…`, an encrypted NFC connection | fails at `Open` with 13: the host answers the clear-text NFC open with an error reply |
| `hotadd` (the proxy itself not encrypted) | fails at `Open` with 13 | fails at `Open` with 13 |
| `nbdssl:nbd`, `hotadd:nbdssl:nbd` | read over NBDSSL | read over NBDSSL |

So the host requires an encrypted NFC stream for an encrypted disk, and
the reference provides one when NBD was asked for, without saying so to
the application.

## Change

From 0.7.5, `Open` over NBD of a disk whose backing has a key ID (the
disk details it already reads from vCenter) uses an encrypted NFC stream,
as for NBDSSL, and `GetTransportMode` still reports `nbd`, as the
reference does. One log line says why: "the disk is encrypted, so the host
accepts it only over an encrypted NFC stream; NBD was asked for, and the
stream is encrypted as with NBDSSL". Unencrypted disks over NBD stay clear
text. Test: `TestNBDStreamEncrypted`.

## Not verified yet

A read of the encrypted test VM over NBD with the 0.7.5 candidate (part of
the 0.7.5 release checks); vSphere 8 and 9; HotAdd through an encrypted
proxy.

# NET-A003 — The NFC ticket VMware's library asks for to write a disk

- **Why:** the first write open of OpenVDDK over NBDSSL (D-A002) used the
  ticket a read uses, and the host refused it. This record holds the call
  VMware's library makes instead.
- **Reference:** VMware VDDK 9.1 (`c955e08f…`), the reference for writes
  (D-A001).
- **Environment:** as NET-A002: vCenter 8.0.3 build 24022515, ESXi 8.0.3
  build 24022510, the target VM powered off, the program on the
  worker the worker VM.
- **Method:** a TLS relay on the worker. The program was given the relay's
  loopback address and certificate thumbprint as its vCenter. The relay
  forwarded to vCenter, pinning vCenter's real thumbprint, and recorded
  both directions, as in NET-0001. Two runs with `vixwrite` over NBDSSL:
  - rw1: a write open, one 64 KiB `Write`, `Flush`, `Close`;
  - rr1: a read-only open of the same region (read back exactly).
- **Date:** 2026-11-06. **Evidence:** evidence kept privately (the two
  recordings; operation names and argument structure are below, the session
  tokens are not).

## The calls

Both runs made the same 29 vCenter calls in the same order but one:

| Run | The call that gives the NFC ticket |
|---|---|
| rr1, read-only | `NfcGetVmFiles` on the NFC service object, with the VM (as NET-0002) |
| rw1, write | `NfcRandomAccessOpenDisk` on the NFC service object, with the VM and `diskDeviceKey` 2000, the device key of the disk opened |

The calls around it were the same in both:
- `RetrieveServiceContent`, `Login`;
- property fetches, `FindByUuid`, `FindAllByUuid`;
- `RetrieveInternalContent`, then the ticket call;
- `FindAllByDnsName`, `QueryNetConfig`, more property fetches, `Logout`.

`NfcRandomAccessOpenDisk`'s answer has the same fields as `NfcGetVmFiles`'s:
`host`, `port` (902), `sslThumbprint`, `service` (`vpxa-nfc`),
`serviceVersion` (1.1) and `sessionId`.

## Through an ESXi host directly

The same two runs were repeated with the program signed in to the target's
ESXi host as root, the VM named by the host's own ID (58081). The relay sat
between the program and the host's port 443, and a plain TCP forward carried
port 902 to the host: on a direct connection the ticket names no host, and
the program connects to the server it signed in to (NET-0005).

| Run | The call that gives the NFC ticket |
|---|---|
| read-only | `NfcGetVmFiles` on `ha-nfc-service`, with the VM |
| write | `NfcRandomAccessOpenDisk` on `ha-nfc-service`, with the VM and `diskDeviceKey` 2000 |

Both answers named no host, service `nfc`, port 902. The write and the read
back succeeded over NBDSSL. The other calls were the same in both runs.

## OpenVDDK with the ticket of a read

Before this record, OpenVDDK opened the disk for writing with a ticket from
`NfcGetVmFiles`. The host refused the open with an `ERROR` frame of kind 11
and disk-library code 0, and its text named a failed permission check for
the file. Open answered 13. With a ticket from `NfcRandomAccessOpenDisk`
the same open succeeded (NBD-A001).

## What this means

- A write open over NBD or NBDSSL asks for its ticket with
  `NfcRandomAccessOpenDisk`, giving the VM and the disk's device key.
- The vCenter privilege this call needs was not checked here. A role
  without it is to be tried before a release with these writes.
- On a direct connection to an ESXi host the same call goes to the host's
  NFC service.

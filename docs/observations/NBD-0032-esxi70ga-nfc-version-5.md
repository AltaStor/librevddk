# NBD-0032 — ESXi 7.0 GA answers NFC version 5 and ends the session on the resource pool request

- **Why:** a user reported that OpenVDDK 0.7.4 cannot open disks on a
  standalone ESXi 7.0.0 GA host (build 15843807) over NBDSSL, with
  `unexpected reply from the host: bad frame magic 0x4`.
- **Source:** the public issue openvddk/openvddk#1, reported and diagnosed by
  AdminSys (ProxCenter): a lab reproduction, a capture of both libraries on
  the wire, the cause, and a change that fixed it on that host. Not yet
  reproduced in our own lab.
- **Date:** 2026-11-07 (report), 2026-11-08 (diagnosis and change).

## What was seen

- The host answers NFC version **5**.
- OpenVDDK 0.7.4 sends connection data (54), features (55) and the AIO
  session (52) to every host from version 3, then the AIO open session,
  the socket options and the resource pool request (AIO type 22). The host
  accepts everything up to the resource pool request, then sends the classic
  message 4 (SessionComplete) and ends the session. OpenVDDK read that
  message as an AIO header, hence "bad frame magic 0x4".
- The reference sends this host connection data only: no features and no
  resource pool request.

## Versions we have recorded ourselves

| ESXi | NFC version | Features and resource pool request |
|---|---|---|
| 6.7 | 2 | none (NBD-0005) |
| 7.0 GA | 5 | none (this record) |
| 7.0 Update 3 | 9 | all three (NET-0003) |
| 8.0 | 11 | all three |
| 9.1.1 | 12 | all three |

7.0 Update 1 and Update 2 are not recorded.

## Change

From 0.7.5: connection data from version 3, as before; features and the
resource pool request only from version 9 (`internal/nfc/nfc.go`,
`poolVersion`). Nothing changes for the recorded versions 2, 9, 11 and 12.
A handshake failure now names the step and the host's NFC version, and a
SessionComplete in place of an AIO reply is named as the host ending the
session. Tests: `TestNFCVersion5GetsNoFeaturesOrResourcePool`,
`TestHandshakeErrorNamesStepAndVersion`.

## Not verified yet

A run against a real 7.0 GA host with the 0.7.5 candidate (offered by the
reporter), and an unchanged handshake on 7.0 Update 3, 8.0 and 9.1.1 in our
own lab.

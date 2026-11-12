# Messages the library emits (current inventory)

Every log statement of the library and every message it causes elsewhere,
for the version in this tree (0.6.10). LOG-0001 is the dated comparison
with the reference that started this list (it inventories 0.6.7); this
file is kept current. When a log statement is added, changed or removed,
update this file in the same commit. `tools/messages-lines.py` checks that
every line reference is the log statement its row quotes, and with
`--fix` moves drifted references; run it before committing a change to a
file listed here.

## How a line is built and delivered

- **Format (D-0021).** `OpenVDDK: VixDiskLib: ` + the function the line
  belongs to and a colon (`VixDiskLib_OpenEx: `) when it belongs to one +
  the text. Steps inside a call carry that call's name: the attach and
  guest discovery `VixDiskLib_OpenEx`, the detach `VixDiskLib_Close`,
  recovery during Cleanup `VixDiskLib_Cleanup`. Secrets are redacted
  (`internal/logx/logx.go`, `Redact`).
- **Level.** LOG or WARN, below. A WARN line goes to the consumer's warn
  callback; without one, to the log callback with `WARNING: ` at the start
  of the text (D-0022). The library never emits PANIC today.
- **Without callbacks** (D-0022): between Init and Exit, a line no
  callback takes goes to `<tmp>/openvddk-<user>/openvddk-<pid>.log`;
  `OpenVDDK.trace.file` sends a copy of every line to a chosen file.
- **Class.** Every line below is external: it is delivered to the
  consumer. The library keeps no internal log; its only internal records
  are the HotAdd journal (`/var/lib/openvix/hotadd/<id>.json`, with the
  last error of a failed step) and the slot lock.

## Log statements

### Start-up and configuration (no function name)

| Where | Level | Text | When |
|---|---|---|---|
| internal/lifecycle/lifecycle.go:120 | LOG | `OpenVDDK <version> (<commit>), independent implementation; VixDiskLib 8.0.3 ABI (…)` | first successful Init/InitEx |
| internal/lifecycle/lifecycle.go:121 | LOG | `initialized for client %d.%d; available transport modes: %s` | same |
| internal/lifecycle/lifecycle.go:118 | LOG | `re-initialized for client %d.%d` | Init/InitEx while initialized |
| internal/lifecycle/lifecycle.go:125 | LOG | `writes over %s enabled by %s: a disk opened without the read-only flag may be written` | Init/InitEx, once for each transport family a switch outside the program turns writes on for: `OPENVIX_NBD_WRITE`, `OpenVDDK.nbd.write` in the configuration file, each one that does (D-A001) |
| internal/lifecycle/lifecycle.go:106 | WARN | `writes: %s` | Init/InitEx, once for each problem with a switch: a value that is not a switch value in the environment or in the configuration file; writes over that family stay off from that source (D-A001) |
| internal/lifecycle/lifecycle.go:64 | WARN | `client requested VixDiskLib %d.%d; this library provides 8.0` | client version above 8.0 (the call fails) |
| internal/lifecycle/lifecycle.go:93 | WARN | `configuration file not used: %v` | the configuration file cannot be read |
| internal/lifecycle/lifecycle.go:96 | WARN | `configuration: %s` | one line per problem in the file (`internal/config/config.go:89`, `:97`, `:101`, `:110`, `:115`: malformed line, unterminated quote, repeated key, unknown project key) |
| internal/lifecycle/lifecycle.go:89 | WARN | `configuration: %s` | Init/InitEx when `vixDiskLib.transport.LogLevel` (0 to 6) or `vixDiskLib.nfc.LogLevel` (0 to 4) is not a level; every message is then delivered (D-0049) |
| internal/lifecycle/lifecycle.go:99 | WARN | `configuration: %s has no effect in this library: %s` | Init/InitEx, once for each key VMware documents that is set in the file but not acted on, with the reason (`internal/config/config.go`, `notHonoured`; D-0048) |
| internal/lifecycle/lifecycle.go:102 | WARN | `OpenVDDK.trace.file %q not used: %v` | the configured log file cannot be opened |
| internal/core/core.go:1146 | WARN | `OpenVDDK.hotadd.readAheadKB = %q is not 0 or a multiple of 4 up to 65536; using %d` | HotAdd Open with an invalid value |
| internal/core/core.go:1154 | WARN | `OpenVDDK.hotadd.readAheadDepth = %q is not 1 to 64; using %d` | same |
| internal/core/nbd.go:137 | WARN | `OpenVDDK.nbd.inFlight = %q is not 1 to 64; using %d` | Open over NBD or NBDSSL with an invalid value (D-0037) |
| internal/core/nbd.go:144 | WARN | `OpenVDDK.nbd.sessions = %q is not 1 to 16; using %d` | same (D-0045) |
| internal/core/nbd.go:169 | WARN | `vixDiskLib.nfcAio.Session.BufSizeIn64KB = %q is not 1 to 32; the host's default is used` | Open over NBD or NBDSSL with VMware's documented key out of its range (D-0048) |
| internal/core/nbd.go:200 | WARN | `vixDiskLib.nfcAio.Session.BufCount = %q is not 1 to 16; the host's default is used` | same (D-0048) |
| internal/core/nbd.go:176 | WARN | `vixDiskLib.nfc.ReadTimeoutMs = %q is not a positive number of milliseconds; the default is used` | Open over NBD or NBDSSL with VMware's key not a positive number (D-0049) |
| internal/core/nbd.go:192 | WARN | `%s = %q is not a positive number of bytes; the default is used` | the same for one of the four `vixDiskLib.nfcAio.SocketOption` keys (D-0049) |
| internal/core/nbd.go:155 | WARN | `OpenVDDK.nbd.readAheadKB = %q is not valid; using %d` | same |
| internal/core/nbd.go:162 | WARN | `OpenVDDK.nbd.readAheadDepth = %q is not 1 to 64; using %d` | same |
| internal/core/core.go:1170 | WARN | `OpenVDDK.hotadd.reconfigureReads = %q is not 0 to 64; using %d` | same (D-0024) |

### `VixDiskLib_ConnectEx`

| Where | Level | Text | When |
|---|---|---|---|
| internal/core/core.go:358 | LOG | `connection to %s, transports %v, snapshot %q, read-only %v` | ConnectEx to a server |
| internal/core/core.go:346 | LOG | `transports %q are not supported and are skipped` | the consumer's list names `file`, `san` or a name the library does not know (or a name in another spelling); they are removed from it, the rest keeps its order (NBD-0019, NBD-0021). A list with nothing usable fails ConnectEx with VIX_E_INVALID_ARG |
| cabi/exports.go (ConnectEx failure) | WARN | `No transport modes available to access disks: hotadd needs a VMware virtual machine` (inside the ConnectEx failure line) | the list's only usable transport is hotadd and this machine is not a VMware virtual machine (NBD-0021) |
| internal/core/core.go:479 | LOG | `logged in to %s: %s` (product, version, build, API) | every successful sign-in; tagged with the call that signs in (ConnectEx, OpenEx, Close, PrepareForAccess, EndAccess, Cleanup) |
| internal/core/core.go:420 | LOG | `sign-in failed, reported at Open: %v` | HotAdd requested, sign-in failed |
| internal/core/core.go:425 | LOG | `HotAdd proxy check left to Open: %v` | HotAdd requested, the proxy check could not run |
| internal/core/core.go:365 | WARN | `No transport modes available to access disks: %v` | HotAdd the only transport the connection could use (`hotadd` alone, or no list) and the BIOS UUID selects several VMs (D-0018, NBD-0013; the call fails with 3) |
| cabi/exports.go:175 | WARN | `%v` (the error chain) | ConnectEx/Connect failed. An error without an operation name now starts with the code's text (`VixDiskLib_ConnectEx: An argument is not valid (VIX_E_INVALID_ARG): …`; LOG-0001, gap G9, fixed in 0.6.9) |
| cabi/exports.go:154 | WARN | `local (hosted) disk access is not supported by this version` | ConnectEx without a server |

### `VixDiskLib_OpenEx` (Open and the steps inside it)

| Where | Level | Text | When |
|---|---|---|---|
| internal/core/core.go:590 | LOG | `Open disk '%s' (read-only, flags %#x) over %v` | Open starts |
| internal/core/core.go:592 | LOG | `Open disk '%s' for writing (flags %#x) over %v` | Open without `VIXDISKLIB_FLAG_OPEN_READ_ONLY` starts, once writes are switched on (D-A001) |
| internal/hotadd/hotadd.go:341 | WARN | `HotAdd: host safety: %s` | host safety failed and `OpenVDDK.hotadd.hostSafety = "warn"` (G12 fixed in 0.6.9: a warning on the warn level) |
| internal/hotadd/hotadd.go:357 | LOG | `HotAdd: recovery before open: cleaned=%d remaining=%d errors=%v` | Open found orphaned transactions of this proxy |
| internal/hotadd/hotadd.go:209 | LOG | `HotAdd: %s: reads in flight did not come down to %d per device within %s; reconfiguring anyway` | an attach, detach or recovery detach found reads still in flight after 5 s (D-0024; also under `VixDiskLib_Close` and `VixDiskLib_Cleanup`) |
| internal/hotadd/recover.go:163 | LOG | `HotAdd: recovered transaction %s (%s at scsi%d:%d)` | each orphan recovered (under `VixDiskLib_Cleanup` when Cleanup recovers) |
| internal/hotadd/hotadd.go:413 | LOG | `HotAdd: %s has no disk ID settings and OpenVDDK.hotadd.configureProxy is false; …` | proxy without disk IDs, changes forbidden |
| internal/hotadd/hotadd.go:416 | WARN | `HotAdd: cannot set disk.EnableUUID and disk.AllowDupUUID on %s (%v); …` | switching the settings on failed (G12 fixed in 0.6.9: warn level) |
| internal/hotadd/hotadd.go:419 | LOG | `HotAdd: set disk.EnableUUID and disk.AllowDupUUID to TRUE on %s, as the reference does; …` | the settings were switched on |
| internal/vsphere/hostconn.go:87 | WARN | `HotAdd: %s: the worker's host is not connected to vCenter; trying again in %s` | vCenter refused the attach (or, under Close, the detach) because the worker's host is not connected; tried again every 2–5 s (0.6.25, HOTADD-0048) |
| internal/vsphere/hostconn.go:94 | LOG | `HotAdd: %s: an earlier try took effect` | before a new try the worker already shows the disk attached (or, under Close, gone) |
| internal/vsphere/hostconn.go:79 | LOG | `HotAdd: %s: done after the worker's host was connected again (%d tries)` | the attach (or detach) succeeded after tries |
| internal/vsphere/hostconn.go:84 | WARN | `HotAdd: %s: the worker's host is still not connected to vCenter after %s; giving up` | the host stayed away for 60 s (attach; Open then fails with 13) or 90 s (detach; Close fails, the journal is kept) |
| internal/hotadd/hotadd.go:483 | LOG | `HotAdd: attached %s to %s at scsi%d:%d in %s` | attach task finished |
| internal/hotadd/hotadd.go:711 | LOG | `HotAdd: redo log location not reported (%s=%q, err=%v)` | ESXi did not report the redo log (also under Close) |
| internal/hotadd/hotadd.go:716 | LOG | `HotAdd: redo log %s is outside the proxy directory; left to ESXi` | unexpected redo log place (also under Close) |
| internal/hotadd/hotadd.go:527 | LOG | `HotAdd: %s is %s (%s), verified by %s; proxy identified by %s` | guest device found and verified |
| internal/hotadd/hotadd.go:529 | WARN | `HotAdd: %s carries LVM physical volume %s, as does this worker's own %s; if the worker restarts while the disk is attached, LVM may take the source disk for its own and the worker may not start (HOTADD-0027). …` | the source disk and the worker share an LVM physical volume UUID (one template) (D-0030); the remedy it names was proven in HOTADD-0032 |
| internal/hotadd/hotadd.go:785 | LOG | `HotAdd: releasing %s in the guest: %v` | rollback or recovery could not release the guest device |
| internal/core/core.go:666 | LOG | `HotAdd: open timing for %s: login=… … total=…` | Open succeeded |
| internal/core/core.go:668 | LOG | `%s: open timing for %s: login=… … total=…` | Open succeeded over NBD or NBDSSL (the transport first) |
| internal/core/nbd.go:311 | LOG | `%s: opened over %s through %s (NFC version %d)` | Open over NBD or NBDSSL: the transport, the host address used and its NFC version |
| internal/core/nbd.go:319 | LOG | `%s: reads spread over %d NFC sessions` | Open over NBD or NBDSSL with `OpenVDDK.nbd.sessions` above 1: the sessions the disk is open in (D-0045) |
| internal/core/nbd.go:285 | LOG | `%s: open for writing in one NFC session; OpenVDDK.nbd.sessions = %d applies to reads` | Open for writing over NBD or NBDSSL when `OpenVDDK.nbd.sessions` is above 1 (D-A002) |
| internal/core/nbd.go:313 | WARN | `%s: open for writing over %s through %s` | every Open for writing over NBDSSL or NBD: the transport and the host (D-A002) |
| internal/core/nbd.go:315 | WARN | `%s: NBD carries the data written in clear text: on the network it can be read and changed; NBDSSL encrypts it` | every Open for writing over NBD (D-A002) |
| internal/core/nbd.go:263 | LOG | `%s: NFC read timeout %v, socket buffers %d/%d bytes, the host's asked %d/%d bytes (0: the default; the host's at most %d)` | Open over NBD or NBDSSL when the configuration sets `vixDiskLib.nfc.ReadTimeoutMs` or a `vixDiskLib.nfcAio.SocketOption` key (D-0049) |
| internal/core/nbd.go:267 | LOG | `%s: NFC AIO buffers asked of the host: size %d KiB, count %d (0: the host's default)` | Open over NBD or NBDSSL when the configuration sets `vixDiskLib.nfcAio.Session.BufSizeIn64KB` or `.BufCount` (D-0048) |
| internal/core/nbd.go:295 | WARN | `%s: NFC session %d of %d not opened (%v); reading in %d` | Open over NBD or NBDSSL when a further session cannot be opened (ticket, connection or the host's open refused); Open succeeds with the sessions already open (D-0045) |
| internal/core/nbd.go:239 | WARN | `%s: this CPU gives the library no AES instructions, so NBDSSL decrypts in software and reads several times more slowly; give this virtual machine the host's CPU features (for example libvirt's host-passthrough or host-model CPU mode)` | Open over NBDSSL on a CPU whose AES instructions Go's crypto does not see (NBD-0023) |
| internal/core/nbd.go:246 | WARN | `%s: %s refused AES-128-GCM alone for the NFC stream; connecting again with the full cipher list` | Open over NBDSSL on such a CPU when the host refuses the AES-128-GCM offer for the data stream; a new ticket, the full list, never clear-text NBD for this reason |
| internal/core/nbd.go:233 | LOG | `%s: the disk is encrypted, so the host accepts it only over an encrypted NFC stream; NBD was asked for, …` | Open over NBD of an encrypted disk (its backing has a key ID): the stream is encrypted as with NBDSSL and the transport is reported as NBD, as VMware's library does (NBD-0034) |
| internal/core/nbd.go:257 | LOG | `%s: NFC stream encrypted with %s` | Open over NBDSSL: the data stream's TLS version and cipher suite |
| internal/core/nbd.go:260 | LOG | `%s: reads ask the host to leave zero data out (skipz)` | Open over NBD or NBDSSL when the application opened with `VIXDISKLIB_FLAG_OPEN_COMPRESSION_SKIPZ` (NET-0004) |
| internal/core/core.go:293 | LOG | `%v. Trying the next listed transport: %s.` | HotAdd skipped with another transport after it in the list, for one of two reasons: `HotAdd works only from a VMware virtual machine; this machine is not one` (NBD-0021) or `HotAdd is available only through vCenter, not on a direct connection to an ESXi host` (D-0052); a warning with other wording until 0.7.5, which nbdkit printed as an error |
| internal/core/core.go:295 | WARN | `%v. No other transport is listed.` | HotAdd skipped for one of the same two reasons as the last listed transport; `Open` then fails with 1 outside a VMware virtual machine (NBD-0021) and 13 on a direct ESXi connection (D-0052) |
| internal/core/core.go:297 | WARN | `transport %s: %v` | a listed transport failed (a HotAdd attach, an NBD or NBDSSL open) |
| internal/core/core.go:679 | WARN | `%s: NBDSSL is not available for this session; the transport list allows NBD, which carries the data without encryption` | the host refused TLS for the NFC stream and nbd is listed after nbdssl; Open goes on to NBD (D-0039) |
| internal/core/core.go:789 | WARN | `descriptor of %s not readable (%v); GetInfo uses what is known` | a descriptor of the chain cannot be downloaded |
| internal/core/core.go:794 | WARN | `descriptor of %s not parsed (%v); GetInfo uses what is known` | a descriptor cannot be parsed |
| internal/core/core.go:798 | WARN | `descriptor of %s says %d sectors, vSphere %d; using vSphere` | sizes disagree |

### `VixDiskLib_Open` (the failure line)

| Where | Level | Text | When |
|---|---|---|---|
| cabi/exports.go:203 | WARN | `Cannot open disk %s. Error %d (%s): %v` | Open failed. Reasons (0.6.9 on) include: no snapshot while the VM is powered on (13, from 0.6.13; a powered-off VM is read without one, D-0031); the disk is not in the VM (A16), a file of its chain is missing (A10), the proxy's host cannot see its datastore (A7), an independent disk (A8; from 0.7.0, 01e92be, answered as not a disk of the VM over every transport, 1 with nbd alone, as the reference does, NBD-0012), no free SCSI slot (A2), a physical-mode RDM (before 01e92be every RDM) or an encrypted disk — all with code 13 —; a rejected sign-in (3014); TLS identity not verified (1); unknown snapshot (13003); the same disk already attached (1) |

### `VixDiskLib_Read`, `GetInfo`, `GetTransportMode`, `QueryAllocatedBlocks`

| Where | Level | Text | When |
|---|---|---|---|
| internal/core/core.go:974 | WARN | `Read %d sectors at %d of %s failed: error %d (%s): %v` | a Read failed: the device returned an I/O error (7, as the reference), the device went away (1, as the reference), the range is outside the disk (catalogue S1, S2; HOTADD-0023) |
| internal/core/core.go:971 | WARN | `Read %d sectors at %d of %s failed: error %d (%s): %v` | the same over NBD or NBDSSL, as an NFC message (it follows `vixDiskLib.nfc.LogLevel`, D-0049) |
| internal/core/nbd.go:408 | WARN | `The ESXi host ended the NFC session for this disk: it accepted no more transfers. …` | once per disk, after the first read (or ReadAsync) the host ended with SessionComplete, which returns 1: the host's NFC memory is the likely cause; the line says what to do (NBD-0033) |
| internal/core/write.go:124 | WARN | `Write %d sectors at %d of %s failed: error %d (%s): %v` | a Write failed on a disk open for writing over NBDSSL or NBD, as an NFC message (D-A002): 8 when the host ran out of space, 7 for another refusal, 14009 for a lost session and for every write after the host ended the session |
| internal/core/write.go:151 | WARN | `Flush of %s failed: error %d (%s): %v; the data written since the last successful Flush is undefined` | a Flush failed over NBDSSL or NBD, or follows a failed write or flush, whose error it returns (D-A002) |
| internal/core/async.go:127 | WARN | `ReadAsync %d sectors at %d of %s failed: error %d (%s): %v` | an asynchronous read over NBD or NBDSSL failed (14009 when the NFC connection was lost, as the reference); the callback receives the code |
| internal/core/nbd.go:94 | LOG | `%s: the NFC session had already ended (%v); nothing to release on the host` | Close over NBD or NBDSSL after a read saw the connection end; Close succeeds |
| internal/core/nbdwrite.go:146 | WARN | `%s: closing the file after the writes: %v` | Close of a disk open for writing over NBDSSL or NBD, when closing the file or the session fails after the flush (D-A002) |
| internal/core/core.go:1012 | LOG | `read %d GiB of %s so far: %.0f MiB/s since open; %.0f s of %.0f s spent inside Read` | every 10 GiB read on one handle |
| internal/core/core.go:1057 | LOG | `%s: %d sectors, adapter %d, %d link(s)` | every GetInfo |
| internal/core/core.go:1067 | LOG | `%s: %s` (disk, transport) | every GetTransportMode |
| internal/core/core.go:1104 | LOG | `%s: sectors %d+%d chunk %d: %d block(s), %d sectors in use (the ESXi host's map over NFC)` | every QueryAllocatedBlocks with a non-empty range, answered by the host over NFC as the reference (D-0036, NET-0002) |
| internal/core/core.go:1114 | LOG | `%s: sectors %d+%d chunk %d: %d block(s), %d sectors in use` | every QueryAllocatedBlocks with a non-empty range, answered from vCenter's allocation map when NFC is not available (D-0035, D-0036) |
| internal/core/core.go:1118 | LOG | `%s: sectors %d+%d chunk %d: 1 block of %d sectors (%s reports the range as allocated)` (HotAdd, NBDSSL or NBD) | every QueryAllocatedBlocks with a non-empty range when there is no allocation map |
| internal/core/allocation.go:102 | LOG | `%s: the ESXi host's allocation map is not available over NFC (%v); using vCenter's map` | the first QueryAllocatedBlocks on a handle whose NFC query fails (port 902 not reachable, ticket refused for a missing privilege, which the message names, protocol error); the handle then uses vCenter's map (D-0036) |
| internal/core/allocation.go:146 | LOG | `%s: allocation map from vCenter: %d area(s), %d of %d sectors in use` | the first QueryAllocatedBlocks on a handle, when vCenter answers (D-0035) |
| internal/core/allocation.go:124 | LOG | `%s: vCenter's allocation map is not available (%v); every range is reported as allocated` | the first QueryAllocatedBlocks on a handle, when vCenter refuses its map for a missing privilege (Allow read-only disk access); the message names it |
| internal/core/allocation.go:128 | LOG | `%s: vCenter has no allocation map for this disk (it needs changed block tracking): %v; every range is reported as allocated` | the first QueryAllocatedBlocks on a handle, when vCenter refuses (no changed block tracking on the disk) |
| internal/core/allocation.go:119 | LOG | `%s: no allocation map (%v); every range is reported as allocated` | the first QueryAllocatedBlocks on a handle, when no vCenter session can be had |
| internal/core/allocation.go:113 | LOG | `%s: the path names a file below the snapshot point; the snapshot point's allocation map covers it` | the first QueryAllocatedBlocks on a handle opened by a chain file below the snapshot point (A10) |

### `VixDiskLib_Close` (Close and the detach)

| Where | Level | Text | When |
|---|---|---|---|
| internal/core/core.go:871 | LOG | `Close disk '%s'` | Close starts |
| internal/hotadd/hotadd.go:576 | LOG | `HotAdd: journal: %v` | Close could not journal its start |
| internal/hotadd/hotadd.go:612 | LOG | `HotAdd: releasing %s in the guest: %v` | removing the guest SCSI device failed |
| internal/hotadd/hotadd.go:600 | LOG | `HotAdd: listing the guest's disks before releasing %s: %v` | Close could not list the guest's disks to check the device before removing it; the device is left, the detach goes on |
| internal/hotadd/hotadd.go:608 | WARN | `HotAdd: %s is no longer the attached disk (it was %s, %d sectors); not released in the guest` | at Close the device name belongs to another device (the disk went away during the session and the name was reused); that device is left alone (SECURITY-REVIEW-0.7.0) |
| internal/hotadd/hotadd.go:653 | LOG | `HotAdd: detached %s from %s in %s` | detach task finished; the retry lines listed under Open (`internal/vsphere/hostconn.go`) also appear here for the detach |
| internal/hotadd/hotadd.go:590 | LOG | `HotAdd: close timing for %s: stop-reads=… … total=…` | every Close of a HotAdd disk |
| internal/core/metadata.go:112 | WARN | `%s: metadata not available: %v` | the descriptors or the first sector could not be read on the first metadata call (D-0034); the call fails with 1 |
| internal/vsphere/proxy.go:343 | LOG | `HotAdd: %s: %.3f s from the call to its completion; in vCenter %.3f s queued and %.3f s running (task %s); %.3f s outside the task` | after every attach or detach reconfigure of the worker (P5, HOTADD-0034); no function name |
| internal/vsphere/proxy.go:346 | LOG | `HotAdd: %s: %.3f s from the call to its completion; vCenter reported no start and end time (task %s)` | the same, when the task's information lacks its times |
| internal/core/core.go:882 | WARN | `%s: detach failed, the vCenter session is no longer valid; signing in again to finish it` | the session was removed while the disk was open (A4) |
| internal/hotadd/recover.go:197 | LOG | `HotAdd: recovered transaction %s (%s at scsi%d:%d)` | the detach was finished after the new sign-in (A4) |
| internal/core/core.go:887 | WARN | `%s: finishing the detach after a new sign-in failed: %v` | the new sign-in or the recovery failed (A4) |
| internal/core/core.go:890 | WARN | `%s: %v` | Close failed; the journal record is kept for recovery |
| internal/core/core.go:1034 | LOG | `closed %s: %d reads, %d bytes, %.0f MiB/s since open; %.3f s of %.3f s spent inside Read; close took %.3f s` | Close succeeded |
| internal/core/core.go:1046 | LOG | `wrote %s: %d writes, %d bytes, %.0f MiB/s of Write time; %.3f s spent inside Write` | Close of a disk that was written |
| internal/core/core.go:1036–853 | LOG | `Disk Open Time: %d milliseconds.`, `Disk Close Time: …`, `Disk IO Time: …`, and with reads `Disk IO Size: %d sectors`, `Disk IO Count: %d`, `Disk IO Speed: %.2f MB/s` (no function name) | after the `closed` line (D-0021) |

### `VixDiskLib_Disconnect`

| Where | Level | Text | When |
|---|---|---|---|
| internal/core/core.go:513 | LOG | `disconnected from %s` | Disconnect of a connection with a session |
| internal/core/core.go:530 | LOG | `Backup Time:: %d milliseconds.` (no function name) | a disk was opened on the connection |
| internal/core/core.go:533–445 | LOG | `Backup Failure Function: %s`, `Backup Failure Error: %d` (no function name) | a call on the connection failed (the last one) |

### `VixDiskLib_PrepareForAccess`, `EndAccess`, `Cleanup`, `Exit`, `FreeConnectParams`, unsupported calls

| Where | Level | Text | When |
|---|---|---|---|
| internal/core/access.go:55 | LOG | `relocation of %s disabled for %q` (PrepareForAccess) | PrepareForAccess succeeded |
| internal/core/access.go:90 | LOG | `%v` (EndAccess) | re-enabling failed; EndAccess still returns success, as the reference does |
| cabi/exports.go:331 | WARN | `%v` (PrepareForAccess or EndAccess) | the call failed. On a connection to an ESXi host both calls fail after signing in, with 20 and the reason `the server is an ESXi host, not vCenter: relocation can be disabled and enabled only through vCenter`, as the reference answers them (D-0052, NET-0005) |
| cabi/exports.go:348 | WARN | `%v` (Cleanup) | Cleanup failed |
| internal/lifecycle/lifecycle.go:140 | LOG | `exit` (Exit) | Exit after a successful Init |
| cabi/exports.go:531 | WARN | `block was not allocated by this library; not freed` (FreeConnectParams) | a foreign block |
| cabi/exports.go:154 | WARN | `this call is not supported by this version` (under the call's own name) | CheckRepair, Rename, GetConnectParams and the connection and disk operations this version does not implement |

## Error texts

`VixDiskLib_GetErrorText` returns this project's own text for each of the
268 codes in `internal/vix/codes.go` (D-0005; none is identical to the
reference's published list since 0.6.9). Consumers show these texts.

## Messages elsewhere

| Cause | Where it shows |
|---|---|
| sign-in and sign-out | vCenter `UserLoginSessionEvent` / `UserLogoutSessionEvent` naming the client `OpenVDDK/<version> (VixDiskLib 8.0.3 compatible) <program>` (LOG-0001, gap G1); `BadUsernameSessionEvent` for a rejected password, once per connection |
| descriptor read for GetInfo | one `DatastoreFileDownloadEvent` per descriptor read at Open (G2, accepted until descriptors are read over NFC) |
| attach and detach | `ReconfigVM_Task` on the proxy with `TaskEvent` and `VmReconfiguredEvent`; the proxy VM's `vmware.log`; ESXi `vmkernel.log` |
| disk ID settings, redo log cleanup | `ReconfigVM_Task` and `DeleteDatastoreFile_Task`, only when needed |
| PrepareForAccess, EndAccess | vCenter's log only (`AuthorizationManager.disableMethods` / `enableMethods`); on a connection to an ESXi host only the sign-in and sign-out (D-0052) |
| guest device handling | the proxy's kernel and LVM journal |
| no callbacks registered | `<tmp>/openvddk-<user>/openvddk-<pid>.log` (D-0022) |

LOG-0001 Part 1c lists these with the code locations of 0.6.7.

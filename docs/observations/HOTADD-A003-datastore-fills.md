# HOTADD-A003 — A HotAdd write when the target's datastore fills (HW6)

- **Why:** HW6 of the HotAdd write qualification: a thin target on a
  datastore that fills during a write over HotAdd. The same condition over
  NBD and NBDSSL is in NET-A002.
- **Libraries:**
  - VMware VDDK 9.1 (`c955e08f…`), the reference for writes (D-A001);
  - OpenVDDK, the development build of HOTADD-A002.
- **Environment:**
  - vCenter 8.0.3 build 24022515, ESXi 8.0.3 build 24022510;
  - the target VM, powered off, with a second disk: 1 GiB, thin,
    alone on a VMFS datastore of 1.75 GiB with 347 MiB free, local to one
    host;
  - the target and the worker VM (Debian 13, 1 GiB of memory) moved,
    powered off, to that host, because the datastore is visible only there.
- **Method:**
  - the thin disk filled over NBD from 0 to 300 MiB first, which left 47 MiB
    free;
  - one write open over HotAdd then wrote from 300 MiB to the end in 4 MiB
    `Write` calls, in the background on the worker;
  - the worker VM's pending question and vCenter's events polled every few
    seconds;
  - the guest inspected through guest operations: the writer's kernel stack,
    the device's requests in flight, dirty memory, kernel messages;
  - the thin disk read back afterwards over NBD;
  - between cases the thin disk was deleted and created again.
- **Date:** 2026-11-06. **Evidence:** evidence kept privately (the programs'
  output, the polls, the event lists, the guest state).

## The out-of-space question

In every case, within 6 s of the attach, ESXi posted a question on the
**worker** VM:
- the message `msg.hbacommon.outofspace`, naming the target's disk file;
- two choices, `button.retry` and `button.abort` (Retry and Cancel).

While the question was pending the worker stopped: no answer to ping, no
guest operations. Neither library got an error from any call.

## VMware's library (hw6a)

- **Blocked.** The writer blocked in the kernel in `balance_dirty_pages`:
  the library writes to the device through the page cache.
- **Retry,** answered after 71 s: the worker ran again, but the write never
  returned. For the next 6 minutes:
  - no new question;
  - 14, then 40 requests in flight on the device;
  - about 315 MiB of dirty memory;
  - the guest kernel reported hung writeback tasks after 120 s;
  - no SCSI timeout and no I/O error in the guest.
- **Power-off.** The worker was powered off, as nothing could free space on
  the datastore. The library's local state did not survive the restart,
  because `/tmp` is in memory on this worker. The disk was detached from
  the worker through vCenter by hand.
- **Data.** Every sector up to the datastore's limit (sector 710656,
  347 MiB) read back as written; after it, zeros.

## OpenVDDK, buffered (hw6b)

- **Blocked.** The question came 2 s after the attach. It disappeared
  within 6 minutes with no answer task in vCenter's events. The writer
  blocked in `balance_dirty_pages`, as the reference did, with 21 to 43
  requests in flight on the device and about 316 MiB of dirty memory.
- **Kill.** After SIGKILL the process became a zombie. One of its threads
  stayed in the device's release, writing back dirty pages.
- **Cleanup.** `VixDiskLib_Cleanup` through OpenVDDK, with the zombie still
  there:
  - took the record as an orphan (its process had ended);
  - detached the disk in 0.4 s;
  - counted 1 cleaned and 0 remaining, and logged the warning for a disk
    open for writing when its process ended.

  In the guest, the requests in flight then failed with "device offline",
  the zombie went away and the dirty memory dropped. No power cycle was
  needed.
- **Data.** Up to sector 712704 (348 MiB) as written, then zeros.

## OpenVDDK, unbuffered (hw6c, hw6d)

With `VIXDISKLIB_FLAG_OPEN_UNBUFFERED` OpenVDDK opens the device with
`O_DIRECT`.

**Cancel (hw6c).**
- The answer Cancel powered the worker off within 1 s.
- The disk stayed attached to the worker. The journal record stayed `ACTIVE`
  with the old boot ID.
- After the worker was powered on, Cleanup recovered it: 1 cleaned, 0
  remaining, with the warning.

**Retry (hw6d).**
- After Retry the worker ran again. The writer waited in state D in
  `submit_bio_wait`, and stayed in state D after SIGKILL.
- **Cleanup** left the record alone, because a live process held it: 0
  cleaned and 0 remaining. `openvddk-admin journal` listed it as "owner
  alive", with nothing about the owner being stuck.
- **Manual detach.** Detaching the disk from the worker through vCenter by
  hand took 0.6 s. The stuck request failed, the process ended and the
  record became an orphan. Cleanup then cleaned it (1 cleaned, 0
  remaining), with the warning.
- **Data.** Up to sector 712704 as written, then zeros.
- **Device names.** After the worker's restart in hw6c, the kernel named its
  own disk `sdb` and the attached target `sda`. OpenVDDK opened the target
  by its WWID, as the journal recorded.

## What this means

- **No error code over HotAdd.** When the target's datastore fills, ESXi
  stops the worker VM, not the write call. Neither library sees an error
  and neither can report one. The question waits for an answer:
  - Cancel powers the worker off;
  - Retry without free space leaves the writes queued without end.
- **For D-A003.** A library cannot turn a full datastore into an error over
  HotAdd. The ways to limit the harm are outside the write call:
  - the operator keeps the datastore's free space above what the thin
    target can still grow;
  - the library compares the two before a write open and refuses or warns;
  - the program writes thin targets on datastores with little free space
    over NBDSSL, which reports 8 (NET-A002).

  VMware's library does none of these.
- **Recovery.** Cleanup handles a zombie writer and a worker powered off by
  Cancel. A writer stuck in `O_DIRECT` I/O keeps its record live: Cleanup
  counts it neither as cleaned nor as remaining, and only a detach through
  vCenter, or a power-off, frees it. The admin tool's journal listing does
  not show that the owner is stuck.
- **Buffered writes.** As in the reference, OpenVDDK writes through the page
  cache unless the program opens the disk unbuffered. The data in the page
  cache at the time of a power-off is lost; what reached the disk is exact.

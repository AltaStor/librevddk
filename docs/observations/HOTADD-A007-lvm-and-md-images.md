# HOTADD-A007 — Images with LVM and MD signatures written over HotAdd (HW7)

- **Why:** HW7 of the HotAdd write qualification: a disk written with an
  image that the worker's LVM or MD could claim, then closed and attached
  again.
- **Libraries:**
  - VMware VDDK 9.1 (`c955e08f…`);
  - OpenVDDK built in the EL8 build container from a
    development version of this change (as HOTADD-A004).
- **Environment:**
  - vCenter 8.0.3 build 24022515, ESXi 8.0.3 build 24022510;
  - the target VM (powered off) with a new 64 MiB thin disk;
  - the worker VM (Debian 13, no LVM of its own) with Debian's
    `lvm2` 2.03.31 and `mdadm` 4.4 installed for this test and purged
    after it.

  Debian's `lvm2` has no devices file, accepts every device and activates
  volume groups by event. `mdadm` brings udev rules that assemble arrays
  incrementally when a member appears.
- **Images,** made on the worker with loop devices, 64 MiB each:
  - **LVM:** a physical volume, a volume group of one 32 MiB logical
    volume with ext4, deactivated;
  - **MD:** one member of a RAID 1 array of two, metadata 1.2, the array
    stopped. Its home host is the worker's host name.
- **Multipath** claims a disk by its WWID, not by its content, so it has no
  image of its own and is not part of this record.
- **Method:** each image written over the whole disk in 1 MiB `Write`
  calls, `Flush`, `Close`; read back through VMware's library over NBDSSL.
  The worker's block devices, `/proc/mdstat` and the journal were read
  after each step.
- **Date:** 2026-11-06. **Evidence:** evidence kept privately (the program's
  output, the library logs, the worker's journal).

## LVM

| Case | What | Result |
|---|---|---|
| hw7a | OpenVDDK, write open, LVM as installed | Open 1: "LVM scans every block device (no devices file, no global_filter)", nothing attached |

Then `global_filter = [ "r|.*|" ]` was set in `/etc/lvm/lvmlocal.conf`:

| Case | What | Result |
|---|---|---|
| hw7b | OpenVDDK writes the LVM image | 0 for every call; nothing claimed; detached; journal empty; read back exactly |
| hw7c | VMware's library writes the LVM image | 0 for every call; nothing claimed; detached |

## MD

| Case | What | Result |
|---|---|---|
| hw7d | OpenVDDK writes the MD image (the disk held the LVM image when attached) | 0 for every call; no array assembled after the write; detached |
| hw7f | OpenVDDK, read-only open of the disk now holding the MD image | at the attach udev's incremental assembly put the disk into an inactive `md127`; Open 1: "sda is claimed by [md127] on the proxy"; the disk detached, the journal empty. `md127` stayed, inactive, naming the removed device, with `mdadm`'s last-resort timer started |
| hw7e | VMware's library writes the MD image (after `md127` was stopped) | at the attach the disk was again put into an inactive `md127`. The library wrote anyway: 0 for every call, detached, and `md127` stayed |
| hw7g | VMware's library, read-only open of the same disk | the stale `md127` was started degraded ("active (auto-read-only) raid1"); the disk came back as another device; the read matched; the library detached the disk and the array stayed active on the removed device |

After the series `md127` was stopped by hand, both packages purged, and the
worker had only its own disk.

## What this means

- **LVM.** The host-safety check holds: an unrestricted LVM refuses the
  open, and with a filter neither library's write is claimed.
- **MD.**
  - The risk is at attach, not after a write: a disk with an MD member
    for the worker's own host name is assembled the moment it appears.
  - OpenVDDK sees the holder and refuses the open, but the inactive array
    stays on the worker.
  - VMware's library reads and writes the disk while the worker's MD holds
    it, and leaves the array behind, started degraded on a device that is
    gone.
- **The gap.** OpenVDDK's host-safety check covers LVM only. A worker with
  `mdadm`'s automatic assembly turns every source disk with an MD member
  into a refused open and a stale array. Disabling it (`AUTO -all` in
  `mdadm.conf`), or a check like the LVM one, needs a decision.
- **The code.** A refused open of a claimed disk answers 1. The other
  refusals of an attach answer 13; this one may be worth aligning.

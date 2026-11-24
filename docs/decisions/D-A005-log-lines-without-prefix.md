# D-A005 — Log lines without the `OpenVDDK: VixDiskLib: ` prefix

- Date: 2026-11-25
- Contract: §86
- Status: accepted (2026-11-25, contract 26.11.25.1). Amends D-0021 point 1 and
  D-0007 (the log prefix). The function names, the reference's wording at
  Open and Close, the per-disk statistics, the connection summary and the
  `WARNING: ` and `PANIC: ` marks (D-0022) stay as they were.

## Context

D-0021 started every message with `OpenVDDK: VixDiskLib: `: the
implementation's name, then the reference's component. Since contract 26.11.25
the library names itself LibreVDDK in its first log line and in its client
identification, but every line still started with OpenVDDK's name, the
project it was forked from. The prefix added 22 characters to each line,
and the consumer usually puts its own tag in front of it (for example
`vddk[LOG]: `).

## Decision

1. A message that belongs to an API call starts with the reference's name
   for that call and a colon (`VixDiskLib_OpenEx: Open disk '…'`), then our
   text. Which call a message belongs to is unchanged (D-0021 point 1).
2. A message that belongs to no call (start-up, configuration, the per-disk
   statistics and the connection summary) starts with its text
   (`Disk IO Count: 7`).
3. Nothing comes before the function name or the text: neither the
   implementation's name nor the reference's component `VixDiskLib: `.
4. The first log line names the build: `LibreVDDK <version> (<commit>),
   independent implementation; …` (§86).
5. The library's own log file and the file named by `OpenVDDK.trace.file`
   (D-0022) carry the same text after the time and the level:
   `2026-11-25T10:00:00.000Z WARN VixDiskLib_Close: …`.

## Consequences

- A search for a reference message's function name or statistic label
  still finds our line for the same step.
- A line alone no longer shows which library wrote it; the first log line
  does, and the reference's lines carry `VixDiskLib: ` before the function
  name where ours do not.
- Tools or tests that matched `OpenVDDK: VixDiskLib: ` must match the
  function name or the text instead. `openvddk-admin bundle` recognises
  the library by its client identification and needs no change.
- Logs of OpenVDDK 0.7.5 and earlier keep the prefix; a search that has to
  cover both should match the text after it.

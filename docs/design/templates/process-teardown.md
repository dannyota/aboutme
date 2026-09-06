# Browser process teardown

Every render result waits for browser teardown before releasing capacity. The
20-second budget starts at admission and controls cancellation. Joined cleanup
may finish later; an expired timer does not prove external work has stopped.

## Process ownership

The server launches a dedicated `render-browser-supervisor` executable installed
beside it. Native startup and the server image build both executables together.
The renderer captures that private absolute path at construction. Tests inject
an immutable path to their compiled supervisor.

The supervisor is the leader of a private process group. Chromium inherits that
group. The live supervisor anchors the group ID until teardown completes, so a
reused group ID cannot redirect cleanup. Signal handling is confined to this
supervisor; the server adds no process-wide subreaper or signal disposition.

Chromium still receives only the fixed environment through `/usr/bin/env -i`:
`TZ=UTC`, `LANG=C.UTF-8`, and `LC_ALL=C.UTF-8`. Browser arguments, sandbox
flags, stdio and the existing renderer network boundary remain unchanged.

## Stop and join

Cancellation and natural browser leader exit both stop and join remaining group
members. The supervisor reads Linux process metadata, selects its own group and
opens a process file descriptor (pidfd) for each candidate. It rereads the exact
PID, group and start identity before signaling that descriptor. It never sends a
signal to a candidate by numeric PID alone.

The supervisor polls each pidfd without `PIDFD_THREAD`. Completion requires all
threads of that process to have exited. A process-state character alone does not
prove this. The kernel documents descriptor identity in
[pidfd_send_signal](https://man7.org/linux/man-pages/man2/pidfd_send_signal.2.html)
and terminal polling in
[pidfd_open](https://man7.org/linux/man-pages/man2/pidfd_open.2.html).

Verified disappearance is absence. Unreadable or malformed metadata, an
inconsistent identity, unsupported descriptor operations or failed polling is
unresolved cleanup. A bounded polling cadence avoids a busy loop; no timeout
converts uncertainty into success. Unsupported facilities fail the preflight
before Chromium starts.

## Completion proof

The supervisor has a private completion pipe to the server. Chromium and its
descendants never inherit its writer. The protocol distinguishes startup from
completed teardown, and permits completion only after no browser was started or
every owned group member is proved terminal.

The renderer waits for both process exit and valid completion proof on every
result path, including version checks, readiness, startup failure and rendering.
A locally failed supervisor start proves no child was created. Once the
supervisor starts, EOF, malformed proof, panic, OOM kill or unexpected exit does
not prove cleanup. Render ownership remains held when proof is missing. Fleet
claims can then be reclaimed only through the existing exact EC2 termination
rule; the renderer cannot turn a helper crash into graceful completion.

## Required proof

- Cancellation and natural leader exit join a live descendant before returning.
- PID reuse cannot redirect a signal to a replacement process.
- Read, parse, descriptor and polling faults cannot report joined cleanup.
- Cancellation before browser launch and definite start failure remain safe.
- Unexpected supervisor death and absent completion proof retain ownership.
- Browser environment, argument and private-pipe isolation remain exact.
- Real pinned Chromium retains its sandbox and passes rendering checks within
  the existing whole-task memory budget.

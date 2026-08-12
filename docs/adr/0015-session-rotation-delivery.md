# 0015 — Session rotation waits for successor delivery

Status: Proposed (2026-08-12)

## Context

Replacing a session cookie on the first request after the rotation age has two
failure points: concurrent requests can race to rotate, and the response that
contains the successor cookie can be lost. Revoking the predecessor at insert
time can strand the user when delivery fails. Leaving it valid without a bound
keeps a superseded credential alive too long.

## Decision

A conditional update admits at most one rotation winner for a predecessor. A
unique lineage key permits at most one successor row. The successor inherits the
predecessor's user, absolute expiry, recent-reauth time, user agent, and IP;
rotation does not extend any of them.

The winner initially parks the predecessor's deadline at
`min(now + 24 hours, absolute expiry)`. The successor's first authenticated use
proves that its cookie reached a client and moves that deadline inward to
`now + 60 seconds`. Concurrent losers continue with the predecessor while it is
live and never mint another successor.

The admission update and successor insert are separate statements. If the insert
fails, the predecessor remains usable only to its parked deadline. If the
response is lost after insert, the unreachable successor expires normally and
the predecessor remains usable to its parked deadline. These outcomes are
bounded and may require a new login; they never create a second successor.

Revoking either member of a rotation pair also revokes its live lineage partner
so a device action cannot leave the paired credential active.

## Consequences

- A stolen predecessor can remain usable until the parked deadline when
  successor delivery is never proved. Monitoring must expose this state.
- Rotation convergence depends on first use, not on assuming that `Set-Cookie`
  was delivered.
- Tests must cover concurrent winners, lost insert, lost response, first-use
  grace, absolute expiry, and lineage revocation.

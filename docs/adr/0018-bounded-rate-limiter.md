# 0018 — Rate-limit state never evicts an active bucket

Status: Proposed (2026-08-12)

## Context

An in-memory limiter keyed by attacker-controlled values can grow without bound.
A size cap that evicts arbitrary active entries is also unsafe because an
attacker can churn keys until its own bucket is reset.

## Decision

Rate-limit storage has an explicit capacity and TTL. Cleanup may evict expired
buckets only. When capacity is full and no expired bucket exists, a bounded
global overflow bucket or admission refusal applies; an active per-key bucket is
never evicted to make room.

Policies choose an IP, account, or account-and-IP key. Caddy derives the one
canonical client address. Go accepts it only from configured trusted-proxy CIDRs
and never derives identity from viewer-supplied forwarding headers.

## Consequences

- Key churn degrades toward a shared restrictive policy, not toward unlimited
  requests.
- Tests need a fake clock and deterministic coverage of expiry, capacity,
  concurrent admission, and overflow behavior.
- A multi-node application tier would require a new distributed rate-limit
  decision; v1 is explicitly one node.

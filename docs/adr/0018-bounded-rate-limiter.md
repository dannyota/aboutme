# 0018 — Rate-limit state uses bounded overflow and expiry

Status: Accepted (2026-08-12)

## Context

An in-memory limiter keyed by attacker-controlled values can grow without bound.
A size cap that evicts arbitrary active entries is also unsafe because an
attacker can churn keys until its own bucket is reset.

## Decision

Each limiter instance stores at most 10,000 per-key buckets. A bucket expires
when it has fully refilled and therefore carries no state that differs from a
new bucket. It also expires after 24 hours with no request for that key. The
idle cap is a backstop for stale state; a request, including a rejected request,
ends the idle period. Twenty-four hours exceeds the longest v1 rate window of
one hour, so the backstop cannot erase legitimate idle debt. Cleanup uses an
injected monotonic clock and may remove only expired buckets.

When capacity is full and no expired bucket exists, every untracked key shares
one global overflow bucket for that limiter instance. The overflow bucket has
the same capacity and refill rate as one ordinary key. Admission refusal is not
an alternative, and an active per-key bucket is never evicted to make room.

Policies choose an IP, account, or account-and-IP key. Caddy derives the one
canonical client address. Go accepts it only from configured trusted-proxy CIDRs
and never derives identity from viewer-supplied forwarding headers. The v1
budgets are:

- All API traffic: 300 requests per minute per client IP.
- Anonymous login starts: 30 requests per minute per client IP.
- Authenticated provider-link and reauthentication starts: 30 requests per
  minute per `(account, client IP)` pair.
- Resume reads: 600 requests per minute per `(account, client IP)` pair.
- Resume writes: 240 requests per minute per `(account, client IP)` pair.
- Photo uploads: 20 requests per hour per `(account, client IP)` pair.

The route-specific limiter is additional to the outer client-IP limiter. The
numeric budget source remains [`../design/budgets.md`](../design/budgets.md).

## Consequences

- Key churn degrades toward a shared restrictive policy, not toward unlimited
  requests.
- Anonymous starts are contained before an account exists. An authenticated
  account cannot consume every other account's privileged-start allowance behind
  the same client IP.
- Tests need a fake clock and deterministic coverage of expiry, capacity, the
  24-hour idle cap, concurrent admission, and overflow behavior.
- A multi-node application tier would require a new distributed rate-limit
  decision; v1 is explicitly one node.

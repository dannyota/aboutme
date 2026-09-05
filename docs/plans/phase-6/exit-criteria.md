# Phase 6 exit criteria

Status: pending. Check each item against one unchanged candidate commit.

1. AC-RT-001: authenticated owner and anonymous public streams send metadata
   only. Every connection and reconnect repairs missed notifications by an
   unconditional read. Three transport failures or buffering activate
   conditional polling. Unknown versions reload.
2. AC-RT-002: unpublish, rename, and delete close admitted public streams before
   the revoking mutation returns. The next completed live read is 404. Editor
   pending changes and viewer scroll survive normal revision refreshes.
3. Bounds: total/IP/account caps, bounded event queues, slow-client eviction, FD
   headroom admission, and key reclamation hold under connection churn. Record a
   local 2,000-connection transport measurement. Do not claim AWS memory or edge
   latency proof from the laptop.
4. PostgreSQL integration proves committed notification delivery, no rollback
   delivery, listener recovery, and no per-stream database connection. Shutdown
   closes streams and joins the listener within the server's drain deadline.
5. OpenAPI, generated types, code, design, architecture, and traceability agree.
   Relevant unit, database, migration, and native HTTPS browser checks pass.
6. One fresh Sol reviewer checks the integrated phase for defects, auth and
   session isolation, public revocation, CAS/idempotency preservation, stream
   concurrency, bounds, interface stability, and acceptance ownership.
7. Integration owner runs `make ci` and connected `make scan` alone at the
   candidate commit, resolves findings, and pushes only after all required
   checks pass. No deployment is part of this phase.

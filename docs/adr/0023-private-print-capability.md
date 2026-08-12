# 0023 — Internal print uses a one-use render capability

Status: Proposed (2026-08-12)

## Context

Caddy denies external `/print/**` requests, but network placement alone does not
authorize a document. An internal route that accepts only a resume ID can expose
another account's draft after a routing mistake, server-side request forgery, or
direct access to Nuxt. Forwarding the browser's session cookie would also give a
render browser ambient account authority unrelated to one job.

## Decision

Go authorizes the requested owner or public render and freezes the exact render
snapshot before Chromium starts. It issues a cryptographically random 256-bit
opaque capability with a maximum 60-second lifetime. Only a hash is retained.
Capability and controller state exist only in the bounded in-memory render
queue. A job reserves one admitted-job slot before either record is created.
Admitted-job capacity is the configured concurrent-render limit plus queue-depth
limit from the [numeric budgets](../plans/budgets.md); each active job has at
most one unused capability record and one controller/job record. Active jobs and
unused capabilities therefore cannot exceed that shared admission bound.
Replacing an unused capability removes the old record atomically before
installing the new one.

The capability record is bound to:

- the resume ID and render-job caller ID;
- the authorized document revision, schema version, public generation (the
  committed resume revision) when applicable, and snapshot digest;
- the `nuxt-print` audience; and
- the exact expiry and unused state.

For the initial document navigation, Chromium sends
`Authorization: RenderCapability <token>` and the render-job caller ID in a
`X-Render-Job-ID: <uuid>` header to the internal Nuxt print route. The
capability never appears in a URL, cookie, log, rendered document, or
subresource request. Nuxt redeems it over a loopback or deployment-private
internal Go interface. Go checks the route resume ID and every stored binding,
atomically changes the record from unused to consumed, and returns the
already-authorized document plus inline normalized-photo context. A failed or
timed-out render is rescheduled as a new queue attempt with a new job ID,
controller handle, and capability; a token or job ID is never reset or reused.

An unused capability is removed when its 60-second expiry is reached, and that
attempt fails. Successful redemption removes the unused-capability record while
the same bounded queue slot retains the consumed job state. Expiry cleanup uses
the queue's injected clock and cannot wait for a later request to make progress.

Consumption removes the Nuxt bearer authority but does not erase the render job.
At job creation, the Go render queue also receives an opaque controller handle
containing separate 256-bit random authority; only its hash is retained with the
job. The handle never leaves the Go process and is never sent to Nuxt, Chromium,
a URL, or an HTTP header. Knowing the render-job ID is insufficient.

The consumed job record is keyed by the render-job ID and bound to the same
snapshot digest and public generation. The controlling Go render job, not Nuxt
or Chromium, receives completed PDF or image bytes from the browser controller
and invokes an unexported in-process completion method with its controller
handle. Completion is not an HTTP or RPC route. The method accepts only the
matching consumed job and controller-handle hash, recomputes the artifact digest
from the supplied bytes, records that digest in the one-shot result, and
atomically chooses acceptance or discard. The controller does not supply an
expected artifact digest; the snapshot digest authenticates the frozen render
input, not a value that could be known before rendering.

On acceptance or discard, the completion operation atomically removes the job
record and controller-handle hash and releases the queue slot before delivering
the result once to the live controlling call. The bounded render timeout
performs the same removal and release. There is no terminal tombstone, durable
capability table, or separate unbounded completion map. Concurrent matching
completions have one atomic winner; every duplicate or completion after cleanup
gets the same generic not-active failure and cannot learn whether the prior
outcome was acceptance, discard, timeout, or process loss. Bad controller
authority or completion before redemption fails without consuming the live job.
A completion with valid controller authority but a stale public generation is a
terminal discard and cleanup. A process loss erases all in-memory records and
fails their attempts; restart does not reconstruct remotely completable state.

The print route rejects a resume ID without a valid capability and never falls
back to an ambient browser session. The render browser has no account cookie and
no general outbound network access. Caddy continues to deny viewer access to
`/print/**`, but that denial is defense in depth rather than the authorization
decision. Direct Nuxt access and a leaked resume ID remain insufficient.

A public artifact is published only after the completion operation verifies that
its public generation still matches current state. Go is the only publisher.
Nuxt and Chromium cannot write an artifact or turn a consumed job into a public
URL. Owner export remains bound to the owner-authorized snapshot even if a later
edit commits while the job runs.

## Consequences

- Every print has one narrow authority and deterministic input.
- The internal capability store and consume operation need atomic, fake-clock,
  expiry, wrong-audience, wrong-caller, wrong-resume, and replay tests.
- Supplying photo bytes inline avoids granting Chromium a second media
  capability or a reusable object URL.
- A render that does not start and redeem within 60 seconds fails closed and
  must be rescheduled.
- Job-state tests cover completion before redemption, duplicate completion,
  missing or wrong controller handle, job-ID-only attempts, recomputed result-
  digest delivery, changed public generation, process loss, timeout, acceptance,
  and discard.
- Fake-clock and saturation tests prove unused records disappear at the
  60-second boundary, every consumed-state terminal path releases its queue
  slot, active jobs and unused capabilities never exceed queue capacity, and
  repeated completed job IDs do not grow any map or table.

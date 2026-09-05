# Task 7.1: Owner PDF and private print jobs

Status: Implemented; integration and phase exit checks are in progress.

Create a bounded Go render queue and capability-gated Nuxt print route. The
owner PDF uses the authorized document revision even if a later edit commits. No
account cookie, media key, or controller authority enters Chromium.

Authorities are the [phase authorities](README.md#authorities). Acceptance:
`AC-PDF-001` through `AC-PDF-004`, and the print part of `AC-SEC-001`.

## Shared limits

- One running render, eight waiting jobs, no unbounded waiting admission.
- A reserved slot covers snapshot preparation, queueing, render, and completion.
- The configured cancellation deadline is 20 seconds from admission, including
  queue time. Cancellation must terminate and join the browser before another
  render starts.
- An unused capability expires within 60 seconds. Cleanup uses an injected clock
  and timer; it progresses without a later request. The shorter job deadline
  usually removes the record first.
- Capabilities and controller handles each use 32 independent random bytes.
  Store SHA-256 hashes only. Job IDs are injected UUIDs. Never reuse an attempt.
- Snapshot payload ceiling: 3,407,872 bytes. This covers a 512 KiB document, a 2
  MiB normalized photo after base64 expansion, and bounded metadata.
- Initial PDF output ceiling: 16,777,216 bytes. This is a provisional local
  safety bound, not evidence that the maximum corpus fits. The phase must
  measure it and update the numeric budgets before exit.
- Errors, logs, metrics, and panic text contain no document, photo, capability,
  controller handle, or raw downstream response.

## Slice 7.1.1: Queue and capability state

Owner: one Sol implementation worker. Exclusive files:
`apps/server/internal/renderjob/*.go`. No other files, Git operations, new
dependencies, browser processes, or database work.

The queue has no HTTP server, database access, browser implementation, or
document layout. Callers supply snapshot preparation and generation validation.
Its renderer dependency performs one controlled navigation and returns bytes.

Use the signatures below so later slices can depend on them. Keep
implementation-only state and completion unexported.

```go
type Format string
const PDF Format = "pdf"
const PNG Format = "png"

type Snapshot struct {
    ResumeID uuid.UUID
    Revision int64
    SchemaVersion int
    PublicGeneration int64
    Payload []byte
}

type Request struct {
    Format Format
    Prepare func(context.Context) (Snapshot, error)
    ValidateGeneration func(context.Context, Snapshot) error
}

type Navigation struct {
    ResumeID uuid.UUID
    JobID uuid.UUID
    Capability string
    Format Format
}

type Renderer interface {
    Render(context.Context, Navigation) ([]byte, error)
}

type Redemption struct {
    ResumeID uuid.UUID
    JobID uuid.UUID
    Audience string
    Capability string
}

type Result struct {
    Bytes []byte
    Digest [32]byte
    Revision int64
}

func New(Config) (*Queue, error)
func (*Queue) Render(context.Context, Request) (Result, error)
func (*Queue) Redeem(context.Context, Redemption) (Snapshot, error)
func (*Queue) Ready() error
func (*Queue) Close() error
```

`PublicGeneration` is zero for an owner snapshot. `Config` injects the renderer,
entropy reader, UUID source, clock, and timers. Production defaults use secure
entropy and real time. Lower bounds may be configured for tests; configuration
cannot raise the production limits.

1. Reserve a slot before calling `Prepare` or creating authority. Full admission
   returns `ErrSaturated` and makes `Ready` fail. Preparation failures release
   the slot. Snapshot fields must be valid, payload nonempty and within its cap.
2. Copy the payload, compute its digest, and bind it with every snapshot field,
   job ID, audience, expiry, capability hash, and separate controller hash.
3. Allow one renderer call at a time. Its only bearer authority is `Navigation`.
   Do not forward the snapshot or controller handle to the browser dependency.
4. `Redeem` validates exact token shape, resume, job, audience, expiry, unused
   state, and stored snapshot digest before atomically consuming the capability.
   It returns a detached payload copy. All rejected authority returns one
   generic `ErrNotActive`, and never consumes valid state.
5. An unexported completion method accepts the controller handle and bytes. It
   rejects completion before redemption, wrong handles, and unknown jobs without
   consuming live state. Recompute the output digest; never trust a supplied
   one.
6. Public completion calls `ValidateGeneration` with the frozen binding. A
   public snapshot without a validator is invalid. A stale generation is
   terminal discard. Owner completion needs no public validator.
7. Acceptance, discard, output overflow, cancellation, timeout, entropy failure,
   and shutdown clear every authority record and release capacity. Deliver one
   result after cleanup. Duplicate completion returns `ErrNotActive`; keep no
   tombstones or terminal map.
8. Deadline and capability-expiry timers advance without another request. Join
   canceled renderer work before admitting its successor. `Close` cancels and
   joins all live calls and makes readiness fail permanently.

### Test cycle

- [ ] Write a failing test using a controlled renderer that redeems its own
      navigation through the real queue and waits on a barrier.
- [ ] Observe failure for missing queue behavior before implementation.
- [ ] Implement the smallest correct state machine.
- [ ] Cover nine admitted jobs and rejected tenth, one renderer active, capacity
      recovery, preparation cancellation, deadline while queued, and joined
      shutdown.
- [ ] Cover wrong token shape, audience, job, resume, expiry boundary, replay,
      payload mutation, concurrent redemption, and entropy/UUID failure.
- [ ] Exercise private completion from same-package tests: before redemption,
      wrong/missing handle, job-ID-only, duplicate race, digest recompute,
      public generation change, success, discard, timeout, and process-state
      loss.
- [ ] Prove map size remains bounded over repeated terminal jobs. Advance fake
      time without another API call to prove expiry and deadline cleanup.
- [ ] Run the exact narrow check from repository root:

```sh
flock --close .dev/phase-7/heavy.lock sh -c \
  'cd apps/server && go test -race -count=1 ./internal/renderjob'
```

Report owned paths, the observed failing check, passing checks, concrete
resource counts, and any unresolved boundary. Never run `make ci` or commit.

## Remaining slices

The integration owner must freeze the browser, print-envelope, owner-route, and
runtime contracts before dispatching those slices. Existing authorities approve
their behavior; no additional product approval is needed.

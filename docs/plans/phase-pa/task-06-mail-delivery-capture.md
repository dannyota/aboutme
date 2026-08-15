# Task 06 — Implement bounded mail delivery and local capture

**Acceptance:** AC-AUTH-014, AC-OPS-020, AC-SEC-005.

**Depends on:** T01 claim/lease queries; T04 encrypted payload contract.

**Owned paths:** T06 paths in `file-structure.md`. SES dependency files are a
short serialized integration-owner subwindow.

## Contract

```go
type Message struct {
  Kind Kind
  To string
  Subject string
  TextBody string
  HTMLBody string
}
type SendOutcome uint8
const (
  SendAccepted SendOutcome = iota
  SendTemporaryFailure
  SendPermanentFailure
)
type SendResult struct { Outcome SendOutcome }
type Sender interface { Send(context.Context, Message) (SendResult, error) }

type WorkerOptions struct {
  Pool *store.Pool
  Queries *store.Queries
  KeyRing *KeyRing
  Sender Sender
  Clock func() time.Time
  Jitter func(time.Duration) time.Duration
  Logger *slog.Logger
  WorkerID uuid.UUID
}
func NewWorker(WorkerOptions) (*Worker, error)
func (w *Worker) Run(context.Context) error
func (w *Worker) RunOnce(context.Context) error
```

`Pool.Begin(ctx)` starts each authorization/finalization transaction and
`Queries.WithTx(tx)` supplies the exact T01 generated query surface. The worker
never sends with pool-backed queries. Production has no alternate store path;
tests inject only package-private begin/commit failure hooks and use the same
`*store.Queries` transaction binding.

Message templates are fixed code, canonical-origin links only, plain text plus
minimal escaped HTML, no tracking pixel, external resource, reply token, or
user-controlled subject. SES v2 owns one `SendEmail` attempt and disables SDK
retries. Temporary outcomes are timeout/transport ambiguity, 429, throttling,
and server 5xx. Permanent outcomes are validation, rejected content,
unverified/suspended sender, and other closed 4xx. Returned/logged errors are
generic and omit AWS request IDs and messages.

`mailcapture.Server` implements D7 and the same `Sender` through authenticated
loopback HTTP. Its viewer is for local humans; tests/UAT consume closed JSON.

## TDD cycle

- [ ] Write worker REDs for claim order/batch, two-send cap, 30s lease, stale
      requeue, current token authority, stale cancellation, 10s timeout,
      success, temporary/permanent/ambiguous outcomes, full-jitter cap, attempt
      8, expiry, cleanup 200, shutdown cancellation, and join.
- [ ] Inject a sender that pauses before/after send and store barriers that
      prove two workers never send one live lease and a stale worker cannot
      finalize a lease reassigned to another worker.
- [ ] Add the exact replacement/send race: pause after scope+job authorization
      locks and before `Sender.Send`; replacement must block, sender completes
      first, then replacement commits. Reverse order must terminalize without a
      sender call. Assert scope → job lock order in both paths.
- [ ] Write SES adapter REDs using a fake SES v2 client. Assert exact region/
      From/configuration set, one SDK call, no SDK retry, fixed message bytes,
      closed error classification, and no recipient/body/error logging.
- [ ] Write capture REDs for loopback address, missing/wrong/repeated bearer,
      methods, strict JSON, 16 KiB message cap, 50/256 KiB eviction, HTML
      escaping, reset, concurrent reads/writes, and secret-free logs.
- [ ] Run expected RED:

  ```sh
  cd apps/server && go test ./internal/authmail ./internal/mailcapture \
    ./cmd/mail-capture -race -count=1 \
    -run 'Test(Worker|SES|MailCapture)'
  ```

- [ ] Owner pins SES exactly:

  ```sh
  cd apps/server && go get github.com/aws/aws-sdk-go-v2/service/sesv2@v1.66.6
  ```

  Inspect module/sum drift and do not initialize an AWS client in tests or
  capture mode.

- [ ] Implement worker/sender/capture with injected clients, clock, jitter, and
      signals. A worker error never includes decrypted data or raw SDK error.
- [ ] Run the minimal GREEN focused tests and:

  ```sh
  make server-build server-vet
  ```

## Adversarial checklist

- Swapped job scope/token/ciphertext/key ID never sends.
- Replacement after claim but before send becomes terminal without send.
- Cancellation during decrypt/authority/send/finalize releases/join bounds and
  leaves a recoverable lease or exact terminal state.
- Capture never binds wildcard/non-loopback, follows no redirect, renders no
  active markup, and exposes no production route.
- Sentinel scan covers email, raw token/digest, plaintext, keys, ciphertext, AWS
  request ID/raw error, and capture bearer across logs/errors/panics.

## Handoff

Report sender/worker/capture interfaces, SES dependency diff, exact outcome
classification, concurrency/retry results, fixed ports/API, and unrun AWS work.
Suggested commit: `feat(auth): deliver bounded authentication email`.

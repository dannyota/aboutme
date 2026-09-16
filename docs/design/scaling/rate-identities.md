# Fleet rate identities and key versions

Status: Accepted detail of [fleet admission](admission.md) under
[ADR 0035](../../adr/0035-replica-coordination-and-uat-lifecycle.md). The
[scaling index](README.md) records whether it is built.

Use the existing canonical caller identities and one private deployment tuple
for every keyed fleet policy. Keys are resolved only inside the process that
needs them. [Shared claims](claim-identities.md) uses the same admission key
with its own domain.

## Rate digest

Go alone canonicalizes source identity through existing caller authorities. One
internal rate-key encoder accepts a closed typed union, never a string key:

- IP: netip.Addr, valid and Unmap applied once; payload is family byte 4 plus
  four network-order bytes, or family byte 6 plus sixteen bytes.
- Peer IP: parsed only from the raw socket RemoteAddr by the existing peerAddr
  authority, then Unmap once; payload bytes match IP but semantic component type
  and accepted policy shape are distinct. It never uses the canonical header.
- UUID component: non-nil uuid.UUID, payload is its 16 bytes. Its semantic type
  is account, OAuth client, durable token, or durable user.
- Email component: the existing 32-byte HMAC-SHA-256 canonical-email digest from
  password_rate.go. Raw or canonical email is never accepted.

The encoder computes HMAC-SHA-256 with the existing admission key over:

1. ASCII aboutme.rate-key.v1 then NUL;
2. uint16 big-endian policy-ID byte length, then exact UTF-8 policy ID;
3. one-byte component count;
4. for each component: one-byte type (1 IP, 2 peer IP, 3 account UUID, 4 email
   digest, 5 OAuth client UUID, 6 token UUID, 7 user UUID), uint16 big-endian
   payload length, then payload.

Component order is fixed by policy. account_ip is account then IP. P05 anonymous
uses IP only; authenticated P05 uses account then IP under the same policy.
Middleware canonical-IP failure uses peer IP alone, spends that distinct bucket,
and still returns the current invalid_client_ip response unless exhausted, when
it returns the existing 429. Equal viewer/peer address bytes never collide. The
policy ID separates every budget even when identity bytes match. SQL accepts
only policy_id plus the final 32-byte digest and never sees identity components.
Go has one fixed vector for each of the seven key shapes and
cross-policy/domain/ secret separation vectors. P22 keeps its accepted unkeyed
SHA-256 encoding and does not use this encoder.

## Deployment key versions

R8 defines one private approved deployment tuple containing the nonsecret pinned
version of the admission HMAC key and the nonsecret pinned version of the
password-email HMAC key. It resolves each key inside the Go process and verifies
that the loaded key version equals that tuple before constructing any claim or
rate adapter. The keys remain available only inside that process and never enter
SQL, readiness output, logs, or errors. The tuple is immutable for the process
lifetime and bound by private composition to its exact task, replica
incarnation, and release identity.

This uses the existing trust boundary: reviewed private deployment input, R8
composition, exact ECS/task/release identity, and the serialized lifecycle
controller. It adds no replica table, SQL column, registration argument,
activation argument, lifecycle action, or ledger field. Before an existing
activation call, the controller obtains the target's private readiness evidence
for that exact incarnation and requires its process-verified tuple to equal the
single approved deployment tuple. It repeats this check for every target it
activates while that controller execution owns the environment. Because the
process tuple is immutable, the subsequent existing activation CAS for the same
exact identity cannot activate a different tuple. Missing, stale, mixed, or
unverifiable evidence forbids the activation and keeps readiness closed. A
public caller cannot select a generation, and an operation never probes an old
digest after a miss.

## Rotation precondition

Changing either key requires this closed runbook predicate because either change
moves P07/P09 email identities, and changing the admission key moves every keyed
rate identity as well as shared-claim scope identities:

1. The existing serialized controller records the approved old and proposed new
   tuples in its private execution input/evidence and enters the closed rotation
   procedure. It closes public and application admission on every replica and
   forbids existing activation calls. Login, render, SSE, mail-send, media, and
   other new work admission stop. PostgreSQL and the private
   lifecycle/maintenance path stay available; this is not final UAT write-gate
   closure or an RDS stop.
2. Join every admitted operation. Use the accepted exact leave or EC2 fencing
   proof where a replica cannot acknowledge. Prove shared-claim running and
   waiting counts are zero. A deadline or missing heartbeat never reclaims work.
3. With no application admission, run the existing bounded rate and P22 cleanup
   operations repeatedly at accepted database effective time. Let each
   algorithm's existing window/refill rules mature; do not advance caller time
   or add a rotation horizon. Resolve P22 expiry only through its
   selected-bucket locked path. Retained terminal receipts may remain because
   their unkeyed attempt UUID identity does not authorize work or carry rate
   debt.
4. Under the policy clocks and normal bucket locks, prove every ordinary rate
   bucket has been legitimately deleted; each token overflow is full, each
   fixed-window overflow has zero count, no effective pending debt and no active
   window, and each rolling overflow has no event; and no P22 pending attempt
   remains. The proof reads only stored digests and state. It does not need
   either HMAC key. Any row/debt ambiguity aborts rotation and keeps admission
   closed.
5. Update the one private approved deployment tuple and replace the selected key
   version through the existing deployment procedure. Restart/recompose every
   replica against that tuple. Do not retain an old key, dual-write, scan both
   generations, or fall back after a miss.
6. Re-run the existing registration and shared-admission rollback proofs. Before
   each existing activation, the controller rechecks exact-target readiness
   evidence against the one new tuple. Reopen application admission only after
   all activated targets match. A failed or partial rollout stays closed and
   uses the normal join/fence path; it never restores service by ignoring old
   rate debt.

The controller's existing single-owner execution history and exact target
evidence record the closed-state, zero-claim/zero-debt observations, selected
deployment tuple, and activation checks. These are predicates and evidence
within the existing close, maintenance, deployment, and activation operations;
they are not new SQL actions or lifecycle-ledger results. Retry rereads the
owned controller execution, database state, target identity, and target evidence
before continuing. No public API, SQL digest-generation argument,
dual-generation row, new retention horizon, lifecycle signature, or cloud
service is added by this contract.

## Required proof

- Pin an independent vector for each keyed shape and the P22 SHA-256 vector.
  Test IPv4-mapped equivalence, distinct IPv6 addresses, nil UUID/malformed IP
  rejection and P05 anonymous/authenticated separation.
- Equal peer/viewer IP bytes remain distinct. Only the existing failed-key
  middleware path may choose peer identity; preserve its 400/429 response.
- Policy, component, domain and key changes produce distinct digests. SQL, logs
  and errors contain no raw identity or key.
- R8 rejects missing, mixed, stale or unverifiable key versions before adapter
  construction/readiness. Controller evidence binds the exact immutable
  process/task/replica/release before each existing activation.
- Rotation cannot switch while any ordinary bucket, overflow debt, pending
  attempt or live claim remains. Each policy must return `policy_idle=true`
  through [bounded cleanup](rate-storage.md) while admission stays closed.
- Rollout failure, replay and controller ownership loss keep admission closed.
  No old-key fallback, artificial clock advance or age-based claim release.

The [deployment design](../deployment.md) carries this requirement. R8 writes
and verifies the executable rotation runbook after the infrastructure and
runtime operations exist. These checks have not run for the new runtime.

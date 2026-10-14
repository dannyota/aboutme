# Account delete

The host account-deletion procedure first ships in **v0.7.9**. Publish the
reviewed image, install its reviewed host bundle, and deploy that release before
using it. The [host guide](../../deploy/vn/README.md#host-account-deletion) owns
installation. The [operations design](../design/vietnam-host-operations.md) owns
execution and recovery; [ADR 0029](../adr/0029-community-showcase.md) owns the
deletion decision. There is no operator screen or HTTP route.

## When to use it

Use it after a showcase Report or other abuse establishes a serious breach of
the Terms or a legal duty to act. Deletion is permanent and sends no notice
email. A minor or doubtful report does not justify it. The self-delete path
removes the account, resumes, sessions, grants, and showcase entries together,
enqueues physical media deletion, and reserves its slugs for 180 days.

## Run it

Use a clean, current reviewed `main` checkout. Set `DEPLOY_HOST`; `SSH_PORT`
defaults to 22922. Pin the host key in the operator's SSH known-hosts file from
a trusted source. Unknown or changed host keys refuse access. The wrapper checks
release eligibility, provenance, amd64 digests, and every installed operation
helper and unit before sending a request. Ordinary installation preserves
secrets, flags, and saved requests and refuses unresolved operations.

```sh
export DEPLOY_HOST=<host>
bash deploy/vn/scripts/operations.sh account-delete show reported-slug
```

Show saves a plan valid for 15 minutes. Its closed JSON has exactly
`schemaVersion`, `requestId`, `kind`, `phase`, `outcome`, `release`,
`expiresAt`, `confirmation`, `action`, `challenge`, and `recoveryCopy`. A usable
plan has `phase:"planned"`, `outcome:"pending"`, the account UUID and sorted
slugs in `action`, and exact confirmation text. Exit 4 means not found and
prints no JSON. Exit 2 means invalid input. Exit 1 means refusal or transport
failure. Exit 0 means a read succeeded or a transition was durably accepted; it
does not mean deletion completed.

## Steps

1. Read the report and open `https://aboutme.vn/reported-slug`. Establish the
   breach before deleting anything.
2. Run show. Inspect the UUID and **every** shown slug. The reported slug must
   appear in `action.shownSlugs`.
3. Copy `requestId` and the complete returned confirmation. Confirming deletes
   every current resume of that account, including resumes added after show.

   ```sh
   id='<requestId>'
   confirmation='delete-account:<requestId>:<uuid>:reported-slug'
   bash deploy/vn/scripts/operations.sh apply "$id" --confirm "$confirmation"
   ```

4. Read saved state, then reconcile after the request unit stops:

   ```sh
   bash deploy/vn/scripts/operations.sh status "$id"
   bash deploy/vn/scripts/operations.sh resume "$id"
   ```

   Resume may accept a separate read attempt. Repeat status/resume after it
   stops. A committed write has `action.writeOutcome:"committed"`. A lost
   receipt resolved by UUID absence has `absent_after_unknown`; it does not
   prove which actor deleted the account. A present UUID requires a fresh show
   and confirmation. Never replay an uncertain destructive command.

5. Run the public verification harness from the same reviewed checkout:

   ```sh
   bash scripts/host-operations-proof.sh "$id"
   bash deploy/vn/scripts/operations.sh status "$id"
   ```

   Proof needs pinned SSH access, `curl`, `jq`, trusted HTTPS certificates, and
   public access to aboutme.vn. It needs no account credentials or browser
   storage. It first verifies absence through the server commands, then checks
   all known slugs across HTML, JSON, photo, PDF, markdown, preview alias, live
   reads, discovery, and every showcase page within five minutes. A nonce binds
   the result to this request, live server digest, and serving phase. Failure,
   interrupted pagination, or stale evidence cannot prove revocation.

6. Inspect physical cleanup separately:

   ```sh
   bash deploy/vn/scripts/operations.sh verify "$id"
   bash deploy/vn/scripts/operations.sh resume "$id"
   bash deploy/vn/scripts/operations.sh status "$id"
   ```

   Resume refuses while the unit or backend may still run. Retry that read after
   the unit stops. Only account absence followed by a successful global queue
   count of zero sets `action.physicalCleanup:"complete"`. Delayed, leased, and
   unrelated jobs keep cleanup pending. The existing media sweep runs after
   fence release; a sweep failure alerts and never undoes deletion.

Public proof remains `limited` even when every checked route rejects: show does
not return card versions, and a lost commit receipt may lose the full committed
slug list. Do not invent a version or recover deleted content from backups.
Responses admitted before commit may finish; live streams close on deletion
notification under [ADR 0010](../adr/0010-public-artifact-revocation.md).

## Audit and failures

The root-only audit lives under `/srv/data/host-operations/`, directories 0700
and files 0600. For 180 days it retains action, outcome, UUID, reported, shown,
and returned slugs, and event UTC time. Tie a report to that record by reported
slug and time. Store no report body, email, name, or headline. The daily
retention unit erases expired personal evidence even from unresolved requests;
status does not extend retention. Attempt metadata expires after 30 days once
quiescent. Root can alter the record, and host loss can lose it. Server journald
and CI are not the audit store.

After lost SSH, use status for the saved request ID. If show lost its reply
before returning an ID, inspect root-only request directory names and closed
status from the provider console; do not guess or remove a fence. A fresh show
cannot authorize an old request. Duplicate accepted apply returns saved state.
Stale plans or changed release, flags, secret metadata, owner, or installed
bundle require new inspection and a fresh plan.

```sh
bash deploy/vn/scripts/operations.sh abort "$id"
```

Abort before dispatch closes without deletion. After dispatch it uses the same
quiescence and UUID checks as resume. It cannot cancel a committed deletion.
Unknown results retain the fence until the unit, containers, and database
backends are proved stopped. Audit-full or unwritable state refuses work; never
evict unexpired records or delete state to make space. Resolved deletion
releases its fence while proof and media cleanup may remain pending, allowing
later capable releases to verify cleanup.

After 180-day target evidence expiry, resume and verify refuse. Once all old
attempts stop, abort closes `unverified` and releases the fence without querying
a target or replaying deletion. Another deletion requires a new report, show,
and confirmation. Rollback and backup restore never undo an operator deletion.
Follow [host lock safeguards](production.md#passkey-release-fence) for retained
operations. TOTP rotation, view-pass rotation, repair, and recovery archives are
separate releases.

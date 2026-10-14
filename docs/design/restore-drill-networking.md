# Restore drill networking

Proposal: [floating IP][ip], drill groups, private SSH through production.
Reject [Public NAT V2][nat]: VPC-wide routing instead of one-instance NAT.

Follow [ADR 0027](../adr/0027-vietnam-hosted-production.md) and the
[restore contract](vietnam-production.md#postgresql): HCM03-1C, HCM04 backups, 2
vCPU/4 GiB, encrypted 30 GB root and 20 GB data disks. Run only PostgreSQL 18
and pgBackRest, no app, renderer, public DB, snapshots, or backup jobs. Keep
both restores, `archive_mode=off`, checks, and timings.

## Network ownership and enforcement

OpenTofu `deploy/vn/prod/{main,variables,outputs}.tf` owns two drill groups and
permits. Preserve production's VPC/subnet/routes/group/IP; never reuse `main`.
[Groups][sg] aggregate, allow stateful replies, and block unpermitted new flows.
Their default egress is deletable but [provider 1.3.21][provider] leaves it
unmanaged. Devops owns idempotent `deploy/vn/scripts/drill-network.sh`:

1. Use reviewed Tofu group IDs/rule tuples; refuse system/production groups and
   attached servers. Lock applies, reconciliation, policy/key changes, and
   drills.
2. Run `vngcloud network list-security-group-rules --security-group-id <id>`.
   Remove only unmanaged default allow-all egress rules, IPv4 and IPv6, using
   `network delete-security-group-rule --security-group-id <id>` with
   `--security-group-rule-id <rule-id> --yes`. Unknown rules abort for review.
   Only Tofu creates/changes permits. Re-list; require exact expected rules.
3. Reconcile every apply/recreation; new groups stay untrusted and detached
   until audited. Defaults must stay absent. Audit rules/attachments before
   allocation, before secrets, and every 60 seconds; drift triggers cleanup. No
   parallel edits.

Both groups admit only production's private `/32` to TCP 22922. Bootstrap
permits TCP/UDP 53 to the chosen resolver, UDP 123 to the chosen time service,
and TCP 80/443 to reviewed package CIDRs. Restore permits only TCP 443 to
reviewed HCM04 CIDRs. Preserve required DHCP and stateful replies; permit no
other new flows, including to production, metadata, or IPv6. Validate address
inputs; never use broad private-network permits.

Before secrets: install signed/pinned packages without unlisted redirects; stop
PostgreSQL/package timers; pin HCM04 IPs/TLS hostname; add guest rules; attach
only restore group. Reboot, verify DHCP/SSH/egress. No apt/DNS/CIDR changes;
drift aborts.

## Backup authorization and custody

Use `aboutme-drill-reader`, with no IAM policies, roles, groups, or console
access; never retain its client secret. Resolve its vStorage SubUserID through
`ensure-service-account-principal`; the sole reader principal is
`{"AWS":"arn:aws:iam:::user/<SubUserID>"}`, never root or the writer. Keep
bucket/objects private with no reader ACL grants. The [bucket policy][policy]
adds exactly these `Allow` statements, version `2012-10-17`, for that principal:

| Sid           | Action                 | Resource                                      | Condition                                    |
| ------------- | ---------------------- | --------------------------------------------- | -------------------------------------------- |
| DrillLocation | `s3:GetBucketLocation` | `arn:aws:s3:::aboutme-backups`                | None                                         |
| DrillList     | `s3:ListBucket`        | `arn:aws:s3:::aboutme-backups`                | `StringLike: {"s3:prefix":"aboutme-prod/*"}` |
| DrillRead     | `s3:GetObject`         | `arn:aws:s3:::aboutme-backups/aboutme-prod/*` | None                                         |

List prefixes must include `aboutme-prod/`; absent/empty prefixes, bare
`aboutme-prod`, and other prefixes fail. GetObject covers HEAD and range reads.

`buckets.sh` accepts only absent policy on a new bucket, exact
`Bucket`/`Objects` writer grants, or those grants plus these reader statements.
Normalize ordering only. Reject unknown/duplicate/malformed statements or
changed actions, conditions, resources, or principals, even under known Sids.
Preserve writer grants. Use disposable fixtures only; failed required denials or
excess rights block delivery:

| Attached-reader fixture                                                                                                         | Required result                                                                              |
| ------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------- |
| Prefix GET/HEAD/list                                                                                                            | Allow                                                                                        |
| Root/sibling object listing; object PUT/DELETE; multipart/version work; policy changes; IAM; unrelated private bucket's objects | Deny                                                                                         |
| Signed `GET /`; `PUT /<unique-fixture-bucket>`; object writes in that new bucket                                                | Record scope/results; treat listing/creation as residual risk, assess writes before approval |
| Both project requests after revocation                                                                                          | Deny; operator removes created fixtures                                                      |

Per run, use `create-s3-key --service-account-id <reader-id> --secret-file`;
verify attachment, journal only UserKeyID. Exit [revokes][keys] with
`delete-s3-key`; prove denied fixture reads or, without the secret,
key-inventory absence. Send only that key and production's cipher field over
pinned SSH stdin. Laptop staging: nonswapping tmpfs, 0600, erased after
delivery. Drill: `/run` tmpfs, root:postgres 0440, parent 0750. Disable
swap/core dumps. No secrets in arguments/logs/user data/state.

Cloud/state/SSH keys stay on laptop; writer/app keys in production. Use key-only
`aboutme-admin`, host pins, no forwarding; admins keep console access. Visitors,
users, stolen tokens, hostile resumes get no drill service. Never render/execute
row content. Containers need SSH keys; production root exposes backups. TLS, SSH
pins, package signatures resist interception. Database writers get no app keys;
leaked readers expose encrypted backups and the capabilities below.

**Reader-key residual risk:** [CLI notes][cli-keys] report policy-independent
bucket listing/creation. Secondary claims need fixture proof; assume success.
Neither is blocked, including outside the drill. Scope: attached HCM04 project,
key creation to revocation, normally three hours, extended by
controller/provider failure. Names leak; unwanted buckets consume quota/cost
without per-run caps. Revocation cannot recall names/delete buckets; operators
clean up attributed buckets. New-bucket object writes remain unproved.

**Export residual risk:** HCM04's [shared endpoint][keys] lets compromised root
PUT plaintext/passphrases to attacker buckets over 443 with attacker
credentials. Filtering/reader keys block neither export nor SSH replies; other
new flows stay blocked. Exposure: secret delivery to external
isolation/deletion, normally three hours; laptop/provider failure extends it
until attended cleanup. Copies cannot be recalled. Revoke reader, isolate/delete
drill, create fresh repository/passphrase, verify full restore. Only then may
the owner delete old backups, losing their recovery window, or retain them
restricted.

## Shared journal, lock, and recovery

Add explicit state-first bootstrap to `buckets.sh` for
[initial setup](../../deploy/vn/README.md#build-order). Owner setup IAM
credentials call cloud APIs for only `aboutme-tfstate`, its service
account/principal, private policy, attached key; preserve guide encryption/key
storage. Before mutation, one attended operator excludes all other project
key/bucket/policy writers, portal/automation, applies, and drills across
machines. No distributed lock exists; refuse without administrative exclusivity.
Durably journal intents, full key-ID baseline, outcomes/IDs outside Git without
secrets. Crashes retain exclusion: owner appoints one successor, transfers
journal, and stops the prior controller or obtains provider-confirmed
revocation/expiry of all its credentials/sessions. Settle in-flight requests
before successor reconciles/completes bootstrap; no S3 claim needed. Missing
journal/uncertain ownership blocks progress; never retry ambiguous creates.
Verify state access/create-only PUTs, acquire `setup` generation 1, copy/read
back journal, then hand off to the lock for media/backup setup. Existing
installations use their state key. Missing keys/records or partial bootstrap
require recovery, never automatic bootstrap or lock reset.

Keep secret-free records at `s3://aboutme-tfstate/restore-drill/v1/` on HCM04,
accessible from any operator machine with state credentials. Use create-only
PUTs (`If-None-Match: *`); read back uncertain writes. Conditional DELETE
[is ignored](vietnam-production.md#media); never use it to release locks.

| Record                        | Fields                                                                                                                                                                                 |
| ----------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `locks/<generation>.json`     | Version 1, kind (`setup`/`drill`), run UUID, random token, operator, UTC start/deadline, predecessor release hash                                                                      |
| `releases/<generation>.json`  | Exact token, terminal journal hash, evidence                                                                                                                                           |
| `runs/<UUID>/<sequence>.json` | Version 1, generation/token, previous hash, UTC time, phase, compute/storage projects, region/zone, resource type/name, attempt UUID, request/order ID, resource IDs, status, evidence |

Start at generation 1; contenders choose the same successor only after release.
Setup releases after audited reconciliation/settled intents; drills after
cleanup. Never delete/reuse records or expire locks; gaps/conflicts/invalid
hashes abort. Journal and acknowledge intents before mutations, outcomes
afterwards, including automatic disk/port/IP IDs. Reconcile ambiguous creates by
name, scope, relationships, and order status; never retry. Unsettled orders stay
pending despite empty inventory.

Except initial state bootstrap, all project S3-key creation, including
`buckets.sh`, holds this lock. Journal full pre-create key IDs and target
SubUserID; require zero reader keys before drill creation. Lost responses
require full paginated inventory comparison, including unattached keys. Revoke
reader keys and attributed deltas, never baseline/unattributed keys. Ambiguity
retains the lock until operator/provider evidence resolves ownership. Never
recover secrets or retry creation.

`restore-drill.sh --cleanup-run <UUID>` on any operator machine validates
scope/records, refuses production IDs, only deletes. Claim
`runs/<UUID>/recovery/<epoch>.json`: sequential epochs from 1, create-only
election, predecessor hash, lock token, operator, fencing evidence. First stop
the prior controller or obtain provider-confirmed revocation/expiry of all its
cloud/state credentials/sessions; settle orders. No timed takeover. Only latest
valid claimant mutates/releases after settled intents and durable cleanup
evidence. Crashes reread the chain.

## Deletion and completion

The owner, who wrote the `vngcloud` CLI, confirmed on 2026-10-13 that a
GreenNode delete is permanent: deleted servers, disks, ports, and floating IPs
leave no recoverable copy. There is no trash to bypass and no purge step.

| Resource        | Documented behavior and required verification                                                                                                     |
| --------------- | ------------------------------------------------------------------------------------------------------------------------------------------------- |
| Server          | [Termination][server] deletes instance-store data. Check complete active and pending server inventories.                                          |
| Root/data disks | [Volumes][volume] bind root to its server; standalone deletion requires Available. Check each disk ID in complete active and pending inventories. |
| Ports           | Check every run port ID, including automatically created ports, in complete active and pending inventories.                                       |
| Floating IP     | [Disassociation][ip] keeps billing; release is separate. Check complete active and pending allocation/reservation inventories.                    |

On completion/failure/EXIT/INT/TERM/timeout, attempt all: stop PostgreSQL, erase
reachable tmpfs, revoke keys, disconnect/release IP, delete server
(`--delete-volumes`) and detached disks. Delete every remaining run resource,
including ports, snapshots, backups, and created buckets. Poll complete active
and pending inventories and orders for every run resource, including all pages;
errors never prove absence. Journal UTC/IDs/action/absence. Failed or ambiguous
deletes, incomplete inventories, and unsettled orders retain the shared lock and
`cleanup-pending` until resolved. A 404, delete receipt, or active-list absence
alone cannot close a run. Exclude physical erasure/provider metadata, including
cloud-init SSH keys.

Before secret delivery, prove the drill's cleanup code on disposable fixtures:
deletion, complete inventory checks, journaled absence, and lock release only
after confirmed cleanup. Verify that failed or ambiguous deletion retains the
lock. This proof checks the drill's code paths; provider deletion permanence
rests on the owner's confirmation above.

Budget three hours: setup 30 minutes, two one-hour restores, cleanup 30 minutes.
Stop restores at 150 minutes, sooner on failure. API calls cap at 60 seconds;
SSH at remaining phase time. At 180 minutes, unfinished cleanup fails and
retains the lock. SIGKILL/laptop loss needs recovery. Success requires both
restores and no run server, disk, port, IP, snapshot, backup, reader key, or
created bucket, active or pending. Persistent groups stay detached and audited.
Preserve source backups; record counts/timings, never rows.

## Cost, approval, and delivery

`--max-price` caps **VAT-inclusive VND/month quotes**, not invoices. Sum
server/root, encryption, disk, IP without omitted/duplicate charges. Refresh
[quotes](../../deploy/vn/README.md#paid-resources-and-quotes); verify rounding,
minimums, request/transfer tariffs before allocation. Billing lasts until
release.

Proxies load production; preloading needs transport; another VPC adds lifecycle
work/isolation. Owner approves both risks/approach, monthly ceiling, and
compromised backup deletion. Reject `--keep`, `--resume`, `--direct`; no public
client/schema changes. Release: approval/review, policy/network tools,
disposable proofs, drill script, host guide. Hosted VN tests: unknown policies,
bootstrap/recreation, ambiguous creates, crashes, stale controllers, cleanup.
Live proofs precede secrets.

Approval (2026-10-11, the primary on the owner's delegation): the temporary
floating IP approach and its stated residual risks are approved. The
`--max-price` ceiling is 1,100,000 VND/month (VAT included), above the 1,017,880
VND/month quote for the drill's server, encryption, disk, and IP. Deleting old
backups after a compromise is not pre-approved; the owner decides at the time.

[nat]: https://docs.greennode.ai/vnetwork/public-nat-instance/create-nat
[ip]: https://docs.greennode.ai/vserver/compute-hcm03-1a/vpc/floating-ip
[sg]:
  https://docs.greennode.ai/vserver/compute-hcm03-1a/security/security-groups
[provider]:
  https://github.com/vngcloud/terraform-provider-vngcloud/blob/v1.3.21/resource/vserver/resource_secgroup.go
[policy]:
  https://docs.greennode.ai/vstorage/object-storage/object-storage-hcm04/cac-tinh-nang-cua-object-storage/lam-viec-voi-bucket/working-with-buckets-via-vstorage-portal/bucket-policy
[keys]:
  https://docs.greennode.ai/vstorage/object-storage/object-storage-hcm04/quan-ly-truy-cap/working-with-s3-keys
[cli-keys]:
  https://github.com/dannyota/vngcloud/blob/v0.66.0/internal/cli/gendocs_notes_storage.go
[server]:
  https://docs.greennode.ai/vserver/compute-hcm03-1a/instance/instance-lifecycle
[volume]: https://docs.greennode.ai/vserver/compute-hcm03-1a/volume

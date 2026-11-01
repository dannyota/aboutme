# Production pull deployment protocol

See the [design overview](README.md) for scope and approval.

## Closed schemas

Every listed property is required. `?` permits JSON `null`, never omission.
Reject additional properties, duplicate keys, invalid enums, noninteger numbers
where integers are required, and noncanonical JSON. Arrays contain unique
entries.

`U` is an integer from 0 through `2^53−1`; `ID` is a positive `U`; `H` is
`sha256:` plus 64 lowercase hex digits; `SHA` is 40 lowercase hex digits; `UUID`
is a lowercase UUIDv4; `T` is UTC `YYYY-MM-DDTHH:MM:SSZ`. `Tag` is
`vMAJOR.MINOR.PATCH`, without leading zeros, with components 0–999; compare
components numerically. `IPv4` is canonical dotted-decimal, globally routable
unicast, at most 15 bytes, without leading zeros.

Requests use RFC 8785 canonical UTF-8 bytes, at most 16 KiB. Their identity is
SHA-256 of those bytes. Strings are at most 256 bytes unless a stricter type
applies.

```text
Release = { tag:Tag, tag_object:SHA, source_commit:SHA, release_id:ID }
Image = { repository:string, index_digest:H, amd64_digest:H, arm64_digest:H }
Images = { server:Image, web:Image, caddy:Image }
ObserverState = { tag:Tag, source_commit:SHA, digest:H }
Observer = { action:"keep"|"replace", image:ObserverState? }
Coordinator = { commit:SHA, bundle_hash:H, control_version:U, policy_hash:H, toolchain_hash:H }
Compatibility = { migration_manifest:H, supported_document_versions:U[], rollback_evidence_hash:H? }
Publication = { workflow_id:ID, run_id:ID, attempt:1, digests_asset_hash:H }
Authorization = { kind:"github", workflow_id:ID, run_id:ID, attempt:1, environment_id:ID } | { kind:"ssh" }
Request = {
  schema:1, repository_id:ID, repository:"dannyota/aboutme", target:"production",
  epoch:UUID, generation:ID, expected_state:H, mode:"deploy"|"rollback"|"activate",
  release:Release, images:Images, observer:Observer, coordinator:Coordinator,
  compatibility:Compatibility, publication:Publication?, authorization:Authorization,
  created_at:T, expires_at:T, nonce:H
}
Installed = {
  release:Release, server:H, web:H, caddy:H, observer:ObserverState?,
  maintenance:ObserverState, bundle_commit:SHA, bundle_hash:H, policy_hash:H,
  toolchain_hash:H, minimum_tag:Tag, config_revision:U, applied_config_revision:U
}
Expectation = { epoch:UUID, generation:U, revision:U, paused:boolean, installed:Installed }
Binding = { operation_id:UUID, request_hash:H, generation:ID }
Completion = { binding:Binding, installed:Installed, outcome:TerminalOutcome, committed_at:T }
ConfigChange = {
  change_id:UUID, previous_revision:U, next_revision:U, started_at:T, recovery_binding:Binding?
}
DeliveryState = { schema:1, expectation:Expectation, owner:Binding?, completion:Completion?, config_change:ConfigChange? }
Phase = "admitted"|"prepared"|"backed_up"|"quiescent"|"maintenance_proved"
  |"host_applied"|"rendered"|"migrated"|"started"|"smoke"|"ready_to_commit"
  |"committed"|"complete"|"failed"|"recovering"
Step = "prepare"|"backup"|"quiesce"|"host_apply"|"render"|"migrate"|"start"
  |"switch_edge"|"start_timers"|"ban_add"|"ban_remove"|"activate"
Outcome = "in_progress"|"succeeded"|"failed"|"unknown"|"manual_required"|"aborted"|"restored"|"repaired"
TerminalOutcome = "succeeded"|"aborted"|"restored"|"repaired"
Executor = { boot_id:UUID, invocation_id:string, recovery_attempt:U }
Recovery = {
  action:"abort_unchanged"|"restore_previous"|"settle_repaired",
  evidence_hash:H, proposed_installed:Installed?
}
Journal = {
  schema:1, binding:Binding, request:Request, previous:Installed,
  phase:Phase, pending_step:Step?, outcome:Outcome, executor:Executor?,
  quiesce_intent:boolean, host_write_intent:boolean, render_intent:boolean,
  migration_intent:boolean, startup_intent:boolean, activation_intent:boolean,
  backup_label:string?, proofs:H[], recovery:Recovery?, proposed_installed:Installed?,
  cleanup:"pending"|"running"|"done"|"unknown", created_at:T, updated_at:T
}
```

Image repositories equal `ghcr.io/dannyota/aboutme-{server,web,caddy}` for their
respective fields. Maintenance uses the Caddy repository. Observer replacement
uses only `ghcr.io/dannyota/aboutme-observer` and cannot be null. `keep` equals
the installed observer, including null when absent.

`tag_object` records the fetched tag ref’s object ID, whether annotated or
lightweight. `nonce` contains 256 random bits. GitHub requests use run creation
time; SSH requests use creation time checked against host time. Expiry is
exactly 24 hours later. Admission requires synchronized time, no future creation
time, and an unexpired request.

`migration_manifest` hashes canonical sorted `{path,sha256}` entries for
migration SQL bytes. Supported document versions come from the target release
contract. The host independently measures live compatibility.
`rollback_evidence_hash` is non-null for rollback and null otherwise; it binds a
reviewed compatibility record, never an unchecked assertion.

Store `DeliveryState` in the existing fence’s `delivery` extension, preserving
other fields and authorities. `minimum_tag` agrees with the existing monotonic
security floor. `expected_state` hashes canonical `Expectation`; request
generation equals consumed generation plus one. Refuse integer exhaustion.

Journals occupy root-only `/srv/data/deployments/<operation-id>/`, with 0700
directories, 0600 files, at most 64 KiB per journal, and at most six proof
hashes. State writes use file fsync, atomic rename, and parent-directory fsync.
Reject symlinks and unexpected mounts.

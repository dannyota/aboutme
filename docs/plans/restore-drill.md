# Restore drill delivery

Implement [restore drill networking](../design/restore-drill-networking.md) before running either restore against production backups. Follow [workflow](../../instructions/workflow.md) for separate implementation and review jobs. The primary owns Git, checks, and production actions.

## Constraints

- Preserve production's VPC, subnet, routes, security group, server, disks, and floating IP.
- New drill groups remain detached and untrusted until default egress has been removed and the rules audited.
- Keep cloud and state credentials on the laptop. Give the drill only its per-run backup reader key and the backup cipher field.
- Live permission, networking, and deletion proofs must pass before secret delivery.
- Keep the shared lock on ambiguous creates, unfinished deletion, or uncertain controller ownership.
- Run tests in GitHub CI. Locally run only formatting and `make pre-push`.

## Work order

1. **Done: provider deletion contract.** The owner confirmed on 2026-10-13, from building the `vngcloud` CLI, that a GreenNode delete is permanent: deleted servers, disks, ports, and floating IPs leave no recoverable copy and need no purge step. Check the first drill's floating-IP charge on the following bill.
2. **In progress: journal transport and exclusive recovery.** The native object proof passed on 2026-10-13 with `vngcloud` v0.81.1 and the read-only `aboutme` profile ([evidence](traceability/vietnam-operations.md#restore-prerequisites)). Next, implement record schemas, hash chains, generations, mutation intents, ambiguous-response recovery, and explicit controller fencing before using the shared state lock.
3. **Planned: network rule reconciliation.** After the shared lock and mutation journal exist, capture default IPv4/IPv6 egress identities on fresh detached group fixtures. Verify complete rule and attachment inventories. Add `deploy/vn/scripts/drill-network.sh` and its failure tests against those observed contracts. Unknown rules or attachments fail closed. Refresh floating-IP quotes before allocation.
4. **Planned: backup policy and key custody.** Update `deploy/vn/scripts/buckets.sh` for state-first bootstrap, exact writer/reader policies, the common lock, complete key inventory, and per-run key revocation. Test excess grants, unknown policy statements, lost create responses, and revocation failures. Establish inventory completeness from provider evidence; do not assume that an unpaged response is complete.
5. **Planned: drill execution.** Replace the unsafe continuation and keep modes in `deploy/vn/scripts/restore-drill.sh`. Implement bounded setup/restores/cleanup, secret transfer after audits, floating-IP lifecycle, journaled resources, and cleanup recovery. Preserve both restore checks and count-only evidence.
6. **Planned: disposable proofs and production drill.** Prove network denial, storage permissions, and revocation. Run both restores only after those proofs pass. Record timings and cleanup evidence, then update acceptance traceability and remove the completed backlog items.

## Open evidence

- A completed GreenNode delete ends the resource. Inventories of the drill's active and pending resources must still be complete.
- Rule deletion must identify only provider defaults. Do not infer default status from a broad CIDR alone.
- Create-only PUT is verified on the state bucket. Exclusive ownership still needs the controller fencing in step 2.
- Runtime permissions and cleanup remain unproved by mocked infrastructure tests.

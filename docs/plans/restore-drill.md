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

1. **Planned: provider evidence and rule reconciliation.** Verify complete rule and attachment inventory shapes, default IPv4/IPv6 egress identities, floating-IP quotes and lifecycle, and active/recoverable resource inventories. Add `deploy/vn/scripts/drill-network.sh` and its failure tests once those contracts are confirmed. Unknown rules or attachments fail closed.
2. **Planned: journal and exclusive recovery.** Implement create-only state records, generations, mutation intents, ambiguous-response recovery, and explicit controller fencing. Choose the signing client from the repository's supported tools. Prove conditional writes on disposable objects before using the shared state lock.
3. **Planned: backup policy and key custody.** Update `deploy/vn/scripts/buckets.sh` for state-first bootstrap, exact writer/reader policies, the common lock, paginated key inventory, and per-run key revocation. Test excess grants, unknown policy statements, lost create responses, and revocation failures.
4. **Planned: drill execution.** Replace the unsafe continuation and keep modes in `deploy/vn/scripts/restore-drill.sh`. Implement bounded setup/restores/cleanup, secret transfer after audits, floating-IP lifecycle, journaled resources, and cleanup recovery. Preserve both restore checks and count-only evidence.
5. **Planned: disposable proofs and production drill.** Prove network denial, storage permissions, revocation, and permanent deletion. Run both restores only after those proofs pass. Record timings and cleanup evidence, then update acceptance traceability and remove the completed backlog items.

## Open evidence

- Provider responses must establish complete inventories, including trash or recoverable resources. A 404 or a successful delete call alone does not prove permanent deletion.
- Rule deletion must identify only provider defaults. Do not infer default status from a broad CIDR alone.
- The shared journal needs a supported signing client and verified create-only PUT behavior.
- Runtime permissions and cleanup remain unproved by mocked infrastructure tests.

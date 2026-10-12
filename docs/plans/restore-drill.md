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

1. **In progress: security-group configuration.** Add optional bootstrap and restore groups in `deploy/vn/prod/main.tf`, validated operator inputs in `variables.tf`, and group/policy outputs in `outputs.tf`. Keep the option disabled by default. Bootstrap permits private SSH from production, resolver DNS, time synchronization, and package HTTP/HTTPS. Restore permits the same SSH source and HCM04 HTTPS only. Neither group attaches to a server in this change. Add credential-free mocked OpenTofu tests under `deploy/vn/prod/tests/` and run them from the existing VN infrastructure check. Cover disabled defaults, exact permit tuples, production isolation, and rejected malformed or overly broad addresses. Document the opt-in and remaining reconciliation gate in `deploy/vn/README.md`.
2. **Planned: provider evidence and rule reconciliation.** Verify complete rule and attachment inventory shapes, default IPv4/IPv6 egress identities, floating-IP quotes and lifecycle, and active/recoverable resource inventories. Add `deploy/vn/scripts/drill-network.sh` and its failure tests once those contracts are confirmed. Unknown rules or attachments fail closed.
3. **Planned: journal and exclusive recovery.** Implement create-only state records, generations, mutation intents, ambiguous-response recovery, and explicit controller fencing. Choose the signing client from the repository's supported tools. Prove conditional writes on disposable objects before using the shared state lock.
4. **Planned: backup policy and key custody.** Update `deploy/vn/scripts/buckets.sh` for state-first bootstrap, exact writer/reader policies, the common lock, paginated key inventory, and per-run key revocation. Test excess grants, unknown policy statements, lost create responses, and revocation failures.
5. **Planned: drill execution.** Replace the unsafe continuation and keep modes in `deploy/vn/scripts/restore-drill.sh`. Implement bounded setup/restores/cleanup, secret transfer after audits, floating-IP lifecycle, journaled resources, and cleanup recovery. Preserve both restore checks and count-only evidence.
6. **Planned: disposable proofs and production drill.** Prove network denial, storage permissions, revocation, and permanent deletion. Run both restores only after those proofs pass. Record timings and cleanup evidence, then update acceptance traceability and remove the completed backlog items.

## Security-group implementation

Use one nullable `drill_network` input object. `null` adds no resources. An enabled object supplies production's private IPv4 `/32`, resolver and time-service IPv4 `/32` sets, and reviewed public package and HCM04 IPv4 CIDR sets. Require nonempty sets. Reject malformed addresses, IPv6, unrestricted ranges, and private, loopback, link-local, or multicast package/storage destinations. Do not guess live endpoints in defaults.

Keep production resource blocks unchanged. Name the new groups `aboutme-drill-bootstrap` and `aboutme-drill-restore`. Use SSH TCP 22922 from the one production address, DNS TCP/UDP 53, time UDP 123, packages TCP 80/443, and HCM04 TCP 443. Expose the IDs and exact configured rule tuples for later reconciliation. State plainly that unmanaged provider default rules still require removal before any attachment.

Write the tests before the resources. Without an approved local exception, record the regression tests as unrun until hosted CI executes them. Use `mock_provider` and plan-only runs; no cloud credentials or live resources. The existing formatting and validation checks remain required. Independent review must cover the infrastructure and CI test change before merge.

## Open evidence

- Provider responses must establish complete inventories, including trash or recoverable resources. A 404 or a successful delete call alone does not prove permanent deletion.
- Rule deletion must identify only provider defaults. Do not infer default status from a broad CIDR alone.
- The shared journal needs a supported signing client and verified create-only PUT behavior.
- Runtime permissions and cleanup remain unproved by mocked infrastructure tests.

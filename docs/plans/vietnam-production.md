# Vietnam production migration

Status: done. Production cut over on 2026-10-11 with release v0.7.3. AWS was removed on 2026-10-11. Design: [Vietnam production](../design/vietnam-production.md). Decision: [ADR 0027](../adr/0027-vietnam-hosted-production.md).

## Open items

- Put the Cloudflare DNS records under OpenTofu. The owner edits them by hand today.
- Give the restore-drill server outbound networking without adding a Public NAT to the production VPC. Public NAT adds a `0.0.0.0/0` route for the whole VPC and sends every VM's outbound traffic through it.
- Add host-side signature verification for `/verify`.
- Diagnose readiness after a hard reset. Readiness stayed failed once until `aboutme-server` restarted.
- Add a static-asset flood scenario with a measured threshold.
- Add and review a host procedure for TOTP key re-encryption.
- Add and review a host procedure for abuse account deletion.
- Add and review a host procedure for rotating the sign-in-to-view key.

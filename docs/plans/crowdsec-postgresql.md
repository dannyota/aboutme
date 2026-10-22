# CrowdSec PostgreSQL release work

Status: in progress. The host migration, catalog gate, bounded retention, seven-field feed, and privacy notice are implemented. The [runbook](../runbooks/crowdsec-postgresql.md) owns operator commands; the [privacy design](../design/crowdsec-http-privacy.md) owns behavior.

- Obtain independent adversarial review of the host migration and explicit reset, including interrupted provisioning, private registration, failure stops, timer order, and ordinary deploy integration.
- Run `make pre-push` in the primary session and require green GitHub CI on the exact candidate. Include the migration ordering test, PostgreSQL catalog and reset tests, real-consumer retention, notice, deployment safety, and workflow contracts.
- Resolve every review and CI defect, then land through the ordinary pull request flow.
- Publish and deploy one eligible patch release through the ordinary deployer. Preserve the completed privacy-notice activation and the release floor.
- Verify the deployed database identity, peer map, connection limits, catalog policy, current retention lease, enabled and active timers, scheduled vacuum, local API, bouncers, and public enforcement without retaining attack values.
- Record aggregate production resource, sensitive error-sink, runtime egress, backup inclusion, and expiry evidence under the existing acceptance rows. Never retain request values, credentials, or database rows in reports.
- Keep reset after a cluster restore as an explicit operator action. Record a reviewed reset proof that replaces only the CrowdSec database and preserves application state.

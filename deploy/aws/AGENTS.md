# Undeployed AWS reference

## Source map and status

This directory is an undeployed reference. Production runs in `deploy/vn/` under [ADR 0026](../../docs/adr/0026-vietnam-hosted-production.md). AWS roots, resource names, and adapter configuration here are not evidence of live resources or authority to operate them.

|Path|Role|
|-|-|
|`bootstrap/`, `prod/`|Retained OpenTofu roots|
|`modules/`|Reusable network, identity, data, host, edge, tasks, operations, DNS, and observer modules|
|`scripts/`|Retained deployment and observer coordinators with static test fixtures|
|`probe/`|Reference service probes|

## Sources, outputs, and boundaries

OpenTofu roots compose `modules/` into infrastructure plans. Scripts consume image release metadata and module outputs for the reference platform. State and generated plans are local operator artifacts, not tracked sources. Reuse requires a brief naming the target environment, reviewed authority, and exact paths; never treat a reference script as a live production procedure.

## Authorities and checks

Read [deployment rules](../../instructions/deploy.md), [deployment design](../../docs/design/deployment.md), [Vietnam production design](../../docs/design/vietnam-production.md), and [production runbook](../../docs/runbooks/production.md). Retained AWS context is in [CloudFront runbook](../../docs/runbooks/cloudfront.md) and [AWS observer runbook](../../docs/runbooks/aws-observer.md).

`scripts/deploy_test.sh` and `scripts/observer_test.sh` cover retained coordinator behavior with stubs. [Verification](../../instructions/verification.md) and [release rules](../../instructions/releases.md) own hosted evidence; static reference tests do not prove a live AWS deployment.

# Architecture decision records

An architecture decision record (ADR) records one proposed or accepted choice,
its reason, and its consequences. The current design states the resulting rules
in [`../design/`](../design/README.md).

ADRs are append-only history. Supersede an accepted decision with a new ADR; do
not edit the old record to make it appear that the later choice was always in
force. A draft ADR may change until accepted.

The most recent accepted decisions are
[ADR 0037](0037-single-host-production-without-hosted-uat.md), which sends the
first release straight to single-host production behind Cloudflare with no
hosted UAT, and [ADR 0038](0038-single-baseline-and-plain-migrator.md), which
sets one baseline migration and a plain goose migrator.

The design's [decision index](../design/decisions.md) maps every ADR to the rule
it establishes.

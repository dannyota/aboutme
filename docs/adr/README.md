# Architecture decision records

An architecture decision record (ADR) records one proposed or accepted choice,
its reason, and its consequences. The current design states the resulting rules
in [`../design/`](../design/README.md).

ADRs are append-only history. Supersede an accepted decision with a new ADR; do
not edit the old record to make it appear that the later choice was always in
force. A draft ADR may change until accepted.

The most recent accepted decisions are
[ADR 0038](0038-single-baseline-and-plain-migrator.md), which sets one baseline
migration and a plain goose migrator, and
[ADR 0039](0039-per-provider-login-enablement.md), which enables provider login
one provider at a time.

The design's [decision index](../design/decisions.md) maps every ADR to the rule
it establishes.

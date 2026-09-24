# Architecture decision records

An architecture decision record (ADR) records one proposed or accepted choice,
its reason, and its consequences. The current design states the resulting rules
in [`../design/`](../design/README.md).

ADRs are append-only history. Supersede an accepted decision with a new ADR; do
not edit the old record to make it appear that the later choice was always in
force. A draft ADR may change until accepted.

The most recent accepted decision is
[ADR 0052](0052-guarded-token-edits-to-generated-primitives.md), which lets a
generated UI primitive carry token-colored variant edits when a guard test
protects them.

The design's [decision index](../design/decisions.md) maps every ADR to the rule
it establishes.

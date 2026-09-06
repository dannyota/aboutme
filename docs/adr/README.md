# Architecture decision records

An architecture decision record (ADR) records one proposed or accepted choice,
its reason, and its consequences. The current design states the resulting rules
in [`../design/`](../design/README.md).

ADRs are append-only history. Supersede an accepted decision with a new ADR; do
not edit the old record to make it appear that the later choice was always in
force. A draft ADR may change until accepted.

Recent accepted decisions include [ADR 0032](0032-public-share-image.md) for the
public share image and
[ADR 0034](0034-scheduled-uat-and-production-autoscaling.md), which replaces the
single-host AWS comparison baseline with scheduled UAT and a replica-safe,
autoscaling production target.

The design's [decision index](../design/decisions.md) maps every ADR to the rule
it establishes.

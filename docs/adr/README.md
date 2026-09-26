# Architecture decision records

An architecture decision record (ADR) records one proposed or accepted choice,
its reason, and its consequences. The current design states the resulting rules
in [`../design/`](../design/README.md).

ADRs are append-only history. Supersede an accepted decision with a new ADR; do
not edit the old record to make it appear that the later choice was always in
force. A draft ADR may change until accepted.

The most recent accepted decision is
[ADR 0056](0056-route-53-production-dns.md), which moves production DNS from
Cloudflare to Route 53 so that CloudFront picks edges near the viewer.

[ADR 0053](0053-public-pdf-tab-renders-the-download-in-the-browser.md) is
rejected: public pages have no PDF tab.

The design's [decision index](../design/decisions.md) maps every ADR to the rule
it establishes.

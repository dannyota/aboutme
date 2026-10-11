# Historical research

These observations, estimates, and experiments are historical evidence, not
project authority or current operating instructions. Their claims describe the
systems and assumptions studied at the time. The [design](../design/README.md)
and [accepted ADRs](../adr/README.md) own project decisions; the
[production runbook](../runbooks/production.md) owns live operations.

| Research                                                   | Material                                                       | Status                                                                      |
| ---------------------------------------------------------- | -------------------------------------------------------------- | --------------------------------------------------------------------------- |
| [FlowCV](flowcv/README.md)                                 | Competitor feature inventory and gap analysis                  | Historical observations; not verified against the live API                  |
| [AWS cost comparison](aws-cost/comparison.md)              | Workload estimates, pricing inputs, and cost models            | Historical AWS estimates; not the deployed topology                         |
| [AWS cost recommendation](aws-cost/recommendation.md)      | Hosting recommendation and operating assumptions               | Historical recommendation; production follows ADR 0027                      |
| [Caddy header experiment](caddy-header-boundary/README.md) | Native matchers tested against duplicate and malformed headers | Historical CloudFront origin-secret experiment; not the live trust boundary |

Production runs in Vietnam under
[ADR 0027](../adr/0027-vietnam-hosted-production.md). AWS code remains an
undeployed reference. Retain the research inputs and results as evidence; do not
read their former production claims as current deployment instructions.

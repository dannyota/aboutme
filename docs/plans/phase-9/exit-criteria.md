# Phase 9 exit criteria

- [ ] Tasks 9.1–9.3 have their declared outputs with dated official sources,
      reproducible units, and explicit workload assumptions.
- [ ] Singapore UAT, idle/retained-resource, and production costs are separate;
      restore drills and recurring operational jobs are included.
- [ ] Free public image builds/native smoke and private `aboutme-infra`
      validation/publication/deployment usage are separate from AWS runtime.
      Private quota, metadata retention, and approval-feature eligibility are
      explicit. ARM64 compatibility assumptions are recorded (ADR 0033).
- [ ] Alternatives use the same requirements and workload. Any topology change
      has an accepted ADR and matching design and plan updates.
- [ ] OpenTofu provider support and managed-service suitability are verified. A
      selected self-managed component has an explicit operating/cost rationale.
- [ ] Selected sizes, UAT lifetime, spending ceiling, alerts, cleanup, and
      remaining charges are recorded. Budget decisions are settled before
      activation.
- [ ] Phase 10 consumes the result and identifies the owner's SES handoff and
      missing mail inputs. Existing resources are not blindly recreated.
- [ ] One fresh Sol review is complete and blocking findings are resolved.
- [ ] The integration owner runs documentation checks, `make ci`, and connected
      `make scan` at one unchanged candidate before phase closure.
- [ ] No cloud mutation or purchase was performed by this phase.

Correct a wrong criterion in this phase and note the change, per ADR 0024. At
closure, delete this directory; retain the research and decision record.

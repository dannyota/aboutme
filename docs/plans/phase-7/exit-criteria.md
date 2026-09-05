# Phase 7 exit criteria

Status: Integration checks in progress, 2026-09-06.

Every row must pass at the unchanged candidate used for `make ci` and connected
`make scan`. The integration owner records exact commands and local evidence.

| Check               | Required evidence                                                                                                                                                | State              |
| ------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------ |
| Queue and authority | Admission, capability, completion, timeout, and cleanup cases in task 7.1                                                                                        | Open               |
| Owner PDF           | Owner-only export, immutable authorized snapshot, correct headers, bounded failure                                                                               | Open               |
| Private print       | ID-only/direct access denial, one-use redemption, no cookies or leaked authority                                                                                 | Open               |
| Renderer            | Go sanitization corpus, local fonts, decoded inline photo, A4/Letter and existing two-column print baselines                                                     | Open               |
| Browser isolation   | Real hostile resource, redirect, popup, worker, and network-attempt proof; process-group kill and join                                                           | Open               |
| Public PDF          | Live plus download gate before cache or conditional reuse; revocation cancels queued and active jobs and response bodies                                         | Open               |
| Generated image     | Owner-approved contract, exact dimensions and public-state matrix                                                                                                | Approved, ADR 0032 |
| Resource bounds     | Repeated minimal/full/maximum corpus, output byte sizes, queue-inclusive p95, cancellation deadline, and joined teardown, pinned 512 MiB Go plus Chromium cgroup | Open               |
| Interfaces          | OpenAPI, generated client, route parity, runtime configuration, and docs agree                                                                                   | Open               |
| Review              | Fresh Sol reviewer authored no implementation; confirms auth, sanitizing, concurrency, capability, media privacy, and revocation invariants                      | Open               |
| Phase gates         | `make ci`, `make web-e2e`, connected `make scan`, and affected native HTTP/HTTPS checks                                                                          | Open               |

Correct an unsatisfiable criterion when evidence demonstrates the defect. Record
the correction and its evidence in the same phase. Local measurements are not
AWS deployment measurements; Phase 10 repeats hosted resource checks.

The timeout criterion was corrected after the first controlled run returned
queued deadline failures 66–84 ms after the configured 20-second deadline.
Mandatory joined teardown makes admission-to-return a different measurement. The
timer remains unchanged; failed calls retain their outcome and teardown
overshoot, and successful calls above 20 seconds still fail the benchmark.

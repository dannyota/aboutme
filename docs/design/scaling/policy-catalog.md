# Admission policy catalog

This catalog lists the production rate limiters and concurrency caps and the
fleet scope each takes under
[ADR 0035](../../adr/0035-replica-coordination-and-uat-lifecycle.md). The
current columns describe built behavior. The fleet column is accepted design;
[admission](admission.md) defines it. Numeric budgets come from
[`budgets.md`](../budgets.md).

## Current rules

Every limiter is process-local under
[ADR 0018](../../adr/0018-bounded-rate-limiter.md). Token buckets use the listed
limit as burst and refill over the window. Each instance tracks at most 10,000
keys and sends new keys to one shared overflow bucket when full. Active keys are
never evicted. Denials consume no token. A denial returns HTTP 429 with a
positive `Retry-After`, rounded up, unless the row says otherwise.

When `api.RateLimit` middleware cannot derive the canonical client IP, it
charges a separate bucket keyed on the raw socket peer and returns 400
`invalid_client_ip`, or 429 once that bucket is empty.

P02 to P04 run after route authentication. P13 then P14, and P23 then P24, admit
in order without refunds. P01 has two route-selected instances in
`internal/api/router.go`, so a client can spend 300 per minute on each chain.
The fleet design shares one 300 per minute budget across both chains.

## Rate policies

Sources are under `apps/server/`.

| ID  | Policy                           | Source                               | Key and algorithm                                   | Limit                            | Fleet scope                   |
| --- | -------------------------------- | ------------------------------------ | --------------------------------------------------- | -------------------------------- | ----------------------------- |
| P01 | `api.outer_request`              | `internal/api/router.go`             | Client IP; token                                    | 300/min per chain                | One IP budget for both chains |
| P02 | `resume.read`                    | `internal/resumeapi/chain.go`        | Account then IP; token                              | 600/min                          | Account plus IP               |
| P03 | `resume.write`                   | `internal/resumeapi/chain.go`        | Account then IP; token                              | 240/min                          | Account plus IP               |
| P04 | `resume.photo_upload_rate`       | `internal/resumeapi/chain.go`        | Account then IP; token                              | 20/hour                          | Account plus IP               |
| P05 | `auth.provider_start`            | `internal/auth/start.go`             | IP when anonymous, account then IP otherwise; token | 30/min                           | One policy, both key shapes   |
| P06 | `password.login_ip`              | `internal/auth/password_rate.go`     | IP; token                                           | 30/min                           | IP                            |
| P07 | `password.login_failure_email`   | `internal/auth/password_rate.go`     | Email HMAC digest; fixed window                     | 10 failures/15 min, not extended | Email digest                  |
| P08 | `password.register_forgot_ip`    | `internal/auth/password_rate.go`     | IP; token                                           | 20/hour                          | IP                            |
| P09 | `password.register_forgot_email` | `internal/auth/password_rate.go`     | Email HMAC digest; token                            | 5/hour                           | Email digest                  |
| P10 | `password.verify_reset_ip`       | `internal/auth/password_rate.go`     | IP; token                                           | 10/hour                          | IP                            |
| P11 | `password.account_mutation`      | `internal/auth/password_rate.go`     | Account then IP; token                              | 10/hour                          | Account plus IP               |
| P12 | `resume.slug_change`             | `internal/resumeapi/slug_limiter.go` | Account; rolling window                             | 30/hour, denials count           | Account                       |
| P13 | `resume.owner_pdf_account`       | `internal/resumeapi/pdf.go`          | Account; token                                      | 10/min                           | Account                       |
| P14 | `resume.owner_pdf_ip`            | `internal/resumeapi/pdf.go`          | IP; token                                           | 10/min                           | IP                            |
| P15 | `account.export`                 | `internal/accountapi/export.go`      | Account then IP; token                              | 5/min                            | Account plus IP               |
| P16 | `account.delete`                 | `internal/accountapi/service.go`     | Account then IP; token                              | 5/min                            | Account plus IP               |
| P17 | `public.artifact_request`        | `internal/publicapi/artifact.go`     | IP; token                                           | 300/min                          | IP                            |
| P18 | `public.render_miss`             | `internal/publicapi/artifact.go`     | IP; token, after P17, cache miss only               | 20/min                           | IP                            |
| P19 | `realtime.public_request`        | `internal/realtimeapi/service.go`    | IP; token                                           | 300/min                          | IP                            |
| P20 | `oauth.register`                 | `internal/oauthsrv/rate.go`          | IP; token                                           | 5/hour                           | IP                            |
| P21 | `oauth.token`                    | `internal/oauthsrv/rate.go`          | IP; token                                           | 30/min                           | IP                            |
| P22 | `oauth.failed_grant`             | `internal/oauthsrv/rate.go`          | OAuth client UUID; fixed window with reservations   | 10 failures plus pending/15 min  | Client                        |
| P23 | `mcp.token`                      | `internal/mcpapi/rate.go`            | Durable token UUID; token                           | 120/min                          | Token                         |
| P24 | `mcp.user`                       | `internal/mcpapi/rate.go`            | Durable user UUID; token                            | 240/min                          | User                          |

Responses that differ from the default:

- P07 keeps the indistinguishable wrong-password response.
- P09 keeps the anti-enumeration response.
- P12 maps a denial to 429 with `Retry-After: 1`.
- P14 and P18 render saturation returns 503 with `Retry-After: 1`.
- P17 and P18 use the public representation's own 429.
- P20 to P22 return OAuth JSON `error=invalid_request`.
- P23 and P24 return the MCP rate error.

## Concurrency caps

| ID  | Class                  | Source                           | Current bound and response                                                    | Fleet scope                                             |
| --- | ---------------------- | -------------------------------- | ----------------------------------------------------------------------------- | ------------------------------------------------------- |
| C01 | `render.global_claim`  | `internal/renderjob/queue.go`    | 1 running, 8 waiting, 20-second deadline; full queue is 503, `Retry-After: 1` | Fleet claim, node-local render                          |
| C02 | `password.hash`        | `internal/password/admission.go` | 2 running, 16 waiting; 503 `authentication_unavailable`, no `Retry-After`     | Fleet claim                                             |
| C03 | `mail.send`            | `internal/authmail/worker.go`    | 2 sends per worker; 30-second lease, 10-second send                           | Fleet claim, leases kept                                |
| C04 | `mcp.user_concurrent`  | `internal/mcpapi/rate.go`        | 4 in flight per user; fifth is an MCP denial with `Retry-After: 1`            | Fleet per-user claim, no waiting                        |
| C05 | `sse.fleet_account_ip` | `internal/realtime/hub.go`       | 2,000 per task, 100 per IP, 20 per account; 429 or 503, `Retry-After: 5`      | Fleet IP and account claims; 2,000 per task stays local |
| C06 | `media.photo_task`     | `internal/media/admission.go`    | 1 decoder per task, waits up to 1 second; busy is 503, `Retry-After: 1`       | Stays per task                                          |

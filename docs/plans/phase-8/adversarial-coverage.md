# Phase 8 adversarial coverage

| Invariant                                        | Author      | Required evidence                                                                           |
| ------------------------------------------------ | ----------- | ------------------------------------------------------------------------------------------- |
| Cookie-only account access and no foreign data   | 8.2/8.3     | Session/CSRF/Origin/method/header matrices                                                  |
| Recent reauth remains valid at commit            | 8.2         | Session reset, revoke and rotation while waiting                                            |
| One account deletion transaction                 | 8.2         | Every cascade, exact jobs, tombstones, audit rollback                                       |
| All public generations stop before success       | 8.2/8.7     | Three-resume/global fence order, active responses and render leases, cached representations |
| Concurrent create cannot escape deletion         | 8.2         | User-locked complete-set recheck and bounded retry                                          |
| Ambiguous commit never reopens guessed state     | 8.2         | Independent committed/rollback/unresolved proofs                                            |
| Export is complete, portable and bounded         | 8.3         | Hidden/private drafts, photos/crop, max fixtures, missing media, no credentials/keys        |
| Cleanup cannot delete live media                 | 8.4         | Exact keys, current refs, queue comparison, candidate lifetime                              |
| Cleanup cannot detach work or lose retries       | 8.4         | Cancellation joins, claims, late results, cursor failure/restart                            |
| Retention preserves accounting and lock order    | 8.5         | Concurrent request cleanup/write/delete and sweep, exact bytes                              |
| Time boundaries do not purge pending work        | 8.4/8.5     | 24h/48h/90d/180d and pending/overdue jobs                                                   |
| UI deletion requires a fresh deliberate action   | 8.6/8.7     | Cancel, reauth, second confirmation, duplicate clicks, errors                               |
| Commands overlap safely and expose fixed signals | 8.4/8.5/8.7 | Advisory lock, CLI validation, cancellation and secret-free output                          |

The phase reviewer confirms auth, sessions, CSRF, public revocation,
concurrency, idempotency, media privacy, bounds and secret handling by name.

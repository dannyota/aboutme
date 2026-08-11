# Task 6b: Resume write-boundary review

Superseded by the owner on 2026-08-11. Generated sqlc write placement is an
architecture and code-review concern, not a custom static-analysis policy.

The connected Semgrep job remains responsible for SAST, dependency, and secret
scanning. Reviews of changes to resume queries or callers must confirm that
domain validation, limits, concurrency control, and idempotency are not
bypassed.

The frozen Phase 2A acceptance catalog is historical and remains unchanged.
Future catalogs must not require the removed `semgrep-policy-test` target.

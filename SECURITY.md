# Security Policy

## Reporting a vulnerability

Please **do not open a public issue** for security problems.

Report privately via
[GitHub Security Advisories](https://github.com/dannyota/aboutme/security/advisories/new).
You will get an acknowledgement within 72 hours.

## Scope notes

- Credentials never belong in the repository; `.env` is git-ignored by policy.
- The service handles personal data (resumes). Reports about data exposure,
  authentication/session flaws, publishing/robots leaks, or sanitizer bypasses
  (rich-text HTML) are especially appreciated.

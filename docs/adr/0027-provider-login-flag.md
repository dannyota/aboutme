# 0027 — Provider login behind a server flag

Status: Accepted (2026-09-02)

Amends the authentication scope in ADR 0025 and the Approved v4 product and
security design for the first release.

## Context

Version 1 ships email-and-password authentication. Google, GitHub, and LinkedIn
login are implemented and proven, but no production credentials exist for them
and the first community does not need them at launch. Leaving the routes
registered advertises dead sign-in paths and keeps three external redirect
surfaces reachable for no benefit.

## Decision

`PROVIDER_LOGIN_ENABLED` is a server flag, default `false`. When false, Go does
not register the provider start and callback routes or the authenticated
provider link and reauthentication starts; the paths return the uniform
not-found response of any unregistered route. The provider code, tests, mock
provider, and OpenAPI operations remain so a later release can turn the flag on
without a new phase. The web learns the flag through the unauthenticated
capabilities read and shows provider controls only when it is true.

## Rejected alternatives

- **Delete the provider code.** Throws away proven work and the local Google
  mock the HTTPS harness depends on.
- **Hide the buttons only.** Leaves the redirect surfaces reachable and lets the
  web and server disagree.
- **Mirror the flag into Nuxt runtime config.** Two sources of truth that can
  drift; every launcher would need a parity test.

## Consequences

- A provider-only account cannot sign in while the flag is off. No such account
  exists in production today.
- The native HTTPS harness sets the flag true because its proofs sign in through
  the local Google mock; the native HTTP stack and Compose leave it unset.
- Turning the flag on in production is a configuration change with its own
  review, not a code change.

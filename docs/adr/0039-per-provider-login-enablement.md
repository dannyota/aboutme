# 0039: Per-provider login enablement

Status: Accepted (2026-09-18).

Amends [ADR 0027](0027-provider-login-flag.md).

## Context

ADR 0027 put Google, GitHub, and LinkedIn login behind one boolean,
`PROVIDER_LOGIN_ENABLED`. The owner has decided that Google sign-in ships next
while GitHub and LinkedIn stay off. With one switch, turning on Google would
also register the GitHub and LinkedIn redirect surfaces and require their
production credentials, which do not exist.

## Decision

`PROVIDER_LOGIN_ENABLED` accepts blank, `false`, `true`, or a comma list of
distinct providers from `google`, `github`, and `linkedin`. Blank and `false`
enable none; `true` still enables all three. Any other value, an unknown
provider, or a repeated provider stops the server at startup.

Go registers the start and callback routes only for enabled providers. A
disabled provider's paths return the uniform not-found response, as in ADR 0027.
In prod and staging, only an enabled provider requires its client ID and secret.

The capabilities read keeps `providerLogin`, now true when at least one provider
is enabled, and adds `providers`: the enabled provider names in the fixed order
`google`, `github`, `linkedin`. The web renders a provider control only for a
name in that list.

Production can enable only Google; it stays off until
`provider_login_enabled = "google"`. Its client ID and secret come from SSM
parameters that the task references only while Google is enabled.

## Rejected alternatives

- **One boolean per provider.** Three variables that must agree with the
  capabilities read, where one list states the whole choice.
- **Keep one switch and ship all three.** GitHub and LinkedIn have no production
  credentials, and their redirect surfaces would be reachable for no benefit.

## Consequences

- Existing `true` and `false` settings keep their meaning.
- A web build that reads only `providerLogin` would still show every provider
  button. The web must read `providers` before any single provider is enabled.
- Enabling another provider in production means creating its SSM parameters,
  wiring them in `deploy/aws`, and changing the setting. No code change is
  needed.
- A release older than this decision accepts only blank, `false`, and `true`,
  and refuses to start with `google`. A rollback builds from the current task
  definition, so before rolling back to such a release, set
  `provider_login_enabled = ""` and apply. Enabling Google is therefore a
  separate step after a release with the list parser is live, never part of that
  release.
- An account whose only credential is Google cannot sign in while Google is off.

// Package password implements the password security primitives of ADR 0015
// within the docs/design/budgets.md bounds: canonical policy checking
// (blocklist and HIBP breach lookup), the bundled common-password blocklist,
// the HIBP range client, Argon2id hashing behind a bounded admission
// controller, and bearer-token derivation. Errors are closed sentinels and
// never contain input or dependency text.
package password

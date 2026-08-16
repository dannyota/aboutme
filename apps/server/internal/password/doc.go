// Package password implements the Phase PA password security primitives (D2):
// canonical policy checking (blocklist and HIBP breach lookup), the bundled
// common-password blocklist, the HIBP range client, Argon2id hashing behind a
// bounded admission controller, and bearer-token derivation. Errors are closed
// sentinels and never contain input or dependency text.
package password

# 0006 — Codegen derives discriminators from the schema, never a hardcoded list

Status: Accepted (2026-08-01)

## Context

A byte-compare drift test (regenerate, diff against committed output) only
certifies that generation is deterministic — it says nothing about whether the
generator is faithful to the schema. A generator whose entry-type list is
hardcoded in its own source reproduces that same omission on every run; the
drift test stays green because the output is byte-identical to itself, not
because it matches `resume.schema.json`. Concretely: adding a ninth
`sectionType` to the schema would be silently accepted by AJV (schema-driven
validation) and by the generated TypeScript union (if the generator's list were
updated) while a Go dispatch switch built from the same stale hardcoded list
silently rejected the new type at runtime — three languages disagreeing, with
the drift test never catching it because nothing forced the generator's list to
track the schema.

## Decision

Code generation **derives every discriminator and entry definition from the JSON
Schema at generation time** — never from a hardcoded list in the generator. A
**conformance test** enumerates every `sectionType` in `resume.schema.json` and
asserts that AJV, the generated TypeScript union, and the Go dispatch each
accept a same-type sample and reject cross-type entries, so adding a section
type fails loudly in all three languages instead of silently in one.

## Consequences

- Go has no sum types, so the dispatch layer that switches on `sectionType` is
  hand-written and deliberately kept **outside** generated output — it cannot
  itself be schema-derived, which is exactly why the conformance test exists to
  catch it drifting from the schema.
- The conformance test, not the drift test, is the gate that keeps AJV, the
  TypeScript union, and the Go dispatch aligned; it must run in CI alongside the
  drift check, not instead of it.

# 0052: Guarded token edits to generated primitives

Status: Accepted (2026-09-24).

Amends [ADR 0029](0029-application-ui-toolkit.md), whose first layer rule says
generated shadcn-vue primitives are never hand-styled.

## Context

The Aurora identity ([ADR 0050](0050-aurora-application-identity.md)) needs a
darker hover and a soft blue shadow on the primary button, and a text-safe blue
on link buttons. The generated button reads `bg-primary/90` on hover and
`text-primary` for links. The design keeps blue text on `--link`, which is tuned
for text contrast over the canvas, and an opacity hover lightens the fill
instead of darkening it. The seal variant already carried a local edit. Wrapping
every button in a composite only to swap three class strings adds a component
that every surface must import instead of `Button`.

## Decision

A generated primitive may carry a local edit to a variant's classes when all of
these hold:

- The edit only swaps utilities for chrome tokens that `theme.css` defines. It
  changes no markup, behavior, prop, or accessibility attribute.
- A unit test asserts each edited class and fails when a regeneration drops it.
- After `apps/web/scripts/ui-add.sh` regenerates the primitive, the author
  re-applies the edits and the guard test passes before the change lands.

Every other change to a primitive is still a regeneration plus a reviewed diff.

The button primitive (`apps/web/app/components/ui/button/index.ts`) carries
three such edits, guarded by `apps/web/test/ui/button-variants.test.ts`:

| Variant   | Edit                                                          |
| --------- | ------------------------------------------------------------- |
| `default` | `hover:bg-primary-hover` and `shadow-[var(--shadow-primary)]` |
| `link`    | `text-link` in place of `text-primary`                        |
| `seal`    | `bg-seal`, `text-seal-foreground`, and `hover:bg-seal/90`     |

## Rejected alternatives

- **An app composite that wraps `Button`.** Keeps the primitive pristine, but
  every surface and every `buttonVariants` call site must switch to it, and a
  missed call site silently falls back to the generated colors.
- **Change `--primary` itself.** One token would then serve fills and text, but
  it also changes every fill, ring, and chip that the palette tunes separately.

## Consequences

- Regenerating a primitive with local edits is a two-step change: run
  `ui-add.sh`, then restore the guarded classes. The guard test fails in CI if
  the second step is skipped.
- The exception covers class strings only. A primitive that needs new markup or
  behavior gets a composite in `components/app`.

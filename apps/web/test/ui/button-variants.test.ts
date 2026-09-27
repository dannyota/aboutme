import { describe, expect, it } from 'vitest';

import { buttonVariants } from '../../app/components/ui/button';

// The button primitive is generated (ADR 0019), but `default`, `link`,
// `seal`, and `destructive` carry local token edits that a regeneration
// would erase. This guards those edits so `scripts/ui-add.sh --overwrite`
// cannot drop them silently.
describe('button variant guard', () => {
  it('keeps the default variant on the primary fill and hover tokens', () => {
    const classes = buttonVariants({ variant: 'default' });

    expect(classes).toContain('hover:bg-primary-hover');
    expect(classes).toContain('shadow-[var(--shadow-primary)]');
  });

  it('keeps the link variant on the text-safe link token', () => {
    const classes = buttonVariants({ variant: 'link' });

    expect(classes).toContain('text-link');
    expect(classes).not.toContain('text-primary');
  });

  it('keeps the seal variant on the seal token', () => {
    const classes = buttonVariants({ variant: 'seal' });

    expect(classes).toContain('bg-seal');
  });

  it('keeps the destructive variant an outline, apart from the seal fill',
    () => {
      // ADR 0020: Delete is a burnt-orange outline so it can never read as
      // the red, filled Publish button.
      const classes = buttonVariants({ variant: 'destructive' });

      expect(classes).toContain('border-destructive');
      expect(classes).toContain('text-destructive');
      expect(classes).toContain('bg-transparent');
      expect(classes).not.toContain('bg-destructive ');
      expect(classes).not.toMatch(/\bbg-seal\b/);
    });
});

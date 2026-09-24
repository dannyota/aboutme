import { describe, expect, it } from 'vitest';

import { buttonVariants } from '../../app/components/ui/button';

// The button primitive is generated (ADR 0029), but `default`, `link`, and
// `seal` carry local token edits that a regeneration would erase. This guards
// those edits so `scripts/ui-add.sh --overwrite` cannot drop them silently.
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
});

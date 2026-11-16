import { describe, expect, it } from 'vitest';

import { buttonVariants } from '../../app/components/ui/button';

describe('button variant guard', () => {
  it('shapes every button as a flat pill at 36, 40, or 48 px', () => {
    const classes = buttonVariants();

    expect(classes).toContain('rounded-full');
    expect(classes).not.toContain('rounded-md');
    expect(classes).not.toMatch(/\bshadow-/u);
    expect(buttonVariants({ size: 'sm' })).toContain('h-9');
    expect(buttonVariants({ size: 'default' })).toContain('h-10');
    expect(buttonVariants({ size: 'lg' })).toContain('h-12');
    expect(classes).toContain('focus-visible:ring-ring');
  });

  it('keeps the default variant on the primary fill and hover tokens', () => {
    const classes = buttonVariants({ variant: 'default' });

    expect(classes).toContain('bg-primary');
    expect(classes).toContain('hover:bg-primary-hover');
    expect(classes).toContain('font-bold');
    expect(classes).not.toContain('shadow-[var(--shadow-primary)]');
  });

  it('keeps the link variant on the text-safe link token', () => {
    const classes = buttonVariants({ variant: 'link' });

    expect(classes).toContain('text-link');
    expect(classes).toContain('font-semibold');
    expect(classes).toContain('focus-visible:underline');
    expect(classes).not.toContain('text-primary');
    expect(classes).not.toContain('font-bold');
  });

  it('keeps the seal variant on the seal token', () => {
    const classes = buttonVariants({ variant: 'seal' });

    expect(classes).toContain('bg-seal');
    expect(classes).toContain('hover:bg-brand-hover');
  });

  it('keeps the destructive variant an outline, apart from the seal fill',
    () => {
      const classes = buttonVariants({ variant: 'destructive' });

      expect(classes).toContain('border-[1.5px] border-destructive');
      expect(classes).toContain('text-destructive');
      expect(classes).toContain('bg-transparent');
      expect(classes).toContain('hover:bg-destructive/10');
      expect(classes).not.toContain('bg-destructive ');
      expect(classes).not.toMatch(/\bbg-seal\b/);
    });

  it('draws the outline variant in ink with an accent hover', () => {
    const classes = buttonVariants({ variant: 'outline' });

    expect(classes).toContain('border-[1.5px] border-foreground');
    expect(classes).toContain('hover:bg-accent');
  });
});

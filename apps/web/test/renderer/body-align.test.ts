import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';

const css = readFileSync('app/components/resume/ResumeDocument.vue', 'utf8');

const rulesUsing = (property: string): string[] =>
  [...css.matchAll(/([^{}]+)\{([^}]*)\}/gu)]
    .filter(([, , body]) => body!.includes(property))
    .map(([, selector]) => selector!.trim());

describe('body text alignment', () => {
  it('aligns entry body paragraphs and list items only', () => {
    const selectors = rulesUsing('var(--body-align)');
    expect(selectors).toHaveLength(1);
    const listed = selectors[0]!.split(',').map((selector) => selector.trim());
    expect(listed).toEqual([
      '.resume-document .entry-body p',
      '.resume-document .entry-body li',
    ]);
    expect(rulesUsing('var(--body-hyphens)')).toEqual(selectors);
  });
});

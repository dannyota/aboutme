// @vitest-environment happy-dom

import { afterEach, describe, expect, it } from 'vitest';

import { removePageRule, syncPageRule } from '../../app/public/pageRule';

const A4 = '@page {\n  size: 210mm 297mm;\n  margin: 15mm 15mm;\n}';
const LETTER = '@page {\n  size: 8.5in 11in;\n  margin: 12mm 18mm;\n}';

afterEach(() => {
  document.head.replaceChildren();
});

describe('public @page rule', () => {
  it('inserts the rule as one style element in the head', () => {
    syncPageRule(document, A4);
    const styles = document.head.querySelectorAll('style');
    expect(styles).toHaveLength(1);
    expect(styles[0]!.textContent).toBe(A4);
    expect(styles[0]!.hasAttribute('data-public-page-rule')).toBe(true);
  });

  it('replaces the rule when the format or margins change', () => {
    syncPageRule(document, A4);
    const first = document.head.querySelector('style');
    syncPageRule(document, LETTER);
    const styles = document.head.querySelectorAll('style');
    expect(styles).toHaveLength(1);
    expect(styles[0]!.textContent).toBe(LETTER);
    expect(styles[0]).toBe(first);
  });

  it('leaves other style elements alone and removes only its own', () => {
    const other = document.createElement('style');
    other.textContent = 'body{}';
    document.head.append(other);
    syncPageRule(document, A4);
    removePageRule(document);
    expect([...document.head.querySelectorAll('style')]).toEqual([other]);
  });
});

import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe, expect, it } from 'vitest';

/**
 * `ResumeDocument`'s narrow-layout rules (the side photo and the two-column
 * layout) must switch on the resume's own rendered width, not the window's,
 * so a narrow editor pane shows the same layout a phone reader sees. A width
 * media query cannot do that: it only sees the window.
 */
const source = readFileSync(
  resolve(
    import.meta.dirname,
    '../../app/components/resume/ResumeDocument.vue',
  ),
  'utf8',
);

describe('ResumeDocument narrow-layout stylesheet', () => {
  it('establishes an inline-size container on the continuous document', () => {
    expect(source).toContain(
      '.resume-document:not(.resume-page) {\n  container-type: inline-size;\n}',
    );
  });

  it('queries that container instead of the window width', () => {
    const containerQueries = source.match(/@container \(width < 36em\)/g);
    expect(containerQueries).toHaveLength(2);
    expect(source).not.toMatch(/@media screen and \(width < 36em\)/);
  });
});

import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe, expect, it } from 'vitest';

/**
 * The public page and the editor's Web preview give the text its measure
 * before the template's print margin (docs/design/web.md, "Pagination and
 * print"). The rule is screen only, so the PDF, print, the paged preview, and
 * the preview card keep the template margin.
 */
const dir = resolve(import.meta.dirname, '../../app/components/resume');
const source = readFileSync(resolve(dir, 'ResumeDocument.vue'), 'utf8');
const margins = readFileSync(resolve(dir, 'resumeScreenMargins.css'), 'utf8');
const editorPreview = readFileSync(
  resolve(
    import.meta.dirname,
    '../../app/components/editor/EditorPreview.vue',
  ),
  'utf8',
);

describe('screen margin stylesheet', () => {
  it('loads inside the first style block, so every render carries it', () => {
    const imported = '@import \'./resumeScreenMargins.css\';';
    expect(source).toContain(imported);
    expect(source.indexOf(imported))
      .toBeLessThan(source.indexOf('.public-resume-page {'));
  });

  it('grows the padding from 16 px toward the template margin', () => {
    for (const axis of ['x', 'y']) {
      expect(margins).toContain(
        'clamp(16px, calc((100cqw - var(--screen-measure)) / 2), '
        + `var(--page-margin-${axis}))`,
      );
    }
    expect(margins).toContain('--screen-measure: 52em;');
    expect(margins).toContain('--screen-measure: 70em;');
  });

  it('applies to screens only, never to print or a paged page', () => {
    expect(margins.trimStart().startsWith('@media screen {')).toBe(true);
    expect(margins).not.toContain('resume-page');
    expect(margins).not.toContain('@media print');
  });

  it('reaches only the public page and the Web preview sheet', () => {
    const selectors = /([^{}]+)\{\s*padding: var\(--screen-margin-y\)/u
      .exec(margins)?.[1] ?? '';
    expect(selectors).toContain('.public-resume-page .resume-document');
    expect(selectors).toContain(
      '.preview-sheet[data-web-columns] .resume-document',
    );
  });

  it('keeps the page bar on the left edge of the body', () => {
    expect(source).toMatch(
      /\.public-toolbar-inner \{[^}]*padding-inline: var\(--screen-margin-x\)/u,
    );
    expect(source).not.toContain('padding-inline: 16px;');
  });

  it('keeps the document padding that print and the PDF use', () => {
    expect(source).toContain(
      'padding: var(--page-margin-y) var(--page-margin-x);',
    );
  });
});

describe('EditorPreview Web sheet hook', () => {
  it('marks the sheet with the column count in Web mode only', () => {
    expect(editorPreview).toContain(
      ':data-web-columns="previewMode === \'web\'',
    );
  });
});

import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe, expect, it } from 'vitest';

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
    expect(margins).not.toMatch(/[ ,]\.resume-page/u);
    expect(margins).not.toContain('@media print');
  });

  const selectorsOf = (declaration: string): string[] =>
    (new RegExp(`([^{}]+)\\{\\s*${declaration}`, 'u').exec(margins)?.[1] ?? '')
      .split(',').map((part) => part.trim()).filter(Boolean);

  it('reaches only the continuous template detail paper', () => {
    expect(selectorsOf('padding: var\\(--screen-margin-y\\)')).toEqual([
      '.template-paper[data-web-columns] .resume-document:not(.resume-sheets)',
    ]);
    expect(selectorsOf('--screen-measure: 52em;')).toEqual([
      '.template-paper[data-web-columns] .resume-document',
    ]);
    expect(selectorsOf('--screen-measure: 70em;')).toEqual([
      '.template-paper[data-web-columns="2"] .resume-document',
    ]);
    expect(margins).not.toContain('.preview-sheet');
    expect(margins).not.toContain('data-columns');
  });

  it('keeps the public page the container of the bar and the sheet gap',
    () => {
      expect(selectorsOf('container-type: inline-size;')).toEqual([
        '.public-resume-page', '.template-paper[data-web-columns]',
      ]);
    });

  it('measures the page bar from the sheet width, not the screen margins',
    () => {
      expect(source).toMatch(
        /\.public-toolbar-inner \{[^}]*padding-inline: clamp\(6px, 2cqi - 2px/u,
      );
      expect(source).not.toContain('var(--screen-margin-x)');
    });

  it('keeps the document padding that print and the PDF use', () => {
    expect(source).toContain(
      'padding: var(--page-margin-y) var(--page-margin-x);',
    );
  });
});

describe('EditorPreview Web sheets', () => {
  it('leaves the column hook to the continuous surfaces', () => {
    expect(editorPreview).not.toContain('data-web-columns');
  });
});

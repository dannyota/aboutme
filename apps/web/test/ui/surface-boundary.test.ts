import { readdirSync, readFileSync } from 'node:fs';
import { join } from 'node:path';

import { describe, expect, it } from 'vitest';

const ROOTS = [
  'app/pages',
  'app/components/editor',
  'app/components/auth',
  'app/components/settings',
  'app/components/app',
];
const EXEMPT = new Set([
  'app/pages/_harness/render.vue',
  'app/components/editor/richtext/RichTextEditor.vue', // ProseMirror root
]);
const RAW = /<(button|input|select|textarea)\b/g;
const DIALOG = /role=(["'])(?:dialog|alertdialog)\1/g;

function vueFiles(dir: string): string[] {
  return readdirSync(dir, { withFileTypes: true }).flatMap((entry) =>
    entry.isDirectory()
      ? vueFiles(join(dir, entry.name))
      : entry.name.endsWith('.vue')
        ? [join(dir, entry.name)]
        : [],
  );
}

describe('surfaces compose the shared layers (AC-UI-002)', () => {
  const files = ROOTS.flatMap(vueFiles).filter((file) => !EXEMPT.has(file));

  it.each(files)('%s renders no raw control', (file) => {
    const source = readFileSync(file, 'utf8');
    const template = source.slice(source.indexOf('<template>'));
    expect(template.match(RAW) ?? [], file).toEqual([]);
  });

  it.each(files)('%s hand-writes no dialog', (file) => {
    const source = readFileSync(file, 'utf8');
    expect(source.match(DIALOG) ?? [], file).toEqual([]);
  });

  it('recognizes both quote styles on hand-written dialogs', () => {
    expect('<div role="dialog">'.match(DIALOG)).toHaveLength(1);
    expect(`<div role='alertdialog'>`.match(DIALOG)).toHaveLength(1);
  });
});

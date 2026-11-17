// @vitest-environment node
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join } from 'node:path';
import { describe, expect, it } from 'vitest';

const WORKSPACE = [
  'app/pages/app/settings',
  'app/components/settings',
  'app/components/auth/PasswordSettings.vue',
  'app/pages/app/views',
  'app/components/views',
  'app/pages/app/projects',
  'app/components/projects',
  'app/components/editor/forms/entries/ProjectLinkNote.vue',
  'app/pages/app/import',
  'app/components/import',
];

const RETIRED = new RegExp([
  'brand-(?:blue|deep-blue|indigo|jade|ochre)',
  'surface-(?:blue|indigo|sand)',
  'radius-feature',
  'shadow-(?:product|primary|cta)',
  'gradient-(?:brand|aurora|hero-glow)',
].join('|'), 'u');

function sources(path: string): string[] {
  if (statSync(join(process.cwd(), path)).isFile()) return [path];
  return readdirSync(join(process.cwd(), path))
    .flatMap((name) => sources(join(path, name)));
}

const files = WORKSPACE.flatMap(sources);

function read(path: string): string {
  return readFileSync(join(process.cwd(), path), 'utf8');
}

describe('signed-in workspace tokens', () => {
  it('reads every workspace source', () => {
    expect(files).toEqual(expect.arrayContaining([
      'app/pages/app/settings/sessions.vue',
      'app/components/views/ViewsResumeCard.vue',
      'app/components/projects/ProjectList.vue',
      'app/components/import/ImportPick.vue',
      'app/pages/app/import/linkedin.vue',
    ]));
  });

  it('names no retired token', () => {
    for (const path of files) {
      expect(read(path), path).not.toMatch(RETIRED);
    }
  });

  it('takes colors from tokens, never hex', () => {
    for (const path of files) {
      expect(read(path), path).not.toMatch(/#[\da-f]{3,8}\b/iu);
    }
  });

  it('never sets text in the main fill', () => {
    for (const path of files) {
      expect(read(path), path)
        .not.toMatch(/\btext-(?:primary|brand)(?![\w-])/u);
    }
  });
});

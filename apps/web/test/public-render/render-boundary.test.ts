// @vitest-environment node

import { existsSync, readdirSync, readFileSync, statSync } from 'node:fs';
import { dirname, relative, resolve } from 'node:path';
import { describe, expect, it } from 'vitest';

const webRoot = resolve(import.meta.dirname, '../..');
const ENTRY_ROOTS = [
  'server/workers/public-render',
  'server/utils/public-render',
];
const FORBIDDEN = [
  'app/components/resume/PagedResume.vue',
  'app/components/resume/measure.ts',
  'app/components/resume/paginate.ts',
  'app/utils/fontsReady.ts',
];
const IMPORT = new RegExp(
  'import\\s+(?:type\\s+)?[^\'"]*?from\\s*[\'"]([^\'"]+)[\'"]'
  + '|export\\s+[^\'"]*?from\\s*[\'"]([^\'"]+)[\'"]'
  + '|import\\s*[\'"]([^\'"]+)[\'"]'
  + '|import\\(\\s*[\'"]([^\'"]+)[\'"]\\s*\\)',
  'gu',
);

function sources(directory: string): string[] {
  return readdirSync(directory).flatMap((name) => {
    const path = resolve(directory, name);
    if (statSync(path).isDirectory()) return sources(path);
    return /\.(?:ts|vue)$/u.test(path) ? [path] : [];
  });
}

function resolveImport(from: string, specifier: string): string | undefined {
  if (!specifier.startsWith('.')) return undefined;
  const base = resolve(dirname(from), specifier);
  return [base, `${base}.ts`, `${base}.vue`, `${base}/index.ts`]
    .find((path) => existsSync(path) && statSync(path).isFile());
}

function reachable(): Map<string, string | undefined> {
  const entries = ENTRY_ROOTS.flatMap((root) =>
    sources(resolve(webRoot, root)));
  const seen = new Map<string, string | undefined>(
    entries.map((entry) => [entry, undefined]),
  );
  const queue = [...entries];
  for (let file = queue.shift(); file !== undefined; file = queue.shift()) {
    for (const match of readFileSync(file, 'utf8').matchAll(IMPORT)) {
      const target = resolveImport(file, match.slice(1).find(Boolean) ?? '');
      if (target === undefined || seen.has(target)) continue;
      seen.set(target, file);
      queue.push(target);
    }
  }
  return seen;
}

describe('public render worker boundary', () => {
  it('walks the public render sources and the shared renderer', () => {
    const graph = [...reachable().keys()]
      .map((path) => relative(webRoot, path));
    expect(graph).toContain('server/workers/public-render/render.ts');
    expect(graph).toContain('app/components/public/PublicResumeApp.vue');
    expect(graph).toContain('app/components/resume/pageMetrics.ts');
  });

  it('never reaches the browser pagination modules', () => {
    const graph = reachable();
    const reached = FORBIDDEN.flatMap((path) => {
      const absolute = resolve(webRoot, path);
      const from = graph.get(absolute);
      const via = from === undefined ? 'entry' : relative(webRoot, from);
      return graph.has(absolute) ? [`${path} <- ${via}`] : [];
    });
    expect(reached).toEqual([]);
  });
});

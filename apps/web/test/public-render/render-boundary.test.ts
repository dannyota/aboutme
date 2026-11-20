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

type Source = (path: string) => string | undefined;

const diskSource: Source = (path) =>
  existsSync(path) && statSync(path).isFile()
    ? readFileSync(path, 'utf8')
    : undefined;

const ALIASES: readonly (readonly [string, string])[] = [
  ['@@/', webRoot],
  ['~~/', webRoot],
  ['@/', resolve(webRoot, 'app')],
  ['~/', resolve(webRoot, 'app')],
];

function specifierBase(from: string, specifier: string): string | undefined {
  if (specifier.startsWith('.')) return resolve(dirname(from), specifier);
  const alias = ALIASES.find(([prefix]) => specifier.startsWith(prefix));
  return alias === undefined
    ? undefined
    : resolve(alias[1], specifier.slice(alias[0].length));
}

function resolveImport(
  from: string,
  specifier: string,
  source: Source,
): string | undefined {
  const base = specifierBase(from, specifier);
  if (base === undefined) return undefined;
  return [base, `${base}.ts`, `${base}.vue`, `${base}/index.ts`]
    .find((path) => source(path) !== undefined);
}

interface Graph {
  readonly reached: Map<string, string | undefined>;
  readonly unresolved: string[];
}

function walk(entries: string[], source: Source): Graph {
  const reached = new Map<string, string | undefined>(
    entries.map((entry) => [entry, undefined]),
  );
  const unresolved: string[] = [];
  const queue = [...entries];
  for (let file = queue.shift(); file !== undefined; file = queue.shift()) {
    for (const match of (source(file) ?? '').matchAll(IMPORT)) {
      const specifier = match.slice(1).find(Boolean) ?? '';
      const target = resolveImport(file, specifier, source);
      if (target === undefined) {
        if (specifierBase(file, specifier) !== undefined) {
          unresolved.push(`${relative(webRoot, file)}: ${specifier}`);
        }
        continue;
      }
      if (reached.has(target)) continue;
      reached.set(target, file);
      queue.push(target);
    }
  }
  return { reached, unresolved };
}

const reachable = (): Graph => walk(
  ENTRY_ROOTS.flatMap((root) => sources(resolve(webRoot, root))),
  diskSource,
);

describe('public render worker boundary', () => {
  it('walks the public render sources and the shared renderer', () => {
    const graph = [...reachable().reached.keys()]
      .map((path) => relative(webRoot, path));
    expect(graph).toContain('server/workers/public-render/render.ts');
    expect(graph).toContain('app/components/public/PublicResumeApp.vue');
    expect(graph).toContain('app/components/resume/pageMetrics.ts');
  });

  it('resolves every local and alias import it follows', () => {
    expect(reachable().unresolved).toEqual([]);
  });

  it('reports a local or alias import that resolves to no file', () => {
    const entry = resolve(webRoot, 'server/workers/public-render/a.ts');
    const files = new Map([[entry, [
      'import a from \'~/missing.ts\';',
      'import b from \'../nowhere\';',
      'import c from \'vue\';',
    ].join('\n')]]);
    expect(walk([entry], (path) => files.get(path)).unresolved).toEqual([
      'server/workers/public-render/a.ts: ~/missing.ts',
      'server/workers/public-render/a.ts: ../nowhere',
    ]);
  });

  it('never reaches the browser pagination modules', () => {
    const graph = reachable().reached;
    const reached = FORBIDDEN.flatMap((path) => {
      const absolute = resolve(webRoot, path);
      const from = graph.get(absolute);
      const via = from === undefined ? 'entry' : relative(webRoot, from);
      return graph.has(absolute) ? [`${path} <- ${via}`] : [];
    });
    expect(reached).toEqual([]);
  });

  it('follows alias imports into the pagination modules', () => {
    const files = new Map([
      [resolve(webRoot, 'server/workers/public-render/a.ts'),
        'import Doc from \'@/components/resume/ResumeDocument.vue\';'],
      [resolve(webRoot, 'app/components/resume/ResumeDocument.vue'),
        'import Paged from \'./PagedResume.vue\';'],
      [resolve(webRoot, 'app/components/resume/PagedResume.vue'), ''],
    ]);
    const graph = walk(
      [resolve(webRoot, 'server/workers/public-render/a.ts')],
      (path) => files.get(path),
    );
    expect([...graph.reached.keys()].map((path) => relative(webRoot, path)))
      .toContain('app/components/resume/PagedResume.vue');
  });
});

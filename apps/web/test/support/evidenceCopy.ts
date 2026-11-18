import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';

const DESIGN = resolve(
  process.cwd(), '../../docs/design/github-evidence-picker.md',
);

function copyRows(): (readonly [string, string, string])[] {
  const lines = readFileSync(DESIGN, 'utf8').split('\n');
  const start = lines.indexOf('## Copy');
  if (start === -1) throw new Error(`No Copy section in ${DESIGN}`);
  const rows: (readonly [string, string, string])[] = [];
  for (const line of lines.slice(start + 1)) {
    if (line.startsWith('## ')) break;
    if (!line.startsWith('|')) continue;
    const cells = line.trim().slice(1, -1).split('|')
      .map((cell) => cell.trim());
    const [key, vi, en] = cells;
    if (
      cells.length !== 3
      || key === undefined || vi === undefined || en === undefined
      || key === 'Key'
      || /^-+$/u.test(key)
    ) continue;
    rows.push([key, vi, en]);
  }
  if (rows.length === 0) throw new Error(`Empty Copy table in ${DESIGN}`);
  return rows;
}

export const DESIGN_COPY: readonly (readonly [string, string, string])[]
  = copyRows();

export function designCopy(): Map<string, readonly [string, string]> {
  return new Map(DESIGN_COPY.map(([key, vi, en]) => [key, [vi, en]]));
}

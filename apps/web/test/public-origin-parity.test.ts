// @vitest-environment node

import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe, expect, it } from 'vitest';

import { decodePrintEnvelope } from '../server/utils/print/envelope';
import {
  normalizedOrigin,
} from '../server/utils/public-render/envelope-fields';

type OriginCase = { origin: string; environment: string; accept: boolean };
type Envelope = Record<string, unknown>;

const table = JSON.parse(readFileSync(
  resolve(
    process.cwd(),
    '../server/internal/publicresume/testdata/public-origins.json',
  ),
  'utf8',
)) as { cases: OriginCase[] };

const corpus = JSON.parse(readFileSync(
  resolve(
    process.cwd(),
    '../server/internal/printsnapshot/testdata/print-envelopes.json',
  ),
  'utf8',
)) as { cases: { name: string; envelope: Envelope }[] };

const linkedTo = (origin: string): Envelope => {
  const envelope = structuredClone(
    corpus.cases.find(
      (entry) => entry.name === 'en-two-linked-entries',
    )!.envelope,
  );
  const links = envelope.projectLinks as Record<string, string>;
  for (const [entry, page] of Object.entries(links)) {
    links[entry] = origin + new URL(page).pathname;
  }
  return envelope;
};

const accepted = table.cases.filter((entry) => entry.accept);

describe('public origin parity with the server', () => {
  it('reads a table with accepted and refused origins', () => {
    expect(accepted.length).toBeGreaterThan(0);
    expect(table.cases.length).toBeGreaterThan(accepted.length);
  });

  it.each(accepted.map((entry) => [entry.origin, entry] as const))(
    'the render decoder accepts %s unchanged',
    (_name, entry) => {
      expect(normalizedOrigin(entry.origin)).toBe(true);
    },
  );

  it.each(accepted.map((entry) => [entry.origin, entry] as const))(
    'the print decoder accepts project pages on %s',
    (_name, entry) => {
      expect(() => decodePrintEnvelope(JSON.stringify(linkedTo(entry.origin))))
        .not.toThrow();
    },
  );
});

// @vitest-environment node

import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe, expect, it } from 'vitest';

import {
  decodePublicRenderEnvelope,
} from '../../server/utils/public-render/envelope';

// The envelopes Go's project page builder makes for a fixed set of projects
// (apps/server/internal/publicapi/project_envelope_golden_test.go). Each must
// pass the renderer's decoder unchanged, its preview included, so a builder
// change the renderer would refuse fails here.
const golden = JSON.parse(readFileSync(
  resolve(
    process.cwd(),
    '../server/internal/publicapi/testdata/project-envelopes.json',
  ),
  'utf8',
)) as { cases: { name: string; envelope: Record<string, unknown> }[] };

describe('project envelopes Go builds', () => {
  it('holds both page kinds', () => {
    const modes = new Set(golden.cases.map((entry) => entry.envelope.mode));
    expect([...modes].sort()).toEqual(['project', 'project-list']);
  });

  it.each(golden.cases.map((entry) => [entry.name, entry.envelope] as const))(
    'decodes %s with its preview as given',
    (_name, envelope) => {
      const decoded = decodePublicRenderEnvelope(
        JSON.stringify(envelope),
      ) as unknown as Record<string, unknown>;
      expect(decoded.preview).toEqual(envelope.preview);
      expect(decoded).toEqual(envelope);
    },
  );
});

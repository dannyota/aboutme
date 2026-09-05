import { readFileSync } from 'node:fs';

import type { PrintEnvelope } from '../../server/utils/print/envelope';

export const RESUME_ID = 'a0000000-0000-4000-8000-000000000001';
export const JOB_ID = 'b0000000-0000-4000-8000-000000000002';
export const CAPABILITY = 'A'.repeat(43);

export const printEnvelope = (): PrintEnvelope => ({
  version: 1,
  resumeId: RESUME_ID,
  revision: '7',
  publicGeneration: null,
  lng: 'en',
  document: JSON.parse(readFileSync(
    new URL(
      '../../../../packages/schema/fixtures/minimal.json',
      import.meta.url,
    ),
    'utf8',
  )),
});

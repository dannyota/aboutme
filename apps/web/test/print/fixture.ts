import { readFileSync } from 'node:fs';

import type {
  PrintCardEnvelope,
  PrintEnvelope,
} from '../../server/utils/print/envelope';

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

/** A valid link-preview card envelope, as Go's printsnapshot emits it. */
export const cardEnvelope = (): PrintCardEnvelope => ({
  version: 1,
  kind: 'card',
  resumeId: RESUME_ID,
  card: {
    layoutVersion: 1,
    lng: 'vi',
    slug: 'nguyen-an',
    name: 'Nguyễn Văn An',
    headline: 'Kỹ sư phần mềm',
    photo: {
      url: 'data:image/png;base64,AA==',
      crop: { height: 0.5, width: 0.5, x: 0.25, y: 0 },
    },
    accent: '#1d4ed8',
  },
});

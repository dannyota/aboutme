import { readFileSync } from 'node:fs';

import { CURRENT_VERSION } from '@aboutme/schema/released';

import { parseCurrentDocument } from '../../app/editor/documentValidation';
import { parseRevision } from '../../app/editor/revision';
import type { AcceptedResume, ResumeMetadata } from '../../app/editor/types';

const minimalFixture = JSON.parse(
  readFileSync('../../packages/schema/fixtures/minimal.json', 'utf8'),
) as unknown;

const fixedMetadata: ResumeMetadata = {
  id: 'resume-1',
  title: 'Fixture',
  lng: 'en',
  live: false,
  downloadEnabled: false,
  seoGeoEnabled: false,
  slug: null,
  schemaVersion: CURRENT_VERSION,
  createdAt: '2026-01-01T00:00:00Z',
  updatedAt: '2026-01-01T00:00:00Z',
};

export function acceptedFixture(
  overrides: Partial<AcceptedResume> = {},
): AcceptedResume {
  return {
    document: structuredClone(parseCurrentDocument(minimalFixture)),
    metadata: structuredClone(fixedMetadata),
    revision: parseRevision('1'),
    metadataFreshness: 'complete',
    ...overrides,
  };
}

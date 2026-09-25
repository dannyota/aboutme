// @vitest-environment node

import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';

import {
  generateSource,
  outputPath,
  schemaPath,
} from '../../scripts/generate-document-validator.mjs';

// app/editor/documentValidator.generated.mjs must always be an exact Ajv
// standalone compile of packages/schema/resume.schema.json: a schema change
// that outruns regeneration would silently reject or accept the wrong
// documents in the editor. Run `npm run document-validator:gen` (apps/web)
// after any resume.schema.json change to refresh it.
describe('document validator generation', () => {
  it('matches the committed generated validator', () => {
    const schema = JSON.parse(readFileSync(schemaPath, 'utf8'));
    expect(readFileSync(outputPath, 'utf8')).toBe(generateSource(schema));
  });
});

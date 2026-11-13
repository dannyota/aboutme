// @vitest-environment node

import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';

import {
  generateSource,
  outputPath,
  schemaPath,
} from '../../scripts/generate-document-validator.mjs';

describe('document validator generation', () => {
  it('matches the committed generated validator', () => {
    const schema = JSON.parse(readFileSync(schemaPath, 'utf8'));
    expect(readFileSync(outputPath, 'utf8')).toBe(generateSource(schema));
  });
});

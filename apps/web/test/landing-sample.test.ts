import Ajv2020 from 'ajv/dist/2020.js';
import addFormats from 'ajv-formats';
import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';
import type { Resume } from '@aboutme/schema';
import currentSchema from '@aboutme/schema/current-schema';
import { validateDocument } from '@aboutme/schema/validation';

import { sampleContext } from '../app/landing/sampleContext';
import { sampleLink, sampleResume } from '../app/landing/sampleResume';
import {
  resolveRenderModel,
} from '../app/components/resume/resolveRenderModel';

const ajv = addFormats(new Ajv2020({ allErrors: true, strict: true }));
const validate = ajv.compile(currentSchema);

describe('landing sample resume', () => {
  it('is a schema-valid, photo-less compiled-in document', () => {
    expect(validate(sampleResume), ajv.errorsText(validate.errors)).toBe(true);
    expect(validateDocument(sampleResume)).toEqual([]);
    expect(sampleResume.schemaVersion).toBe(2);
    expect(sampleResume.personalDetails.photo).toBeUndefined();
    expect(sampleResume.personalDetails.fullName).toBe('Ada Lovelace');
    expect(sampleLink).toBe('/ada-lovelace');
  });

  it('resolves through the renderer with the landing context', () => {
    expect(() => resolveRenderModel(sampleResume, sampleContext)).not.toThrow();
  });

  it('is kept as a literal fixture copy with only the photo removed', () => {
    const fixture = JSON.parse(
      readFileSync('../../packages/schema/fixtures/full.json', 'utf8'),
    ) as Resume;
    delete fixture.personalDetails.photo;
    expect(sampleResume).toEqual(fixture);
  });
});

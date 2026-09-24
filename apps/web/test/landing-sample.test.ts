import Ajv2020 from 'ajv/dist/2020.js';
import addFormats from 'ajv-formats';
import { existsSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe, expect, it } from 'vitest';
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
  it('is a schema-valid compiled-in document for the owner', () => {
    expect(validate(sampleResume), ajv.errorsText(validate.errors)).toBe(true);
    expect(validateDocument(sampleResume)).toEqual([]);
    expect(sampleResume.schemaVersion).toBe(4);
    expect(sampleResume.personalDetails.fullName).toBe('Danny');
    expect(sampleLink).toBe('/danny');
  });

  it('resolves through the renderer with the landing context', () => {
    expect(() => resolveRenderModel(sampleResume, sampleContext)).not.toThrow();
  });

  it('points its photo key at a file under the web public root', () => {
    const key = sampleResume.personalDetails.photo?.key;
    expect(key).toBeDefined();
    const publicPath = resolve(__dirname, '../public', key!);
    expect(existsSync(publicPath)).toBe(true);
  });

  it('names no real employer, only a role description', () => {
    const work = sampleResume.content.work;
    if (work?.sectionType !== 'work') {
      throw new Error('sample has no work section');
    }
    expect(work.entries.length).toBeGreaterThan(0);
    for (const entry of work.entries) {
      expect(entry.employer).toMatch(/^A /);
    }
  });
});

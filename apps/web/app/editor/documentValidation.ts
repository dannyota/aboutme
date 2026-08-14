import type { Resume } from '@aboutme/schema';
import currentSchema from '@aboutme/schema/current-schema';
import { CURRENT_VERSION } from '@aboutme/schema/released';
import { validateDocument } from '@aboutme/schema/validation';
import Ajv2020 from 'ajv/dist/2020.js';
import addFormats from 'ajv-formats';

const ajv = new Ajv2020({ allErrors: true, strict: true });
addFormats(ajv);
const validateSchema = ajv.compile(currentSchema);

export function parseCurrentDocument(value: unknown): Resume {
  if (
    !validateSchema(value)
    || !isCurrentVersion(value)
    || validateDocument(value as never).length !== 0
  ) {
    throw new Error('invalid current document');
  }
  return value as Resume;
}

function isCurrentVersion(value: unknown): value is { schemaVersion: number } {
  return (
    typeof value === 'object'
    && value !== null
    && 'schemaVersion' in value
    && value.schemaVersion === CURRENT_VERSION
  );
}

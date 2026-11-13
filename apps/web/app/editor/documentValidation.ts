import type { Resume } from '@aboutme/schema';
import { CURRENT_VERSION } from '@aboutme/schema/released';
import { validateDocument } from '@aboutme/schema/validation';

import validateSchema from './documentValidator.generated.mjs';

export class UnknownDocumentVersionError extends Error {
  constructor() {
    super('invalid current document');
  }
}

export function parseCurrentDocument(value: unknown): Resume {
  if (
    typeof value === 'object'
    && value !== null
    && 'schemaVersion' in value
    && typeof value.schemaVersion === 'number'
    && Number.isInteger(value.schemaVersion)
    && value.schemaVersion !== CURRENT_VERSION
  ) {
    throw new UnknownDocumentVersionError();
  }
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

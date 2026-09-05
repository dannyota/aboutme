import { compareRevision, parseRevision } from '../editor/revision';
import type { Revision } from '../editor/types';
import type { RevisionDecision } from './controller';

export function ownerRevisionDecision(
  value: unknown,
  resumeId: string,
  currentRevision: Revision | undefined,
): RevisionDecision {
  if (typeof value !== 'object' || value === null) return 'unknown';
  const event = value as Record<string, unknown>;
  if (
    typeof event.resume_id !== 'string'
    || typeof event.revision !== 'string'
    || typeof event.deleted !== 'boolean'
  ) { return 'unknown'; }
  if (event.resume_id !== resumeId) return 'ignore';
  let revision;
  try {
    revision = parseRevision(event.revision);
  } catch {
    return 'unknown';
  }
  if (event.deleted) return 'accept';
  return currentRevision !== undefined
    && compareRevision(revision, currentRevision) <= 0
    ? 'ignore'
    : 'accept';
}

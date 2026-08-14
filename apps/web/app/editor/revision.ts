import type { ParentETag, Revision } from './types';

const DECIMAL = /^[1-9][0-9]*$/;
const MAX_REVISION = '9223372036854775807';
const PARENT_ETAG = /^"r([1-9][0-9]*)"$/;

export function parseRevision(value: unknown): Revision {
  if (typeof value !== 'string' || !DECIMAL.test(value)) {
    throw new Error('invalid revision');
  }
  if (
    value.length > MAX_REVISION.length
    || (value.length === MAX_REVISION.length && value > MAX_REVISION)
  ) {
    throw new Error('revision out of range');
  }
  return value as Revision;
}

export function parseParentETag(value: string | null): ParentETag {
  const match = value?.match(PARENT_ETAG);
  if (match === undefined || match === null) {
    throw new Error('invalid parent ETag');
  }
  parseRevision(match[1]);
  return value as ParentETag;
}

export function parentETag(revision: Revision): ParentETag {
  return `"r${revision}"` as ParentETag;
}

export function compareRevision(left: Revision, right: Revision): -1 | 0 | 1 {
  if (left.length !== right.length) return left.length < right.length ? -1 : 1;
  if (left === right) return 0;
  return left < right ? -1 : 1;
}

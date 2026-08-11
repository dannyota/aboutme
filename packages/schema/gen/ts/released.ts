// Code generated from released-versions.json. DO NOT EDIT.

/**
 * One released document-shape version: its immutable schema file and the
 * retained generated types derived from that file. Paths are relative to
 * packages/schema. Released entries are append-only (design spec §3,
 * "Wire-version compatibility").
 */
export interface ReleasedSchema {
  readonly version: number;
  readonly schema: string;
  readonly goPackage: string;
  readonly tsTypes: string;
}

/**
 * The document-shape version resume.schema.json currently describes.
 */
export const CURRENT_VERSION = 1;

/**
 * Every released version, ascending. Frozen: the registry is a contract, not
 * a mutable cache.
 */
export const RELEASED_SCHEMAS: readonly ReleasedSchema[] = Object.freeze([
  Object.freeze({
    version: 1,
    schema: "resume.v1.schema.json",
    goPackage: "gen/go/v1",
    tsTypes: "gen/ts/v1/resume.ts",
  }),
]);

/**
 * Reports whether `version` has been released. Non-integer, negative, and
 * NaN inputs are simply not released, so callers need no separate guard.
 */
export function isReleasedVersion(version: number): boolean {
  return RELEASED_SCHEMAS.some((released) => released.version === version);
}

/**
 * Resolves a released version. Fails closed: an unreleased version throws
 * rather than falling back to the current or nearest one, because a document
 * claiming an unreleased version cannot be validated or converted at all.
 * The result is a copy, so a caller cannot retarget the registry.
 */
export function releasedSchema(version: number): ReleasedSchema {
  const released = RELEASED_SCHEMAS.find((candidate) => candidate.version === version);
  if (released === undefined) {
    throw new Error(`schema: unknown released schema version ${version}`);
  }
  return { ...released };
}

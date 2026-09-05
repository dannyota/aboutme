import { mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

import Ajv from 'ajv';
import addFormats from 'ajv-formats';
import standaloneCode from 'ajv/dist/standalone/index.js';
import { parse } from 'yaml';

type Schema = Record<string, unknown>;

const ucs2LengthRequire
  = /const (\w+) = require\("ajv\/dist\/runtime\/ucs2length"\)\.default;/u;
const uriFormatRequire
  = /const (\w+) = require\("ajv-formats\/dist\/formats"\)\.fullFormats\.uri;/u;
const openapiPath = fileURLToPath(
  new URL('../../../../../docs/api/openapi.yaml', import.meta.url),
);

const nativeESM = (source: string): string => {
  if (!ucs2LengthRequire.test(source) || !uriFormatRequire.test(source)) {
    throw new Error('PublicResume validator runtime helpers changed');
  }
  const output = source
    .replace(
      ucs2LengthRequire,
      'const $1 = ucs2LengthRuntime;',
    )
    .replace(uriFormatRequire, 'const $1 = formatsRuntime.fullFormats.uri;');
  if (/\brequire\(/u.test(output)) {
    throw new Error(
      'PublicResume validator contains an unsupported CommonJS helper',
    );
  }
  return [
    'import ucs2LengthModule from \'ajv/dist/runtime/ucs2length.js\';',
    'import formatsModule from \'ajv-formats/dist/formats.js\';',
    'const ucs2LengthRuntime = typeof ucs2LengthModule === \'function\'',
    '  ? ucs2LengthModule : ucs2LengthModule.default;',
    'const formatsRuntime = \'fullFormats\' in formatsModule',
    '  ? formatsModule : formatsModule.default;',
    output,
  ].join('\n');
};

const refName = (reference: string): string => {
  const prefix = '#/components/schemas/';
  if (!reference.startsWith(prefix)) {
    throw new Error(
      `PublicResume schema has an external reference: ${reference}`,
    );
  }
  return reference.slice(prefix.length);
};

const refsIn = (value: unknown, found: Set<string>): void => {
  if (Array.isArray(value)) {
    value.forEach((item) => refsIn(item, found));
    return;
  }
  if (value === null || typeof value !== 'object') return;
  for (const [key, item] of Object.entries(value)) {
    if (key === '$ref' && typeof item === 'string') found.add(refName(item));
    else refsIn(item, found);
  }
};

const rewriteRefs = (value: unknown): unknown => {
  if (Array.isArray(value)) return value.map(rewriteRefs);
  if (value === null || typeof value !== 'object') return value;
  return Object.fromEntries(
    Object.entries(value).map(([key, item]) => [
      key,
      key === '$ref' && typeof item === 'string'
        ? `#/$defs/${refName(item)}`
        : rewriteRefs(item),
    ]),
  );
};

/**
 * Reads the OpenAPI source and writes an Ajv standalone validator containing
 * only PublicResume and every schema it reaches.  The generated module is an
 * artifact: no hand-written mirror of the public DTO is used at runtime.
 */
export function buildPublicResumeValidator(buildDir: string): string {
  return buildValidator(buildDir, 'PublicResume', 'public-resume');
}

/** Builds the private print document validator from the same OpenAPI source. */
export function buildPrintDocumentValidator(buildDir: string): string {
  return buildValidator(buildDir, 'PublicResumeDocument', 'print-document');
}

function buildValidator(
  buildDir: string,
  rootName: string,
  outputName: string,
): string {
  const source = parse(readFileSync(openapiPath, 'utf8')) as {
    components?: { schemas?: Record<string, Schema> };
  };
  const schemas = source.components?.schemas;
  if (schemas === undefined) {
    throw new Error('OpenAPI components.schemas is absent');
  }
  const pending = [rootName];
  const closure = new Set<string>();
  while (pending.length > 0) {
    const name = pending.pop()!;
    if (closure.has(name)) continue;
    const schema = schemas[name];
    if (schema === undefined) {
      throw new Error(`OpenAPI schema is absent: ${name}`);
    }
    closure.add(name);
    const references = new Set<string>();
    refsIn(schema, references);
    for (const reference of references) pending.push(reference);
  }
  const definitions = Object.fromEntries(
    [...closure].sort().map((name) => [name, rewriteRefs(schemas[name]!)]),
  );
  if (rootName === 'PublicResumeDocument') {
    definitions.PublicContent = {
      ...definitions.PublicContent as Schema,
      minProperties: 0,
    };
  }
  const schema: Schema = {
    $id: `aboutme-${outputName}`,
    $ref: `#/$defs/${rootName}`,
    $defs: definitions,
  };
  const ajv = new Ajv({
    allErrors: false,
    strict: false,
    code: { esm: true, source: true },
  });
  addFormats(ajv);
  const validate = ajv.compile(schema);
  const output = resolve(buildDir, `${outputName}-validator.mjs`);
  mkdirSync(buildDir, { recursive: true });
  writeFileSync(output, nativeESM(standaloneCode(ajv, validate)));
  writeFileSync(
    resolve(buildDir, `${outputName}-validator.d.ts`),
    [
      'declare const validate: (value: unknown) => boolean;',
      'export default validate;',
      '',
    ].join('\n'),
  );
  return output;
}

export const publicRenderWorkerOutput = (nitroOutputDir: string): string =>
  resolve(nitroOutputDir, 'server/workers/public-render.mjs');

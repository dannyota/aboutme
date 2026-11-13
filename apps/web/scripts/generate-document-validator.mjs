#!/usr/bin/env node
import { fileURLToPath } from 'node:url';
import { readFileSync, writeFileSync } from 'node:fs';
import { resolve } from 'node:path';

import Ajv2020 from 'ajv/dist/2020.js';
import addFormats from 'ajv-formats';
import standaloneCode from 'ajv/dist/standalone/index.js';

const here = fileURLToPath(new URL('.', import.meta.url));
export const schemaPath
  = resolve(here, '../../../packages/schema/resume.schema.json');
export const outputPath
  = resolve(here, '../app/editor/documentValidator.generated.mjs');

// Ajv standalone ESM still emits require() for helpers (ajv-validator/ajv#889).
const runtimeHelperRequire
  = /const (\w+) = require\("ajv\/dist\/runtime\/([a-zA-Z0-9]+)"\)\.default;/gu;
const formatHelperRequire = new RegExp(
  'const (\\w+) = require\\("ajv-formats/dist/formats"\\)'
  + '\\.fullFormats\\.(\\w+);',
  'gu',
);

function toEsm(code) {
  const imports = [];
  const consts = [];
  let rewritten = code.replace(
    runtimeHelperRequire,
    (_match, varName, helperName) => {
      const moduleVar = `${helperName}Module`;
      const importLine
        = `import ${moduleVar} from 'ajv/dist/runtime/${helperName}.js';`;
      if (!imports.includes(importLine)) imports.push(importLine);
      consts.push(
        `const ${varName} = typeof ${moduleVar} === 'function' `
        + `? ${moduleVar} : ${moduleVar}.default;`,
      );
      return '';
    },
  );

  let formatsImported = false;
  rewritten = rewritten.replace(
    formatHelperRequire,
    (_match, varName, formatName) => {
      if (!formatsImported) {
        imports.push(
          'import formatsModule from \'ajv-formats/dist/formats.js\';',
        );
        consts.push(
          'const formatsRuntime = \'fullFormats\' in formatsModule '
          + '? formatsModule : formatsModule.default;',
        );
        formatsImported = true;
      }
      return `const ${varName} = formatsRuntime.fullFormats.${formatName};`;
    },
  );

  if (/\brequire\(/u.test(rewritten)) {
    throw new Error('generated validator still contains a require()');
  }
  return [
    '// Code generated from resume.schema.json. DO NOT EDIT.',
    '/* eslint-disable */',
    ...imports,
    ...consts,
    rewritten,
    '',
  ].join('\n');
}

export function generateSource(schema) {
  const ajv = new Ajv2020({
    allErrors: true,
    strict: true,
    code: { esm: true, source: true },
  });
  addFormats(ajv);
  const validate = ajv.compile(schema);
  return toEsm(standaloneCode(ajv, validate));
}

function isMain() {
  return process.argv[1] === fileURLToPath(import.meta.url);
}

if (isMain()) {
  const schema = JSON.parse(readFileSync(schemaPath, 'utf8'));
  writeFileSync(outputPath, generateSource(schema));
  console.log(`generate-document-validator: wrote ${outputPath}`);
}

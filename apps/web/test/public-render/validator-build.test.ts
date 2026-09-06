// @vitest-environment node

import { execFileSync } from 'node:child_process';
import { mkdtempSync, readFileSync, rmSync } from 'node:fs';
import { join } from 'node:path';
import { pathToFileURL } from 'node:url';
import { afterEach, describe, expect, it } from 'vitest';

import {
  buildPublicResumeValidator,
} from '../../server/utils/public-render/worker-build';
import * as validatorBuild from '../../server/utils/public-render/worker-build';

const temporaryDirectories: string[] = [];

afterEach(() => {
  for (const directory of temporaryDirectories.splice(0)) {
    rmSync(directory, { recursive: true, force: true });
  }
});

describe('public resume validator build', () => {
  it('preserves cleared optional links in public and print documents', () => {
    const directory = mkdtempSync(
      join(process.cwd(), '.nuxt/link-validator-test-'),
    );
    temporaryDirectories.push(directory);
    const result = execFileSync(process.execPath, [
      '--input-type=module', '--eval',
      'import { readFileSync } from "node:fs"; '
      + 'const printValidate = (await import(process.argv[1])).default; '
      + 'const publicValidate = (await import(process.argv[2])).default; '
      + 'const document = JSON.parse(readFileSync(process.argv[3], "utf8")); '
      + 'const id = "018f5b6a-9a3e-7c21-8b1e-000000000001"; '
      + 'const entry = {id, title: "Synthetic award", titleLink: ""}; '
      + 'document.personalDetails = {fullName: "Synthetic resume"}; '
      + 'document.content = {[id]: {sectionType: "custom", entries: [entry]}}; '
      + 'document.customization.layout.sections.main = [id]; '
      + 'const values = ["", "https://example.com", "mailto:a@example.com", '
      + '"tel:+84123456789", "javascript:alert(1)", "http://example.com", "broken"]; '
      + 'const results = values.map(value => {entry.titleLink = value; '
      + 'return [printValidate(document), publicValidate({slug: "resume", '
      + 'revision: "1", lng: "und", downloadEnabled: false, document})];}); '
      + 'process.stdout.write(JSON.stringify(results));',
      pathToFileURL(validatorBuild.buildPrintDocumentValidator(directory)).href,
      pathToFileURL(buildPublicResumeValidator(directory)).href,
      join(process.cwd(), '../../packages/schema/fixtures/minimal.json'),
    ], { encoding: 'utf8' });
    expect(JSON.parse(result)).toEqual([
      [true, true], [true, true], [true, true], [true, true],
      [false, false], [false, false], [false, false],
    ]);
  });

  it('builds a print validator that rejects private fields', () => {
    const build = Reflect.get(validatorBuild, 'buildPrintDocumentValidator');
    expect(typeof build).toBe('function');
    const directory = mkdtempSync(
      join(process.cwd(), '.nuxt/print-validator-test-'),
    );
    temporaryDirectories.push(directory);
    const output = build(directory);
    const result = execFileSync(process.execPath, [
      '--input-type=module', '--eval',
      'import { readFileSync } from "node:fs"; '
      + 'const validate = (await import(process.argv[1])).default; '
      + 'const document = JSON.parse(readFileSync(process.argv[2], "utf8")); '
      + 'document.personalDetails = { fullName: "" }; '
      + 'const valid = validate(document); '
      + 'document.personalDetails.photo = { key: "private-object-key" }; '
      + 'const privatePhoto = validate(document); '
      + 'delete document.personalDetails.photo; document.userId = "owner"; '
      + 'const privateOwner = validate(document); delete document.userId; '
      + 'const publicValidate = (await import(process.argv[3])).default; '
      + 'const publicEmpty = publicValidate({slug: "resume", revision: "1", '
      + 'lng: "und", downloadEnabled: false, document}); '
      + 'process.stdout.write(JSON.stringify('
      + '[valid, privatePhoto, privateOwner, publicEmpty]));',
      pathToFileURL(output).href,
      join(process.cwd(), '../../packages/schema/fixtures/minimal.json'),
      pathToFileURL(buildPublicResumeValidator(directory)).href,
    ], { encoding: 'utf8' });
    expect(JSON.parse(result)).toEqual([true, false, false, false]);
  });

  it('prepares Nuxt types before a development worker build', () => {
    const packageJSON = JSON.parse(
      readFileSync(join(process.cwd(), 'package.json'), 'utf8'),
    ) as { scripts?: Record<string, unknown> };

    expect(packageJSON.scripts?.dev).toBe('nuxt prepare && nuxt dev');
  });

  it('loads as native ESM without a CommonJS runtime', async () => {
    const directory = mkdtempSync(
      join(process.cwd(), '.nuxt/validator-test-'),
    );
    temporaryDirectories.push(directory);
    const output = buildPublicResumeValidator(directory);

    const result = execFileSync(
      process.execPath,
      [
        '--input-type=module',
        '--eval',
        'const validator = (await import(process.argv[1])).default; '
        + 'process.stdout.write(String(validator({})));',
        pathToFileURL(output).href,
      ],
      { encoding: 'utf8' },
    );

    expect(result).toBe('false');
  });
});

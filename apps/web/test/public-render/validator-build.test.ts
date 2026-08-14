// @vitest-environment node

import { execFileSync } from 'node:child_process';
import { mkdtempSync, readFileSync, rmSync } from 'node:fs';
import { join } from 'node:path';
import { pathToFileURL } from 'node:url';
import { afterEach, describe, expect, it } from 'vitest';

import {
  buildPublicResumeValidator,
} from '../../server/utils/public-render/worker-build';

const temporaryDirectories: string[] = [];

afterEach(() => {
  for (const directory of temporaryDirectories.splice(0)) {
    rmSync(directory, { recursive: true, force: true });
  }
});

describe('public resume validator build', () => {
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

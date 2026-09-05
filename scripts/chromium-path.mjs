import { accessSync, constants } from 'node:fs';
import { createRequire } from 'node:module';
import { isAbsolute } from 'node:path';

const require = createRequire(new URL('../apps/web/package.json', import.meta.url));
const executable = process.env.ABOUTME_CHROMIUM_PATH
  || require('playwright').chromium.executablePath();
if (!isAbsolute(executable) || /[\r\n\0]/u.test(executable)) {
  throw new Error('The pinned Chromium executable path is invalid.');
}
accessSync(executable, constants.X_OK);
process.stdout.write(`${executable}\n`);

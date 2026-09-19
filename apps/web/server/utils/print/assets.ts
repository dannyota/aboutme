import { createHash } from 'node:crypto';
import { copyFileSync, mkdirSync, readFileSync } from 'node:fs';
import { resolve } from 'node:path';

export function buildPrintAssets(
  assetsDir: string,
  fontsDir: string,
  printCSS: string,
): void {
  const sourceCSS = resolve('app/assets/css/fonts.css');
  const fontCSS = readFileSync(sourceCSS, 'utf8');
  const fonts = [...fontCSS.matchAll(
    /url\('\.\.\/fonts\/([a-z0-9-]+\.woff2)'\)/gu,
  )].map((match) => match[1]!);
  if (fonts.length === 0 || fontCSS.match(/url\(/gu)?.length !== fonts.length) {
    throw new Error('Print font asset references changed');
  }
  mkdirSync(assetsDir, { recursive: true });
  mkdirSync(fontsDir, { recursive: true });
  copyFileSync(printCSS, resolve(assetsDir, 'print.css'));
  copyFileSync(sourceCSS, resolve(assetsDir, 'print-fonts.css'));
  for (const font of new Set(fonts)) {
    copyFileSync(
      resolve('app/assets/fonts', font),
      resolve(fontsDir, font),
    );
  }
}

/**
 * Version of the resume stylesheets the public page links. The files keep
 * fixed names and are cached as immutable, so the public page appends this
 * content hash as a query to fetch fresh CSS whenever either file changes.
 */
export function publicStyleVersion(printCSS: string, fontsCSS: string): string {
  return createHash('sha256')
    .update(printCSS)
    .update('\0')
    .update(fontsCSS)
    .digest('hex')
    .slice(0, 16);
}

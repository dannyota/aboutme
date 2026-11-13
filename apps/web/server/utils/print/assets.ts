import { createHash } from 'node:crypto';
import {
  copyFileSync,
  mkdirSync,
  readFileSync,
  writeFileSync,
} from 'node:fs';
import { resolve } from 'node:path';

/**
 * Writes the public pages' fixed-name stylesheets and fonts beside the
 * hydration bundle: print.css from the print worker build, print-fonts.css,
 * and project-pages.css, the project pages' stylesheet text.
 */
export function buildPrintAssets(
  assetsDir: string,
  fontsDir: string,
  printCSS: string,
  projectCSS: string,
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
  writeFileSync(resolve(assetsDir, 'project-pages.css'), projectCSS);
  for (const font of new Set(fonts)) {
    copyFileSync(
      resolve('app/assets/fonts', font),
      resolve(fontsDir, font),
    );
  }
}

/**
 * Version of the stylesheets the public pages link: print.css and
 * print-fonts.css on the resume page, print-fonts.css and project-pages.css
 * on the project pages. The files keep fixed names and are cached as
 * immutable, so each page appends this content hash as a query to fetch
 * fresh CSS whenever any of them changes.
 */
export function publicStyleVersion(
  printCSS: string,
  fontsCSS: string,
  projectCSS: string,
): string {
  return contentVersion(printCSS, fontsCSS, projectCSS);
}

/**
 * Version of the public hydration bundle, which also keeps a fixed, immutable
 * name; the public page loads it as public-resume.mjs?v=<version>.
 */
export function publicScriptVersion(source: string): string {
  return contentVersion(source);
}

// contentVersion is the first 16 hex characters of the SHA-256 of the parts
// joined by NUL, so a byte moved across a boundary changes the version.
function contentVersion(...parts: string[]): string {
  return createHash('sha256')
    .update(parts.join('\0'))
    .digest('hex')
    .slice(0, 16);
}

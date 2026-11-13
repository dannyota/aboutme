import { createHash } from 'node:crypto';
import {
  copyFileSync,
  mkdirSync,
  readFileSync,
  writeFileSync,
} from 'node:fs';
import { resolve } from 'node:path';

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

export function publicStyleVersion(
  printCSS: string,
  fontsCSS: string,
  projectCSS: string,
): string {
  return contentVersion(printCSS, fontsCSS, projectCSS);
}

export function publicScriptVersion(source: string): string {
  return contentVersion(source);
}

function contentVersion(...parts: string[]): string {
  return createHash('sha256')
    .update(parts.join('\0'))
    .digest('hex')
    .slice(0, 16);
}

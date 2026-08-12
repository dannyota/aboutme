import catalog from '../assets/fonts/catalog.json';

export interface ResolvedFontSelection {
  readonly id: string;
  readonly cssFamily: string;
  readonly fallbackId: 'noto-sans' | 'noto-serif' | 'space-mono';
  readonly fallbackCssFamily: string;
  readonly cssStack: string;
  readonly loadDescriptors: readonly [string, string];
}

interface CatalogEntry {
  readonly id: string;
  readonly cssFamily: string;
  readonly fallback: {
    readonly id: string;
    readonly cssFamily: string;
  };
}

const entries = catalog.entries as readonly CatalogEntry[];
const entriesById = new Map(entries.map((entry) => [entry.id, entry]));

const isFallbackId = (
  id: string,
): id is ResolvedFontSelection['fallbackId'] =>
  id === 'noto-sans' || id === 'noto-serif' || id === 'space-mono';

const quoted = (family: string): string =>
  `"${family.replaceAll('"', '\\"')}"`;

export function resolveFontSelection(id: string): ResolvedFontSelection {
  const entry = entriesById.get(id);
  if (entry === undefined) {
    throw new Error(`unknown font ID: ${id}`);
  }
  if (!isFallbackId(entry.fallback.id)) {
    throw new Error(
      `unknown fallback font ID for ${entry.id}: ${entry.fallback.id}`,
    );
  }

  const selectedFamily = quoted(entry.cssFamily);
  const fallbackFamily = quoted(entry.fallback.cssFamily);
  const cssStack = entry.id === entry.fallback.id
    ? selectedFamily
    : `${selectedFamily}, ${fallbackFamily}`;

  return {
    id: entry.id,
    cssFamily: entry.cssFamily,
    fallbackId: entry.fallback.id,
    fallbackCssFamily: entry.fallback.cssFamily,
    cssStack,
    loadDescriptors: [
      `400 1em ${cssStack}`,
      `700 1em ${cssStack}`,
    ],
  };
}

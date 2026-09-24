import type { Locale } from '../i18n/locale';
import manifest from '../../public/templates/pages/manifest.json';

interface ManifestEntry {
  readonly templateId: string;
  readonly lng: 'en' | 'vi';
  readonly pages: number;
  readonly width: number;
  readonly height: number;
  readonly files: readonly string[];
}

const entries = manifest as readonly ManifestEntry[];

export interface SamplePageImage {
  readonly src: string;
  readonly width: number;
  readonly height: number;
}

/**
 * Page one of a gallery sample, from the stored images `sample-pages.spec`
 * produces (DESIGN.md, Library). Undefined when the template has no sample
 * in that language, so the card falls back to the live filler render.
 */
export function samplePageImage(
  templateId: string,
  lng: Locale,
): SamplePageImage | undefined {
  const entry = entries.find((candidate) =>
    candidate.templateId === templateId && candidate.lng === lng);
  const file = entry?.files[0];
  if (entry === undefined || file === undefined) return undefined;
  return {
    src: `/templates/pages/${file}`,
    width: entry.width,
    height: entry.height,
  };
}

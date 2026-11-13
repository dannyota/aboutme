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

export interface SampleNumberedPageImage extends SamplePageImage {
  readonly number: number;
}

export function samplePageImages(
  templateId: string,
  lng: Locale,
): readonly SampleNumberedPageImage[] {
  const entry = entries.find((candidate) =>
    candidate.templateId === templateId && candidate.lng === lng);
  if (entry === undefined) return [];
  return entry.files.map((file, index) => ({
    src: `/templates/pages/${file}`,
    width: entry.width,
    height: entry.height,
    number: index + 1,
  }));
}

export function samplePageImage(
  templateId: string,
  lng: Locale,
): SamplePageImage | undefined {
  return samplePageImages(templateId, lng)[0];
}

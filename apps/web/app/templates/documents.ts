import type { Resume } from '@aboutme/schema';
import {
  loadFiller,
  loadSample,
  type SampleLanguage,
} from '@aboutme/schema/samples';

import { applyTemplate } from '../components/resume/applyTemplate';
import type { GalleryTemplate } from './catalog';

export interface GalleryDocument {
  readonly document: Resume;
  readonly lng: SampleLanguage;
  readonly isSample: boolean;
}

export async function galleryDocument(
  template: GalleryTemplate,
  lng: SampleLanguage,
): Promise<GalleryDocument> {
  const sample = template.sampleLanguages.includes(lng)
    ? await loadSample(template.id, lng)
    : undefined;
  if (sample !== undefined) return { document: sample, lng, isSample: true };
  const filler = await loadFiller(lng);
  if (filler === undefined) throw new Error(`no filler in ${lng}`);
  return {
    document: {
      ...filler,
      customization: applyTemplate(
        filler.customization,
        template.preset,
        filler.content,
        lng,
      ),
    },
    lng,
    isSample: false,
  };
}

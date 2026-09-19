import type { Resume } from '@aboutme/schema';
import {
  FILLER_LANGUAGES,
  loadFiller,
  loadSample,
  SAMPLES,
  type SampleLanguage,
} from '@aboutme/schema/samples';

/**
 * Harness fixture ids for the gallery samples: `sample-<templateId>-<vi|en>`
 * for a template's own sample and `filler-<vi|en>` for the generic filler.
 * Unknown ids resolve to undefined.
 */
export async function loadSampleFixture(
  fixture: string,
): Promise<{ document: Resume; lng: SampleLanguage } | undefined> {
  const sample = /^sample-([a-z0-9-]+)-(vi|en)$/u.exec(fixture);
  if (sample !== null) {
    const [, templateId, lng] = sample as unknown as [
      string, string, SampleLanguage,
    ];
    if (!SAMPLES.some((ref) => ref.templateId === templateId
      && ref.lng === lng)) {
      return undefined;
    }
    const document = await loadSample(templateId, lng);
    return document === undefined ? undefined : { document, lng };
  }
  const filler = /^filler-(vi|en)$/u.exec(fixture);
  if (filler !== null) {
    const lng = filler[1] as SampleLanguage;
    if (!FILLER_LANGUAGES.includes(lng)) return undefined;
    const document = await loadFiller(lng);
    return document === undefined ? undefined : { document, lng };
  }
  return undefined;
}

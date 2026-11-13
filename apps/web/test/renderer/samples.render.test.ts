// @vitest-environment node

import type { Resume } from '@aboutme/schema';
import {
  FILLER_LANGUAGES,
  loadFiller,
  loadSample,
  SAMPLES,
} from '@aboutme/schema/samples';
import { TEMPLATES } from '@aboutme/schema/templates';
import { createSSRApp, h } from 'vue';
import { renderToString } from 'vue/server-renderer';
import { describe, expect, it } from 'vitest';

import { applyTemplate } from '../../app/components/resume/applyTemplate';
import { dateFormatForLanguage } from '../../app/components/resume/formatDate';
import { PaginationMeasureKey } from '../../app/components/resume/measure';
import ResumeDocument from '../../app/components/resume/ResumeDocument.vue';
import { syntheticMeasure } from './synthetic-measure';

const render = (
  document: Resume,
  lng: string,
  mode: 'paged' | 'continuous',
): Promise<string> => {
  const app = createSSRApp({
    render: () => h(ResumeDocument, { document, context: { lng, mode } }),
  });
  app.provide(PaginationMeasureKey, syntheticMeasure);
  return renderToString(app);
};

const template = (id: string) => TEMPLATES.find((preset) => preset.id === id)!;

describe('gallery samples', () => {
  it.each(SAMPLES.map(({ templateId, lng }) => [templateId, lng] as const))(
    '%s (%s) is its template applied to its content, apart from date format',
    async (templateId, lng) => {
      const sample = (await loadSample(templateId, lng))!;
      const preset = template(templateId);
      const applied = applyTemplate(
        sample.customization,
        preset,
        sample.content,
        lng,
      );
      const { dateFormat: appliedDateFormat, ...appliedRest } = applied;
      const { dateFormat: _sampleDateFormat, ...sampleRest }
        = sample.customization;
      expect(appliedRest).toEqual(sampleRest);
      expect(appliedDateFormat).toBe(dateFormatForLanguage(lng));
    },
  );

  it.each(SAMPLES.map(({ templateId, lng }) => [templateId, lng] as const))(
    '%s (%s) renders paged and continuous',
    async (templateId, lng) => {
      const sample = (await loadSample(templateId, lng))!;
      for (const mode of ['paged', 'continuous'] as const) {
        const html = await render(sample, lng, mode);
        expect(html).toContain(sample.personalDetails.fullName!);
        expect(html).toContain(`lang="${lng}"`);
      }
    },
  );

  it.each(FILLER_LANGUAGES.map((lng) => [lng] as const))(
    'the %s filler renders under every template',
    async (lng) => {
      const filler = (await loadFiller(lng))!;
      for (const preset of TEMPLATES) {
        const document = structuredClone(filler);
        document.customization = applyTemplate(
          document.customization,
          preset,
          document.content,
          lng,
        );
        const html = await render(document, lng, 'paged');
        expect(html, preset.id).toContain(filler.personalDetails.fullName!);
      }
    },
  );
});

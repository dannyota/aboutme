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
import { PaginationMeasureKey } from '../../app/components/resume/measure';
import ResumeDocument from '../../app/components/resume/ResumeDocument.vue';
import { syntheticMeasure } from './synthetic-measure';

// Gallery samples render with the real renderer, and a sample's layout is
// exactly what applying its own template produces, so "use this sample" and
// "use this template" agree.

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
    '%s (%s) is its template applied to its content',
    async (templateId, lng) => {
      const sample = (await loadSample(templateId, lng))!;
      expect(applyTemplate(
        sample.customization,
        template(templateId),
        sample.content,
      )).toEqual(sample.customization);
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
        );
        const html = await render(document, lng, 'paged');
        expect(html, preset.id).toContain(filler.personalDetails.fullName!);
      }
    },
  );
});

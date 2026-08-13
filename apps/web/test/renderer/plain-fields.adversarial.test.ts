// @vitest-environment node

import type { PersonalDetail, Resume } from '@aboutme/schema';
import { JSDOM } from 'jsdom';
import { createSSRApp, h } from 'vue';
import { renderToString } from 'vue/server-renderer';
import { describe, expect, it } from 'vitest';

import ResumeDocument from '../../app/components/resume/ResumeDocument.vue';

const hostile = '<img src=x onerror=alert(1)>& event=javascript:';
const baseCustomization: Resume['customization'] = {
  font: { family: 'inter', baseSizePx: 14 },
  colors: {
    primary: '#111111',
    text: '#222222',
    background: '#ffffff',
  },
  spacing: { sectionGap: 16, entryGap: 8, lineHeight: 1.4 },
  heading: { style: 'normal', showRule: false },
  layout: { columns: 1, sections: { main: [], sidebar: [] } },
  sectionDisplay: {
    skill: { style: 'text' },
    language: { style: 'text' },
  },
  pageFormat: 'a4',
  dateFormat: 'MM/YYYY',
};

const detailTypes: PersonalDetail['type'][] = [
  'email',
  'phone',
  'location',
  'website',
  'linkedin',
  'github',
  'twitter',
  'custom',
];

const entryId = (index: number): string =>
  `00000000-0000-4000-8000-${String(index).padStart(12, '0')}`;

describe('renderer plain fields', () => {
  it(
    'escapes every renderable plain-text slot as text with no active URL',
    async () => {
      const document: Resume = {
        schemaVersion: 2,
        personalDetails: {
          fullName: hostile,
          headline: hostile,
          details: detailTypes.map((type, index) => ({
            id: entryId(index),
            type,
            label: hostile,
            value: hostile,
            isHidden: false,
          })),
        },
        content: {
          profile: {
            sectionType: 'profile',
            displayName: hostile,
            entries: [{ id: entryId(100), text: '' }],
          },
          work: {
            sectionType: 'work',
            displayName: hostile,
            entries: [
              {
                id: entryId(101),
                jobTitle: hostile,
                employer: hostile,
                city: hostile,
                country: hostile,
              },
            ],
          },
          education: {
            sectionType: 'education',
            displayName: hostile,
            entries: [
              {
                id: entryId(102),
                degree: hostile,
                school: hostile,
                city: hostile,
                country: hostile,
              },
            ],
          },
          skill: {
            sectionType: 'skill',
            displayName: hostile,
            entries: [{ id: entryId(103), name: hostile }],
          },
          language: {
            sectionType: 'language',
            displayName: hostile,
            entries: [{ id: entryId(104), name: hostile }],
          },
          certificate: {
            sectionType: 'certificate',
            displayName: hostile,
            entries: [
              { id: entryId(105), title: hostile, issuer: hostile },
            ],
          },
          project: {
            sectionType: 'project',
            displayName: hostile,
            entries: [{ id: entryId(106), title: hostile }],
          },
          custom: {
            sectionType: 'custom',
            displayName: hostile,
            entries: [
              {
                id: entryId(107),
                title: hostile,
                subtitle: hostile,
                city: hostile,
              },
            ],
          },
        },
        customization: {
          ...baseCustomization,
          layout: {
            ...baseCustomization.layout,
            sections: {
              main: [
                'profile',
                'work',
                'education',
                'skill',
                'language',
                'certificate',
                'project',
                'custom',
              ],
              sidebar: [],
            },
          },
        },
      };
      const html = await renderToString(
        createSSRApp({
          render: () =>
            h(ResumeDocument, {
              document,
              context: { lng: 'en', mode: 'continuous' },
            }),
        }),
      );
      const dom = new JSDOM(html);
      const doc = dom.window.document;
      expect(doc.querySelectorAll('img,script').length).toBe(0);
      expect(doc.querySelector('[onerror]')).toBeNull();
      expect(doc.querySelector('[href^="javascript:"]')).toBeNull();
      expect(doc.querySelectorAll('a').length).toBe(0);
      expect(doc.body.textContent?.split(hostile).length - 1).toBe(42);
    });
});

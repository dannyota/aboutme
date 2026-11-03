import type { Resume, Section } from '@aboutme/schema';
import { TEMPLATES } from '@aboutme/schema/templates';

import source from '../../../../../packages/schema/fixtures/full.json';
import { applyTemplate } from '../../components/resume/applyTemplate';

export const PRINT_PROBES = [
  'heading', 'header-only', 'orphan', 'widow', 'breakable-entry',
  'widgets-bar', 'widgets-dots', 'widgets-tag', 'widgets-text',
] as const;
export type PrintProbe = typeof PRINT_PROBES[number];

const id = (index: number) => '00000000-0000-4000-9000-'
  + String(index).padStart(12, '0');

export function printProbe(
  probe: PrintProbe,
  lng: 'en' | 'vi',
  templateId: 'modern-sidebar' | 'engineer-compact',
): Resume {
  const document = structuredClone(source) as Resume;
  const template = TEMPLATES.find(({ id }) => id === templateId)!;
  document.customization = applyTemplate(
    document.customization, template, document.content, lng,
  );
  document.personalDetails = { details: [] };
  if (probe.startsWith('widgets-')) {
    const mode = probe.slice('widgets-'.length) as
      'bar' | 'dots' | 'tag' | 'text';
    const entries = (prefix: string) => Array.from({ length: 7 },
      (_, index) => ({
        id: id(index + (prefix === 'SKILL' ? 1 : 11)),
        isHidden: false,
        name: `${prefix}-${index < 6 ? index : 'NONE'}`,
        ...(index < 6 ? { level: index } : {}),
      }));
    document.content = {
      skill: { sectionType: 'skill', displayName: lng === 'vi'
        ? 'Kỹ năng'
        : 'Skills', entries: entries('SKILL') },
      language: { sectionType: 'language', displayName: lng === 'vi'
        ? 'Ngôn ngữ'
        : 'Languages', entries: entries('LANGUAGE') },
    };
    document.customization.layout.sections = {
      main: ['skill'], sidebar: ['language'],
    };
    document.customization.sectionDisplay.skill.style = mode;
    document.customization.sectionDisplay.language.style = mode;
    return document;
  }
  document.customization.font.baseSizePx = 16;
  document.customization.spacing = {
    sectionGap: 0, entryGap: 0, lineHeight: 1.5,
    pageMargin: { x: 15, y: 15 },
  };
  document.customization.heading = { style: 'normal', showRule: false };
  document.customization.layout.surfaceTarget = 'none';
  if (probe === 'breakable-entry') {
    document.customization.layout.columns = 1;
    document.customization.layout.sections = {
      main: ['profile'], sidebar: [],
    };
    document.content = { profile: { sectionType: 'profile', entries: [
      { id: id(1), isHidden: false, text: '<p>MAIN-START MAIN-ENTRY-01<br>'
        + Array.from({ length: 10 }, (_, index) => `BREAK-LINE-${index + 1}`)
          .join('<br>')
          + '<br>BREAK-LAST</p>' },
      { id: id(2), isHidden: false, text: '<p>MAIN-ENTRY-02 MAIN-END</p>' },
    ] } };
    return document;
  }
  if (probe === 'header-only') {
    document.personalDetails.fullName = 'HEADER-FIXTURE';
    const section = (kind: 'skill' | 'language'): Section => ({
      sectionType: kind,
      displayName: kind.toUpperCase(),
      entries: Array.from({ length: 56 }, (_, index) => ({
        id: id(index + (kind === 'skill' ? 1 : 101)),
        isHidden: false,
        name: `${kind.toUpperCase()}-${String(index + 1).padStart(2, '0')}`,
      })),
    });
    document.content = {
      skill: section('skill'), language: section('language'),
    };
    document.customization.layout.sections = {
      main: ['skill'], sidebar: ['language'],
    };
  } else {
    document.customization.layout.columns = 1;
    document.customization.layout.sections = { main: ['probe'], sidebar: [] };
    document.content = probe === 'heading'
      ? { probe: { sectionType: 'work', displayName: 'KEEP-HEADING', entries: [{
          id: id(1), isHidden: false, jobTitle: 'KEEP-TITLE',
          description: '<p>KEEP-FIRST<br>KEEP-SECOND</p>',
        }] } }
      : { probe: { sectionType: 'profile', entries: [{
          id: id(1), isHidden: false,
          text: `<p>${probe.toUpperCase()}-FIRST<br>`
            + `${probe.toUpperCase()}-SECOND</p>`,
        }] } };
  }
  return document;
}

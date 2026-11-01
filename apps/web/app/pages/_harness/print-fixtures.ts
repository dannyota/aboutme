import type { Resume, Section } from '@aboutme/schema';
import { TEMPLATES } from '@aboutme/schema/templates';

import fullSource from '../../../../../packages/schema/fixtures/full.json';
import vnFullSource from '../../../../../packages/schema/fixtures/vn-full.json';
import { applyTemplate } from '../../components/resume/applyTemplate';

export type PrintFixtureId = 'print-main-overflow' | 'print-sidebar-overflow';

export interface PrintFixture {
  readonly document: Resume;
  readonly overflowingFlow: 'main' | 'sidebar';
}

const modernSidebar = (() => {
  const preset = TEMPLATES.find(({ id }) => id === 'modern-sidebar');
  if (preset === undefined) {
    throw new Error(
      'The modern-sidebar template is required for print fixtures.',
    );
  }
  return preset;
})();

const MAIN_ENTRIES = 9;
const SIDEBAR_ENTRIES = 13;

const uuid = (flow: 'main' | 'sidebar', index: number): string =>
  `00000000-0000-4000-${flow === 'main' ? '9' : '8'}000-`
  + String(index).padStart(12, '0');

function baseDocument(lng: 'en' | 'vi'): Resume {
  return structuredClone(lng === 'en' ? fullSource : vnFullSource) as Resume;
}

function applyModernSidebar(document: Resume, lng: 'en' | 'vi'): Resume {
  // The fixture owner already prints on the preset's paper; a template switch
  // keeps the owner's page format.
  document.customization = applyTemplate(
    {
      ...document.customization,
      pageFormat: modernSidebar.customization.pageFormat,
    },
    modernSidebar,
    document.content,
    lng,
  );
  return document;
}

function sidebarOverflow(lng: 'en' | 'vi'): Resume {
  const document = baseDocument(lng);
  const profile = structuredClone(document.content.profile) as Section;
  const skill = structuredClone(document.content.skill) as Section;
  if (profile.sectionType !== 'profile' || skill.sectionType !== 'skill') {
    throw new Error('Vietnamese fixture print sections have unexpected types.');
  }
  profile.entries = [
    {
      id: uuid('main', 1),
      isHidden: false,
      text: '<p>MAIN-SHORT</p>',
    },
  ];
  const skillTemplate = skill.entries[0];
  if (skillTemplate === undefined) {
    throw new Error('Vietnamese skill fixture must contain one entry.');
  }
  // Enough sidebar entries to overflow page one under the print rhythm.
  skill.entries = Array.from({ length: SIDEBAR_ENTRIES }, (_, offset) => {
    const number = offset + 1;
    const marker = `SIDEBAR-ENTRY-${String(number).padStart(2, '0')}`;
    return {
      ...structuredClone(skillTemplate),
      id: uuid('sidebar', number),
      isHidden: false,
      name: [
        number === 1 ? 'SIDEBAR-START' : '',
        marker,
        number === SIDEBAR_ENTRIES ? 'SIDEBAR-END' : '',
      ]
        .filter(Boolean)
        .join(' '),
      infoHtml: lng === 'vi'
        ? `<p>Nội dung kiểm tra phân trang ${number}.</p>`
        : `<p>Pagination test content ${number}.</p>`,
    };
  });
  document.content = { profile, skill };
  document.customization.layout.sections = {
    main: ['profile'],
    sidebar: ['skill'],
  };
  return applyModernSidebar(document, lng);
}

function mainOverflow(lng: 'en' | 'vi'): Resume {
  const document = baseDocument(lng);
  const work = structuredClone(document.content.work) as Section;
  const skill = structuredClone(document.content.skill) as Section;
  if (work.sectionType !== 'work' || skill.sectionType !== 'skill') {
    throw new Error('Vietnamese fixture print sections have unexpected types.');
  }
  const workTemplate = work.entries[0];
  if (workTemplate === undefined) {
    throw new Error('Vietnamese work fixture must contain one entry.');
  }
  // Enough main entries to overflow page one with room to spare.
  work.entries = Array.from({ length: MAIN_ENTRIES }, (_, offset) => {
    const number = offset + 1;
    const marker = `MAIN-ENTRY-${String(number).padStart(2, '0')}`;
    return {
      ...structuredClone(workTemplate),
      id: uuid('main', number),
      isHidden: false,
      jobTitle: [
        number === 1 ? 'MAIN-START' : '',
        marker,
        number === MAIN_ENTRIES ? 'MAIN-END' : '',
      ]
        .filter(Boolean)
        .join(' '),
      description: lng === 'vi'
        ? `<p>Nội dung kiểm tra phân trang bản in ${number}.</p>`
        : `<p>Print pagination test content ${number}.</p>`,
    };
  });
  const skillTemplate = skill.entries[0];
  if (skillTemplate === undefined) {
    throw new Error('Vietnamese skill fixture must contain one entry.');
  }
  skill.entries = [
    {
      ...structuredClone(skillTemplate),
      id: uuid('sidebar', 1),
      isHidden: false,
      name: 'SIDEBAR-SHORT',
      infoHtml: '<p>SIDEBAR-SHORT</p>',
    },
  ];
  document.content = { work, skill };
  document.customization.layout.sections = {
    main: ['work'],
    sidebar: ['skill'],
  };
  return applyModernSidebar(document, lng);
}

export const PRINT_FIXTURES: Readonly<Record<PrintFixtureId, PrintFixture>>
  = Object.freeze({
    'print-main-overflow': Object.freeze({
      document: mainOverflow('vi'),
      overflowingFlow: 'main',
    }),
    'print-sidebar-overflow': Object.freeze({
      document: sidebarOverflow('vi'),
      overflowingFlow: 'sidebar',
    }),
  });

export const ENGLISH_PRINT_FIXTURES: Readonly<
  Record<PrintFixtureId, PrintFixture>
> = Object.freeze({
  'print-main-overflow': Object.freeze({
    document: mainOverflow('en'),
    overflowingFlow: 'main',
  }),
  'print-sidebar-overflow': Object.freeze({
    document: sidebarOverflow('en'),
    overflowingFlow: 'sidebar',
  }),
});

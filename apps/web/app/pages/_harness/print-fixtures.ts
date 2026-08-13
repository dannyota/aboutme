import type { Resume, Section } from '@aboutme/schema';
import { TEMPLATES } from '@aboutme/schema/templates';

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

const uuid = (flow: 'main' | 'sidebar', index: number): string =>
  `00000000-0000-4000-${flow === 'main' ? '9' : '8'}000-`
  + String(index).padStart(12, '0');

function baseDocument(): Resume {
  return structuredClone(vnFullSource) as Resume;
}

function applyModernSidebar(document: Resume): Resume {
  document.customization = applyTemplate(
    document.customization,
    modernSidebar,
    document.content,
  );
  return document;
}

function sidebarOverflow(): Resume {
  const document = baseDocument();
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
  skill.entries = Array.from({ length: 16 }, (_, offset) => {
    const number = offset + 1;
    const marker = `SIDEBAR-ENTRY-${String(number).padStart(2, '0')}`;
    return {
      ...structuredClone(skillTemplate),
      id: uuid('sidebar', number),
      isHidden: false,
      name: [
        number === 1 ? 'SIDEBAR-START' : '',
        marker,
        number === 16 ? 'SIDEBAR-END' : '',
      ]
        .filter(Boolean)
        .join(' '),
      infoHtml: `<p>Nội dung kiểm tra phân trang ${number}.</p>`,
    };
  });
  document.content = { profile, skill };
  document.customization.layout.sections = {
    main: ['profile'],
    sidebar: ['skill'],
  };
  return applyModernSidebar(document);
}

function mainOverflow(): Resume {
  const document = baseDocument();
  const work = structuredClone(document.content.work) as Section;
  const skill = structuredClone(document.content.skill) as Section;
  if (work.sectionType !== 'work' || skill.sectionType !== 'skill') {
    throw new Error('Vietnamese fixture print sections have unexpected types.');
  }
  const workTemplate = work.entries[0];
  if (workTemplate === undefined) {
    throw new Error('Vietnamese work fixture must contain one entry.');
  }
  work.entries = Array.from({ length: 18 }, (_, offset) => {
    const number = offset + 1;
    const marker = `MAIN-ENTRY-${String(number).padStart(2, '0')}`;
    return {
      ...structuredClone(workTemplate),
      id: uuid('main', number),
      isHidden: false,
      jobTitle: [
        number === 1 ? 'MAIN-START' : '',
        marker,
        number === 18 ? 'MAIN-END' : '',
      ]
        .filter(Boolean)
        .join(' '),
      description: `<p>Nội dung kiểm tra phân trang bản in ${number}.</p>`,
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
  return applyModernSidebar(document);
}

export const PRINT_FIXTURES: Readonly<Record<PrintFixtureId, PrintFixture>>
  = Object.freeze({
    'print-main-overflow': Object.freeze({
      document: mainOverflow(),
      overflowingFlow: 'main',
    }),
    'print-sidebar-overflow': Object.freeze({
      document: sidebarOverflow(),
      overflowingFlow: 'sidebar',
    }),
  });

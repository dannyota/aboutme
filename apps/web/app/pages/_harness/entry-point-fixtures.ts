import type { Resume } from '@aboutme/schema';

type Lng = 'vi' | 'en';

export interface EntryPointFixture {
  readonly projectsUrl: string;
  readonly projectLinks: ReadonlyMap<string, string>;
}

const PROJECTS_URL = 'https://aboutme.vn/harness-public/projects';

const SHORT_LINKED = 'e0000000-0000-4000-8000-000000000001';
const SHORT_PLAIN = 'e0000000-0000-4000-8000-000000000002';
const LONG_LINKED = 'e0000000-0000-4000-8000-000000000003';
const LONG_PLAIN = 'e0000000-0000-4000-8000-000000000004';
const OWN_LINK = 'e0000000-0000-4000-8000-000000000005';

const TITLES: Readonly<Record<Lng, Readonly<Record<
  'short' | 'long' | 'own', string>>>> = {
  vi: {
    short: 'Từ điển dữ liệu mở',
    long: 'Nền tảng tra cứu thuật ngữ kỹ thuật tiếng Việt cho cộng đồng lập '
      + 'trình viên, gồm từ điển mở, công cụ đóng góp và giao diện lập trình',
    own: 'Thư viện địa chỉ Việt Nam',
  },
  en: {
    short: 'Note G Interpreter',
    long: 'An open reference platform for analytical engine programs, with '
      + 'a shared glossary, contribution tools and a public programming '
      + 'interface',
    own: 'Difference Engine study',
  },
};

export function withEntryPoints(
  document: Resume,
  lng: Lng,
): EntryPointFixture {
  const section = Object.values(document.content)
    .find((value) => value.sectionType === 'project');
  if (section?.sectionType !== 'project') {
    throw new Error('Fixture has no project section.');
  }
  const titles = TITLES[lng];
  section.entries = [
    { id: SHORT_LINKED, title: titles.short },
    { id: SHORT_PLAIN, title: titles.short },
    { id: LONG_LINKED, title: titles.long },
    { id: LONG_PLAIN, title: titles.long },
    { id: OWN_LINK, title: titles.own, link: 'https://github.com/ada/engine' },
  ];
  return {
    projectsUrl: PROJECTS_URL,
    projectLinks: new Map([
      [SHORT_LINKED, `${PROJECTS_URL}/short-title`],
      [LONG_LINKED, `${PROJECTS_URL}/long-title`],
    ]),
  };
}

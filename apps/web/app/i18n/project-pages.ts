// Copy for the public project pages (/{slug}/projects and
// /{slug}/projects/{project-slug}), specified in
// docs/design/ui/project-pages.md. The pages take their copy language from
// the resume, as the public page bar does, never from the interface cookie.
// Only the public render worker imports this catalog.

export type ProjectPageLanguage = 'vi' | 'en';

export type ProjectPageCopy = {
  readonly projects: string;
  readonly outcome: string;
  readonly about: string;
  readonly role: string;
  readonly dates: string;
  readonly technologies: string;
  readonly otherProjects: string;
  readonly repository: string;
  readonly demo: string;
  readonly link: string;
  /** The page credit; Go's validator expects exactly this text. */
  readonly credit: string;
  /** The accessible name of the bar's link back to the resume. */
  readonly back: (name: string) => string;
};

export const PROJECT_PAGE_COPY: Readonly<
  Record<ProjectPageLanguage, ProjectPageCopy>
> = {
  vi: {
    projects: 'Dự án',
    outcome: 'Kết quả',
    about: 'Mô tả',
    role: 'Vai trò',
    dates: 'Thời gian',
    technologies: 'Công nghệ',
    otherProjects: 'Xem các dự án khác',
    repository: 'Repository',
    demo: 'Demo',
    link: 'Liên kết',
    credit: 'Tạo bằng aboutme.vn',
    back: (name) => `Về CV của ${name}`,
  },
  en: {
    projects: 'Projects',
    outcome: 'Outcome',
    about: 'About the project',
    role: 'Role',
    dates: 'Dates',
    technologies: 'Technologies',
    otherProjects: 'See other projects',
    repository: 'Repository',
    demo: 'Demo',
    link: 'Link',
    credit: 'Built with aboutme.vn',
    back: (name) => `Back to ${name}'s resume`,
  },
};

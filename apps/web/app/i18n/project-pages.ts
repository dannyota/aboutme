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
  readonly credit: string;
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

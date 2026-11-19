export type ProjectPageLanguage = 'vi' | 'en';

export type ProjectEvidenceLabels = {
  readonly repository: string;
  readonly pullRequest: string;
  readonly release: string;
};

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
  readonly evidence: string;
  readonly evidenceLabels: ProjectEvidenceLabels;
  readonly checked: (date: string) => string;
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
    evidence: 'Bằng chứng trên GitHub',
    evidenceLabels: {
      repository: 'Chủ sở hữu kho mã này',
      pullRequest: 'Tác giả của pull request đã được merge',
      release: 'Tác giả của bản phát hành này',
    },
    checked: (date) => `kiểm tra ${date}`,
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
    evidence: 'GitHub evidence',
    evidenceLabels: {
      repository: 'Owner of this repository',
      pullRequest: 'Author of a merged pull request',
      release: 'Author of this release',
    },
    checked: (date) => `checked ${date}`,
  },
};

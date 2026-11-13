import type {
  ProjectDetail,
  ProjectPageResume,
  ProjectSummary,
} from '../../../server/utils/public-render/project-envelope';

// Fixed project pages for the render harness, as the public render worker
// receives them (docs/design/projects-contract.md "Render envelope"). Each
// language has a list with a fallback-language row and a project page with
// every field, so both pages show every part at every width.

type Lng = 'vi' | 'en';
type Scheme = ProjectPageResume['colorScheme'];

const RESUME_URL = 'https://aboutme.vn/harness-public';
const LIST_URL = `${RESUME_URL}/projects`;

export interface ProjectListFixture {
  readonly resume: ProjectPageResume;
  readonly projects: readonly ProjectSummary[];
}

export interface ProjectFixture {
  readonly resume: ProjectPageResume;
  readonly project: ProjectDetail;
  readonly entryLink: string;
  readonly listUrl: string;
  readonly otherProjects: boolean;
}

const resume = (lng: Lng, colorScheme: Scheme): ProjectPageResume => ({
  slug: 'harness-public',
  url: RESUME_URL,
  ownerName: lng === 'vi' ? 'Nguyễn Văn An' : 'Ada Lovelace',
  lng,
  uiLng: lng,
  colorScheme,
  dateFormat: lng === 'vi' ? 'MM/YYYY' : 'Mon YYYY',
});

const row = (slug: string, fields: Omit<ProjectSummary, 'slug' | 'url'>) =>
  ({ slug, url: `${LIST_URL}/${slug}`, ...fields });

const TECHNOLOGIES = [
  'Go', 'PostgreSQL', 'Kafka', 'Redis', 'Kubernetes', 'gRPC',
  'OpenTelemetry', 'Terraform', 'GitHub Actions',
];

const PAYMENTS_DATES = {
  start: { y: 2022, m: 3 }, end: { y: 2024, m: 6 }, present: false,
};

const PAYMENTS: Readonly<Record<Lng, ProjectDetail>> = {
  vi: {
    slug: 'he-thong-thanh-toan',
    url: `${LIST_URL}/he-thong-thanh-toan`,
    name: 'Hệ thống thanh toán thời gian thực',
    role: 'Trưởng nhóm backend',
    dates: PAYMENTS_DATES,
    summary: 'Xử lý hơn hai triệu giao dịch mỗi ngày với độ trễ dưới 50 ms '
      + 'cho một ví điện tử trong nước.',
    outcome: 'Giảm 40% thời gian đối soát cuối ngày và không có sự cố mất '
      + 'giao dịch nào trong hai năm vận hành.',
    description: '<p>Thiết kế lại luồng thanh toán thành các dịch vụ nhỏ, '
      + 'mỗi dịch vụ sở hữu dữ liệu của mình.</p><ul><li>Hàng đợi sự kiện '
      + 'cho mọi thay đổi số dư</li><li><strong>Đối soát</strong> tự động '
      + 'mỗi giờ</li></ul><p>Chi tiết trong <a href="https://example.com/'
      + 'bai-viet" rel="noopener noreferrer">bài viết kỹ thuật</a>.</p>',
    technologies: TECHNOLOGIES,
    repositoryUrl: 'https://github.com/example/thanh-toan',
    demoUrl: 'https://demo.example.com/thanh-toan',
  },
  en: {
    slug: 'payments-platform',
    url: `${LIST_URL}/payments-platform`,
    name: 'Real-time payments platform',
    role: 'Backend lead',
    dates: PAYMENTS_DATES,
    summary: 'Processes over two million transactions a day under 50 ms for '
      + 'a domestic e-wallet.',
    outcome: 'Cut end-of-day reconciliation time by 40% with no lost '
      + 'transaction in two years of operation.',
    description: '<p>Split the payment flow into small services, each '
      + 'owning its data.</p><ul><li>An event queue for every balance '
      + 'change</li><li><strong>Reconciliation</strong> every hour</li></ul>'
      + '<p>Details in <a href="https://example.com/write-up" '
      + 'rel="noopener noreferrer">the technical write-up</a>.</p>',
    technologies: TECHNOLOGIES,
    repositoryUrl: 'https://github.com/example/payments',
    demoUrl: 'https://demo.example.com/payments',
  },
};

// The list's first row is the payments project as a list row carries it.
const paymentsRow = (lng: Lng): ProjectSummary => {
  const { slug, url, name, role, dates, summary } = PAYMENTS[lng];
  return {
    slug, url, name, role, dates, summary,
    technologies: TECHNOLOGIES.slice(0, 6),
    moreTechnologies: TECHNOLOGIES.length - 6,
  };
};

const ROWS: Readonly<Record<Lng, readonly ProjectSummary[]>> = {
  vi: [
    paymentsRow('vi'),
    row('cong-cu-giam-sat', {
      name: 'Công cụ giám sát',
      role: 'Kỹ sư',
      dates: { start: { y: 2021, m: 1 }, end: null, present: true },
      summary: 'Bảng điều khiển và cảnh báo cho ba trăm máy chủ.',
      technologies: ['Prometheus', 'Grafana', 'Terraform'],
    }),
    row('deployment-cli', {
      lang: 'en',
      name: 'Open-source deployment CLI',
      technologies: [],
    }),
    row('thu-vien-xac-thuc', {
      name: 'Thư viện xác thực OAuth dùng chung cho các dịch vụ nội bộ '
        + 'của công ty',
      dates: { start: { y: 2020 }, end: { y: 2020 }, present: false },
      technologies: ['TypeScript'],
    }),
  ],
  en: [
    paymentsRow('en'),
    row('monitoring-tool', {
      name: 'Monitoring tool',
      role: 'Engineer',
      dates: { start: { y: 2021, m: 1 }, end: null, present: true },
      summary: 'Dashboards and alerts for three hundred servers.',
      technologies: ['Prometheus', 'Grafana', 'Terraform'],
    }),
    row('may-ve-bieu-do', {
      lang: 'vi',
      name: 'Máy vẽ biểu đồ',
      technologies: [],
    }),
    row('shared-oauth-library', {
      name: 'Shared OAuth library for every internal service in the '
        + 'company',
      dates: { start: { y: 2020 }, end: { y: 2020 }, present: false },
      technologies: ['TypeScript'],
    }),
  ],
};

export function projectListFixture(
  lng: Lng, scheme: Scheme,
): ProjectListFixture {
  return { resume: resume(lng, scheme), projects: ROWS[lng] };
}

export function projectFixture(lng: Lng, scheme: Scheme): ProjectFixture {
  return {
    resume: resume(lng, scheme),
    project: PAYMENTS[lng],
    entryLink: lng === 'vi'
      ? 'https://blog.example.com/thanh-toan'
      : 'https://blog.example.com/payments',
    listUrl: LIST_URL,
    otherProjects: true,
  };
}

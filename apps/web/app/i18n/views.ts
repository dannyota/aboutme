// Copy for the owner's Views pages (/app/views and /app/views/{id}).
// Every count and label here describes docs/design/viewer-analytics/
// counting.md: real views pass every layer; filtered views are bots,
// hosting networks, anomalies, failed checks, and crawlers.
import type { WorkspaceCopy } from './workspace';

export type ViewsPlatform
  = | 'facebook' | 'linkedin' | 'x' | 'telegram' | 'whatsapp' | 'slack'
    | 'discord' | 'skype' | 'viber' | 'zalo';

// Brand names read the same in both site languages.
export const platformNames: Readonly<Record<ViewsPlatform, string>> = {
  facebook: 'Facebook',
  linkedin: 'LinkedIn',
  x: 'X',
  telegram: 'Telegram',
  whatsapp: 'WhatsApp',
  slack: 'Slack',
  discord: 'Discord',
  skype: 'Skype',
  viber: 'Viber',
  zalo: 'Zalo',
};

// "1 real view" vs "N real views"; Vietnamese has no plural form.
function realViews(real: number): string {
  return `${real} real ${real === 1 ? 'view' : 'views'}`;
}

export type ViewsIndexCopy = {
  readonly title: string;
  readonly loading: string;
  readonly unavailable: string;
  readonly emptyTitle: string;
  readonly emptyDescription: string;
  readonly live: string;
  readonly draft: string;
  readonly last7: string;
  readonly last30: string;
  readonly last90: string;
  readonly realFiltered: (real: number, filtered: number) => string;
};

export type ViewsFilteredKey
  = 'bot' | 'datacenter' | 'anomaly' | 'invalid' | 'crawler';

export type ViewsDetailCopy = {
  readonly loading: string;
  readonly unavailable: string;
  readonly notFoundTitle: string;
  readonly notFoundDescription: string;
  readonly backToViews: string;
  readonly headline: (real: number, filtered: number) => string;
  readonly definition: string;
  readonly chartHeading: string;
  readonly chartCaption: string;
  readonly chartDay: (date: string, real: number) => string;
  readonly monthlyHeading: string;
  readonly monthLabel: (month: string, real: number) => string;
  readonly filteredHeading: string;
  readonly filteredLabels: Readonly<Record<ViewsFilteredKey, string>>;
  readonly previewsNote: string;
  readonly previewLine: (platform: string, fetches: number) => string;
  readonly honestLimit: string;
};

export const viewsIndexCopy: WorkspaceCopy<ViewsIndexCopy> = {
  vi: {
    title: 'Lượt xem',
    loading: 'Đang tải lượt xem…',
    unavailable: 'Không thể tải lượt xem. Hãy thử lại.',
    emptyTitle: 'Chưa có CV nào được xuất bản',
    emptyDescription: 'Xuất bản một CV để bắt đầu theo dõi lượt xem.',
    live: 'Đang công khai',
    draft: 'Bản nháp',
    last7: '7 ngày qua',
    last30: '30 ngày qua',
    last90: '90 ngày qua',
    realFiltered: (real, filtered) => (
      `${real} lượt xem thật · ${filtered} bị lọc`
    ),
  },
  en: {
    title: 'Views',
    loading: 'Loading views…',
    unavailable: 'Could not load views. Try again.',
    emptyTitle: 'No resume has been published yet',
    emptyDescription: 'Publish a resume to start tracking its views.',
    live: 'Live',
    draft: 'Draft',
    last7: 'Last 7 days',
    last30: 'Last 30 days',
    last90: 'Last 90 days',
    realFiltered: (real, filtered) => (
      `${realViews(real)} · ${filtered} filtered`
    ),
  },
};

export const viewsDetailCopy: WorkspaceCopy<ViewsDetailCopy> = {
  vi: {
    loading: 'Đang tải lượt xem…',
    unavailable: 'Không thể tải lượt xem. Hãy thử lại.',
    notFoundTitle: 'Không tìm thấy CV',
    notFoundDescription: 'CV này không tồn tại hoặc bạn không có quyền xem.',
    backToViews: 'Quay lại Lượt xem',
    headline: (real, filtered) => `${real} lượt xem thật · ${filtered} bị lọc`,
    definition:
      'Mỗi kết nối mạng được tính tối đa một lượt xem mỗi ngày. Hai người '
      + 'dùng chung một kết nối mạng trong một ngày được tính một lần; một '
      + 'người xem vào hai ngày được tính hai lần.',
    chartHeading: 'Lượt xem thật theo ngày (90 ngày qua)',
    chartCaption: 'Số lượt xem thật mỗi ngày, 90 ngày qua',
    chartDay: (date, real) => `${date}: ${real} lượt xem thật`,
    monthlyHeading: 'Tổng theo tháng (12 tháng qua)',
    monthLabel: (month, real) => `${month}: ${real} lượt xem thật`,
    filteredHeading: 'Lượt bị lọc trong 90 ngày qua',
    filteredLabels: {
      bot: 'Bot',
      datacenter: 'Truy cập từ máy chủ',
      anomaly: 'Tăng đột biến',
      invalid: 'Không vượt qua kiểm tra',
      crawler: 'Trình thu thập dữ liệu',
    },
    previewsNote:
      'Mỗi lần chia sẻ, ứng dụng có thể tải bản xem trước nhiều lần.',
    previewLine: (platform, fetches) => (
      `Xem trước liên kết trên ${platform} × ${fetches}`
    ),
    honestLimit:
      'Số liệu chỉ loại được những gì bộ lọc phát hiện; công cụ tự '
      + 'động tinh vi vẫn có thể được tính.',
  },
  en: {
    loading: 'Loading views…',
    unavailable: 'Could not load views. Try again.',
    notFoundTitle: 'Resume not found',
    notFoundDescription: 'This resume does not exist or you cannot view it.',
    backToViews: 'Back to views',
    headline: (real, filtered) => `${realViews(real)} · ${filtered} filtered`,
    definition:
      'One view per network per day. Two people on one network in one day '
      + 'count once; one person on two days counts twice.',
    chartHeading: 'Real views by day (last 90 days)',
    chartCaption: 'Real views per day, last 90 days',
    chartDay: (date, real) => `${date}: ${realViews(real)}`,
    monthlyHeading: 'Monthly totals (last 12 months)',
    monthLabel: (month, real) => `${month}: ${realViews(real)}`,
    filteredHeading: 'Filtered over the last 90 days',
    filteredLabels: {
      bot: 'Bots',
      datacenter: 'Hosting networks',
      anomaly: 'Anomalies',
      invalid: 'Failed checks',
      crawler: 'Crawlers',
    },
    previewsNote: 'One share can fetch the preview more than once.',
    previewLine: (platform, fetches) => (
      `Link previews on ${platform} × ${fetches}`
    ),
    honestLimit:
      'Counts exclude only what the filters detect; sophisticated '
      + 'automation can still be counted.',
  },
};

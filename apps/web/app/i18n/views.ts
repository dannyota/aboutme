// Copy for the owner's Views pages (/app/views and /app/views/{id}).
// Every count and label here describes docs/design/viewer-analytics/
// counting.md: real views pass every layer; filtered views are bots,
// hosting networks, anomalies, failed checks, and crawlers.
import type { Locale } from './locale';
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

const numberFormatters: Readonly<Record<Locale, Intl.NumberFormat>> = {
  vi: new Intl.NumberFormat('vi-VN'),
  en: new Intl.NumberFormat('en-US'),
};

function formatNumber(locale: Locale, value: number): string {
  return numberFormatters[locale].format(value);
}

// "1 real view" vs "N real views"; Vietnamese has no plural form.
function realViewsEn(real: number): string {
  return `${formatNumber('en', real)} real ${real === 1 ? 'view' : 'views'}`;
}

// Days and months arrive as 'YYYY-MM-DD' / 'YYYY-MM' Asia/Ho_Chi_Minh
// calendar values. Parsing as Date.UTC and formatting with timeZone: 'UTC'
// keeps the calendar day from shifting with the viewer's own time zone.
function parseDay(date: string): Date {
  const [year, month, day] = date.split('-').map(Number);
  return new Date(Date.UTC(year, month - 1, day));
}

function parseMonth(month: string): Date {
  const [year, monthIndex] = month.split('-').map(Number);
  return new Date(Date.UTC(year, monthIndex - 1, 1));
}

const axisDayFormatters: Readonly<Record<Locale, Intl.DateTimeFormat>> = {
  vi: new Intl.DateTimeFormat('vi-VN', {
    day: 'numeric', month: 'short', timeZone: 'UTC',
  }),
  en: new Intl.DateTimeFormat('en-US', {
    day: 'numeric', month: 'short', timeZone: 'UTC',
  }),
};

const fullDayFormatters: Readonly<Record<Locale, Intl.DateTimeFormat>> = {
  vi: new Intl.DateTimeFormat('vi-VN', {
    day: 'numeric', month: 'short', year: 'numeric', timeZone: 'UTC',
  }),
  en: new Intl.DateTimeFormat('en-US', {
    day: 'numeric', month: 'short', year: 'numeric', timeZone: 'UTC',
  }),
};

// vi-VN's short month reads as "thg 9 2026"; the long form reads as a
// natural Vietnamese date ("tháng 9 năm 2026").
const monthFormatters: Readonly<Record<Locale, Intl.DateTimeFormat>> = {
  vi: new Intl.DateTimeFormat('vi-VN', {
    month: 'long', year: 'numeric', timeZone: 'UTC',
  }),
  en: new Intl.DateTimeFormat('en-US', {
    month: 'short', year: 'numeric', timeZone: 'UTC',
  }),
};

function formatAxisDay(locale: Locale, date: string): string {
  return axisDayFormatters[locale].format(parseDay(date));
}

function formatFullDay(locale: Locale, date: string): string {
  return fullDayFormatters[locale].format(parseDay(date));
}

function formatMonth(locale: Locale, month: string): string {
  return monthFormatters[locale].format(parseMonth(month));
}

export type ViewsIndexCopy = {
  readonly title: string;
  readonly loading: string;
  readonly unavailable: string;
  readonly emptyTitle: string;
  readonly emptyDescription: string;
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
  readonly headlineCount: (real: number) => string;
  readonly headlineRest: (real: number, filtered: number) => string;
  readonly definition: string;
  readonly chartHeading: string;
  readonly chartCaption: string;
  readonly chartEmpty: string;
  readonly chartDay: (date: string, real: number) => string;
  readonly axisDay: (date: string) => string;
  readonly fullDay: (date: string) => string;
  readonly monthlyHeading: string;
  readonly monthLabel: (month: string, real: number) => string;
  readonly filteredHeading: string;
  readonly filteredLabels: Readonly<Record<ViewsFilteredKey, string>>;
  readonly count: (n: number) => string;
  readonly previewsHeading: string;
  readonly previewsNote: string;
  readonly previewLine: (platform: string, fetches: number) => string;
  readonly honestLimit: string;
  readonly retry: string;
};

export const viewsIndexCopy: WorkspaceCopy<ViewsIndexCopy> = {
  vi: {
    title: 'Lượt xem',
    loading: 'Đang tải lượt xem…',
    unavailable: 'Không thể tải lượt xem. Hãy thử lại.',
    emptyTitle: 'Chưa có CV nào được xuất bản',
    emptyDescription: 'Xuất bản một CV để bắt đầu theo dõi lượt xem.',
    last7: '7 ngày qua',
    last30: '30 ngày qua',
    last90: '90 ngày qua',
    realFiltered: (real, filtered) => (
      `${formatNumber('vi', real)} lượt xem thật · `
      + `${formatNumber('vi', filtered)} bị lọc`
    ),
  },
  en: {
    title: 'Views',
    loading: 'Loading views…',
    unavailable: 'Could not load views. Try again.',
    emptyTitle: 'No resume has been published yet',
    emptyDescription: 'Publish a resume to start tracking its views.',
    last7: 'Last 7 days',
    last30: 'Last 30 days',
    last90: 'Last 90 days',
    realFiltered: (real, filtered) => (
      `${realViewsEn(real)} · ${formatNumber('en', filtered)} filtered`
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
    headlineCount: (real) => formatNumber('vi', real),
    headlineRest: (real, filtered) => (
      `lượt xem thật · ${formatNumber('vi', filtered)} bị lọc`
    ),
    definition:
      'Mỗi kết nối mạng được tính tối đa một lượt xem mỗi ngày. Hai người '
      + 'dùng chung một kết nối mạng trong một ngày được tính một lần; một '
      + 'người xem vào hai ngày được tính hai lần.',
    chartHeading: 'Lượt xem thật theo ngày (90 ngày qua)',
    chartCaption: 'Số lượt xem thật mỗi ngày, 90 ngày qua',
    chartEmpty: 'Chưa có lượt xem thật trong 90 ngày qua',
    chartDay: (date, real) => (
      `${formatFullDay('vi', date)}: ${formatNumber('vi', real)} `
      + 'lượt xem thật'
    ),
    axisDay: (date) => formatAxisDay('vi', date),
    fullDay: (date) => formatFullDay('vi', date),
    monthlyHeading: 'Tổng theo tháng (12 tháng qua)',
    monthLabel: (month, real) => (
      `${formatMonth('vi', month)}: ${formatNumber('vi', real)} lượt xem thật`
    ),
    filteredHeading: 'Lượt bị lọc trong 90 ngày qua',
    filteredLabels: {
      bot: 'Bot',
      datacenter: 'Truy cập từ máy chủ',
      anomaly: 'Tăng đột biến',
      invalid: 'Không vượt qua kiểm tra',
      crawler: 'Trình thu thập dữ liệu',
    },
    count: (n) => formatNumber('vi', n),
    previewsHeading: 'Xem trước liên kết',
    previewsNote:
      'Mỗi lần chia sẻ, ứng dụng có thể tải bản xem trước nhiều lần.',
    previewLine: (platform, fetches) => (
      `Xem trước liên kết trên ${platform} × ${formatNumber('vi', fetches)}`
    ),
    honestLimit:
      'Số liệu chỉ loại được những gì bộ lọc phát hiện; công cụ tự '
      + 'động tinh vi vẫn có thể được tính.',
    retry: 'Thử lại',
  },
  en: {
    loading: 'Loading views…',
    unavailable: 'Could not load views. Try again.',
    notFoundTitle: 'Resume not found',
    notFoundDescription: 'This resume does not exist or you cannot view it.',
    backToViews: 'Back to views',
    headlineCount: (real) => formatNumber('en', real),
    headlineRest: (real, filtered) => (
      `real ${real === 1 ? 'view' : 'views'} · `
      + `${formatNumber('en', filtered)} filtered`
    ),
    definition:
      'One view per network per day. Two people on one network in one day '
      + 'count once; one person on two days counts twice.',
    chartHeading: 'Real views by day (last 90 days)',
    chartCaption: 'Real views per day, last 90 days',
    chartEmpty: 'No real views in the last 90 days',
    chartDay: (date, real) => (
      `${formatFullDay('en', date)}: ${realViewsEn(real)}`
    ),
    axisDay: (date) => formatAxisDay('en', date),
    fullDay: (date) => formatFullDay('en', date),
    monthlyHeading: 'Monthly totals (last 12 months)',
    monthLabel: (month, real) => (
      `${formatMonth('en', month)}: ${realViewsEn(real)}`
    ),
    filteredHeading: 'Filtered over the last 90 days',
    filteredLabels: {
      bot: 'Bots',
      datacenter: 'Hosting networks',
      anomaly: 'Anomalies',
      invalid: 'Failed checks',
      crawler: 'Crawlers',
    },
    count: (n) => formatNumber('en', n),
    previewsHeading: 'Link previews',
    previewsNote: 'One share can fetch the preview more than once.',
    previewLine: (platform, fetches) => (
      `Link previews on ${platform} × ${formatNumber('en', fetches)}`
    ),
    honestLimit:
      'Counts exclude only what the filters detect; sophisticated '
      + 'automation can still be counted.',
    retry: 'Try again',
  },
};

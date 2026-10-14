import type { Locale } from './locale';

export interface FilterControlCopy {
  readonly filters: string;
  readonly filtersActive: (count: number) => string;
  readonly close: string;
  readonly clearFilters: string;
  readonly activeFilters: string;
  readonly removeFilter: (label: string) => string;
  readonly clearAll: string;
}

export const filterControls: Readonly<Record<Locale, FilterControlCopy>> = {
  vi: {
    filters: 'Bộ lọc',
    filtersActive: (count) => `Bộ lọc, ${count} đang bật`,
    close: 'Đóng',
    clearFilters: 'Xóa bộ lọc',
    activeFilters: 'Bộ lọc đang bật',
    removeFilter: (label) => `${label}, gỡ bộ lọc`,
    clearAll: 'Xóa hết',
  },
  en: {
    filters: 'Filters',
    filtersActive: (count) => `Filters, ${count} active`,
    close: 'Close',
    clearFilters: 'Clear filters',
    activeFilters: 'Active filters',
    removeFilter: (label) => `${label}, remove filter`,
    clearAll: 'Clear all',
  },
};

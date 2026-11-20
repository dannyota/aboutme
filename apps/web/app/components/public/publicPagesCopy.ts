export interface PublicPagesCopy {
  readonly pages: string;
  readonly previous: string;
  readonly next: string;
  readonly page: (number: number, count: number) => string;
}

const VIETNAMESE: PublicPagesCopy = {
  pages: 'Các trang CV',
  previous: 'Trang trước',
  next: 'Trang sau',
  page: (number, count) => `Trang ${number}/${count}`,
};

const ENGLISH: PublicPagesCopy = {
  pages: 'Resume pages',
  previous: 'Previous page',
  next: 'Next page',
  page: (number, count) => `Page ${number} of ${count}`,
};

export const publicPagesCopy = (lng: string): PublicPagesCopy =>
  lng.split('-')[0]?.toLowerCase() === 'vi' ? VIETNAMESE : ENGLISH;

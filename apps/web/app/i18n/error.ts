import type { Locale } from './locale';

type ErrorPageContent = {
  readonly title: string;
  readonly description: string;
};

export type ErrorPageCopy = {
  readonly notFound: ErrorPageContent;
  readonly generic: ErrorPageContent;
  readonly homeLink: string;
};

export const errorCopy: Record<Locale, ErrorPageCopy> = {
  vi: {
    notFound: {
      title: 'Không tìm thấy trang',
      description: 'Trang bạn tìm không tồn tại hoặc đã được di chuyển.',
    },
    generic: {
      title: 'Đã xảy ra lỗi',
      description: 'Đã có lỗi xảy ra. Vui lòng thử lại sau.',
    },
    homeLink: 'Về trang chủ',
  },
  en: {
    notFound: {
      title: 'Page not found',
      description: 'The page you are looking for does not exist or moved.',
    },
    generic: {
      title: 'Something went wrong',
      description: 'Something went wrong. Please try again later.',
    },
    homeLink: 'Go home',
  },
};

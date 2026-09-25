// Copy for the app-wide error page (app/error.vue): a 404 message and a
// generic message for every other error, in both site languages. Never
// includes the error's own message or stack; docs/design/security.md's CSP
// section is why the page has no framework-rendered fallback of its own.
import type { Locale } from './locale';

export type ErrorPageContent = {
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

import type { WorkspaceCopy } from './workspace';

export type PdfCopy = {
  readonly download: string;
  readonly downloading: string;
  readonly ariaDownload: (size: string) => string;
  readonly error: Record<
    'save-required' | 'session-lost' | 'download-failed'
    | 'temporarily-unavailable',
    string
  > & { readonly generic: string };
};

export const pdfCopy: WorkspaceCopy<PdfCopy> = {
  vi: {
    download: 'Tải PDF',
    downloading: 'Đang tải PDF…',
    ariaDownload: (size) => `Tải PDF, ${size}`,
    error: {
      'save-required': 'Lưu thay đổi trước khi tải PDF.',
      'session-lost': 'Phiên của bạn đã kết thúc. Đăng nhập lại.',
      'download-failed': 'Không thể tải PDF. Hãy thử lại.',
      'temporarily-unavailable': 'PDF tạm thời không khả dụng. Hãy thử lại.',
      'generic': 'Không thể tải PDF. Hãy thử lại.',
    },
  },
  en: {
    download: 'Download PDF',
    downloading: 'Downloading PDF…',
    ariaDownload: (size) => `Download PDF, ${size}`,
    error: {
      'save-required': 'Save changes before downloading PDF.',
      'session-lost': 'Your session ended. Sign in again.',
      'download-failed': 'PDF download failed. Try again.',
      'temporarily-unavailable': 'PDF is temporarily unavailable. Try again.',
      'generic': 'PDF download failed. Try again.',
    },
  },
};
